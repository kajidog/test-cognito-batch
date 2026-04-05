// service パッケージ — バッチ処理のビジネスロジック。
//
// JobService がバッチ処理の中核を担い、以下のフローを管理する:
//
//	[フロントエンド] → startBatchCreate mutation
//	  ↓
//	[StartBatchCreate] Job レコード作成 (QUEUED) → goroutine で prepareBatch 開始
//	  ↓
//	[prepareBatch] ① バリデーション → ② 新規ユーザーの Cognito import 開始
//	  ↓
//	[enqueueImport] CognitoImportQueue にレコード追加
//	  ↓
//	[Worker → ProcessPendingImports] 定期ポーリングで Cognito ジョブの完了を検知
//	  ↓
//	[processImportQueue] 完了検知 → ユーザー resolve → ローカル DB create → Job を COMPLETED に
package service

import (
	"bytes"
	"cognito-batch-backend/db"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/internal/repository"
	"cognito-batch-backend/model"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// queuedImportPayload は CognitoImportQueue.Payload に JSON として保存される構造体。
// Cognito import 完了後に、どのユーザーが対象だったかを復元するために使う。
type queuedImportPayload struct {
	Users []model.BatchUser `json:"users"`
}

// JobService はバッチ処理のオーケストレーター。
// バリデーション、S3 アップロード、Cognito import の呼び出しを統括する。
type JobService struct {
	userRepo          repository.UserRepository
	jobRepo           repository.JobRepository
	importQueueRepo   repository.ImportQueueRepository
	validationService *ValidationService
	s3Service         *S3Service
	cognitoService    CognitoService
	processDelay      time.Duration // 各ステップ間のスリープ (デモ用の進捗可視化)
	pollInterval      time.Duration // Cognito import ジョブのポーリング間隔
}

func NewJobService(
	cfg config.JobConfig,
	userRepo repository.UserRepository,
	jobRepo repository.JobRepository,
	importQueueRepo repository.ImportQueueRepository,
	validationService *ValidationService,
	s3Service *S3Service,
	cognitoService CognitoService,
) *JobService {
	return &JobService{
		userRepo:          userRepo,
		jobRepo:           jobRepo,
		importQueueRepo:   importQueueRepo,
		validationService: validationService,
		s3Service:         s3Service,
		cognitoService:    cognitoService,
		processDelay:      cfg.ProcessDelay,
		pollInterval:      cfg.PollInterval,
	}
}

// StartBatchCreate は GraphQL mutation から呼ばれるエントリーポイント。
// Job レコードを QUEUED 状態で作成し、即座にフロントエンドへ Job ID を返す。
// 実際の処理は goroutine (prepareBatch) でバックグラウンド実行される。
func (s *JobService) StartBatchCreate(ctx context.Context, inputs []db.User) (*db.Job, error) {
	job := &db.Job{
		Status:         db.JobStatusQueued,
		TotalCount:     len(inputs),
		ProcessedCount: 0,
		SuccessCount:   0,
		FailureCount:   0,
	}

	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, err
	}

	// バックグラウンドで prepareBatch を開始。
	// フロントエンドは返された Job ID を使って進捗をポーリングする。
	go s.prepareBatch(job.ID, inputs)

	return job, nil
}

// GetByID はジョブの詳細を取得する。エラー一覧も行番号順でプリロードする。
// フロントエンドの進捗画面・完了画面から定期的に呼ばれる。
func (s *JobService) GetByID(ctx context.Context, jobID string) (*db.Job, error) {
	return s.jobRepo.GetByIDWithErrors(ctx, jobID)
}

// prepareBatch はバッチ処理のメインロジック。goroutine で実行される。
// 処理は 2 フェーズに分かれる:
//
//	フェーズ 1: バリデーション — 全行を検証し、エラー行 / 新規行に分類
//	フェーズ 2: Cognito import — 新規ユーザーを S3 経由で Cognito にインポート開始
func (s *JobService) prepareBatch(jobID string, inputs []db.User) {
	ctx := context.Background()

	// panic が発生してもジョブを FAILED にして安全に終了させる
	defer func() {
		if recovered := recover(); recovered != nil {
			s.failJob(jobID, fmt.Sprintf("panic: %v", recovered))
		}
	}()

	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return
	}
	if s.isCanceled(job) {
		return
	}

	// === フェーズ 1: バリデーション ===
	job.Status = db.JobStatusRunning
	s.setJobMessage(job, "CSV validation started")
	if err := s.jobRepo.Save(ctx, job); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	validationResult, err := s.validationService.ValidateUsers(inputs)
	if err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	newTargets := make([]model.BatchUser, 0) // DB に未存在 → Cognito import 対象
	validationErrors := make([]db.JobError, 0)

	// バリデーション結果を 2 カテゴリに分類。
	for index, row := range validationResult.Rows {
		input := inputs[index]
		batchUser := model.BatchUser{
			RowNumber: row.RowNumber,
			Email:     strings.TrimSpace(input.Email),
			Username:  strings.TrimSpace(input.Username),
			Name:      strings.TrimSpace(input.Name),
		}

		switch row.Status {
		case ValidationStatusNew:
			newTargets = append(newTargets, batchUser)
		case ValidationStatusError:
			validationErrors = append(validationErrors, db.JobError{
				JobID:     jobID,
				RowNumber: batchUser.RowNumber,
				Name:      batchUser.Name,
				Email:     batchUser.Email,
				Message:   joinValidationErrors(row.Errors),
			})
		}
	}

	// バリデーションエラーを DB に保存
	if err := s.jobRepo.CreateErrors(ctx, validationErrors); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	// バリデーションエラー分を処理済みとしてカウント
	job.ProcessedCount = len(validationErrors)
	job.FailureCount = len(validationErrors)
	s.setJobMessage(job, "Validation completed")
	if err := s.jobRepo.Save(ctx, job); err != nil {
		s.failJob(jobID, err.Error())
		return
	}
	if s.refreshAbortState(ctx, jobID) {
		return
	}

	// === フェーズ 2: 新規ユーザーの Cognito import ===
	if len(newTargets) == 0 {
		job.Status = db.JobStatusCompleted
		s.setJobMessage(job, "No new Cognito users to import")
		if err := s.jobRepo.Save(ctx, job); err != nil {
			s.failJob(jobID, err.Error())
		}
		return
	}

	// S3 に CSV を保存 (監査・デバッグ用)。
	csvData, err := buildAuditCSV(newTargets)
	if err != nil {
		s.recordBatchFailures(ctx, job, jobID, newTargets, fmt.Sprintf("csv build failed: %v", err), true)
		return
	}
	objectKey := s.s3Service.ObjectKey(jobID, "new-users.csv")
	if err := s.s3Service.Upload(ctx, objectKey, csvData, "text/csv"); err != nil {
		s.recordBatchFailures(ctx, job, jobID, newTargets, fmt.Sprintf("s3 upload failed: %v", err), true)
		return
	}
	job.SourceObjectKey = &objectKey
	if err := s.jobRepo.Save(ctx, job); err != nil {
		s.failJob(jobID, err.Error())
		return
	}
	if s.refreshAbortState(ctx, jobID) {
		_ = s.cleanupJobArtifacts(ctx, job, nil, nil)
		return
	}

	// Cognito User Import Job を開始。
	startResult, err := s.cognitoService.StartImport(ctx, newTargets)
	if err != nil {
		s.recordBatchFailures(ctx, job, jobID, newTargets, fmt.Sprintf("cognito import start failed: %v", err), true)
		return
	}

	job.ExternalJobID = &startResult.ProviderJobID
	s.setJobMessage(job, startResult.Message)
	if err := s.jobRepo.Save(ctx, job); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	// import ジョブをキューに登録。以降は Worker のポーリングに処理を委譲する。
	if err := s.enqueueImport(ctx, job, startResult.ProviderJobID, newTargets); err != nil {
		s.failJob(jobID, err.Error())
		return
	}
}

// CancelJob は進行中ジョブを停止し、作成済みのユーザーと補助データを削除する。
func (s *JobService) CancelJob(ctx context.Context, jobID string) (*db.Job, error) {
	job, err := s.jobRepo.GetByIDWithErrors(ctx, jobID)
	if err != nil {
		return nil, err
	}

	if job.Status == db.JobStatusCompleted || job.Status == db.JobStatusFailed || job.Status == db.JobStatusCanceled {
		return nil, fmt.Errorf("job cannot be canceled in status %s", job.Status)
	}

	queue, err := s.importQueueRepo.FindByJobID(ctx, jobID)
	if err != nil {
		return nil, err
	}

	var payload *queuedImportPayload
	if queue != nil {
		payload, err = decodePayload(queue.Payload)
		if err != nil {
			return nil, err
		}
	}

	job.Status = db.JobStatusCanceled
	s.setJobMessage(job, "Job canceled")
	if err := s.jobRepo.Save(ctx, job); err != nil {
		return nil, err
	}

	if err := s.cleanupJobArtifacts(ctx, job, queue, payload); err != nil {
		return nil, err
	}

	return s.jobRepo.GetByIDWithErrors(ctx, jobID)
}

// ProcessPendingImports はポーリング時刻に達したキューを順に処理する。
// worker.Worker から定期的に呼ばれる。
func (s *JobService) ProcessPendingImports() {
	ctx := context.Background()
	queues, err := s.importQueueRepo.FindDue(ctx, time.Now())
	if err != nil {
		return
	}

	for _, queue := range queues {
		s.processImportQueue(ctx, queue)
	}
}

// processImportQueue は個別のキューレコードを処理する。
func (s *JobService) processImportQueue(ctx context.Context, queue db.CognitoImportQueue) {
	// キューに保存されている対象ユーザー一覧を復元
	payload, err := decodePayload(queue.Payload)
	if err != nil {
		s.failJob(queue.JobID, err.Error())
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	job, err := s.jobRepo.GetByID(ctx, queue.JobID)
	if err != nil {
		return
	}
	if s.isCanceled(job) {
		_ = s.cleanupJobArtifacts(ctx, job, &queue, payload)
		return
	}

	// Cognito 側のジョブ状態を問い合わせ
	status, err := s.cognitoService.DescribeImport(ctx, queue.ProviderJobID)
	if err != nil {
		// ポーリング失敗時はリトライ。ジョブ自体は RUNNING のまま維持する。
		s.setJobMessage(job, fmt.Sprintf("poll failed: %v", err))
		job.Status = db.JobStatusRunning
		_ = s.jobRepo.Save(ctx, job)
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.importQueueRepo.Save(ctx, &queue)
		return
	}

	// Cognito の進捗と import 前の処理結果から Job 全体の進捗を算出
	newUserCount := len(payload.Users)
	preImportProcessed := job.TotalCount - newUserCount
	preImportSuccess := preImportProcessed - queue.PreImportFailureCount

	job.ExternalJobID = &queue.ProviderJobID
	s.setJobMessage(job, status.Message)
	job.ProcessedCount = preImportProcessed + status.ImportedUsers + status.FailedUsers
	job.SuccessCount = preImportSuccess + status.ImportedUsers
	job.FailureCount = queue.PreImportFailureCount + status.FailedUsers

	// まだ進行中 → 次のポーリングをスケジュール
	if status.State == ImportJobStatePending || status.State == ImportJobStateRunning {
		job.Status = db.JobStatusRunning
		_ = s.jobRepo.Save(ctx, job)
		queue.State = db.ImportQueueStateActive
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.importQueueRepo.Save(ctx, &queue)
		return
	}

	// Cognito 側で失敗 → 全対象ユーザーを失敗として記録
	if status.State == ImportJobStateFailed {
		message := status.Message
		if message == "" {
			message = "cognito import failed"
		}
		errors := buildResolutionErrors(queue.JobID, payload.Users, message)
		_ = s.jobRepo.CreateErrors(ctx, errors)
		job.Status = db.JobStatusFailed
		job.ProcessedCount = preImportProcessed + newUserCount
		job.FailureCount = queue.PreImportFailureCount + newUserCount
		job.SuccessCount = preImportSuccess
		s.setJobMessage(job, message)
		_ = s.jobRepo.Save(ctx, job)
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	// === Cognito import 完了 → ユーザー解決フェーズ ===
	resolvedUsers, err := s.cognitoService.ResolveImportedUsers(ctx, usernamesFromBatchUsers(payload.Users))
	if err != nil {
		s.failJob(queue.JobID, err.Error())
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	resolvedByUsername := make(map[string]ImportedUser, len(resolvedUsers))
	for _, resolved := range resolvedUsers {
		resolvedByUsername[resolved.Username] = resolved
	}

	unresolved := make([]model.BatchUser, 0)
	for _, user := range payload.Users {
		resolved, ok := resolvedByUsername[user.Username]
		if !ok {
			unresolved = append(unresolved, user)
			continue
		}
		if err := s.createImportedUser(ctx, user, resolved); err != nil {
			unresolved = append(unresolved, user)
		}
	}

	if len(unresolved) > 0 {
		_ = s.jobRepo.CreateErrors(ctx, buildResolutionErrors(
			queue.JobID,
			unresolved,
			"cognito import completed but user could not be resolved by username",
		))
	}

	job.Status = db.JobStatusCompleted
	job.ProcessedCount = preImportProcessed + newUserCount
	job.SuccessCount = preImportSuccess + newUserCount - len(unresolved)
	job.FailureCount = queue.PreImportFailureCount + len(unresolved)
	if len(unresolved) == 0 {
		s.setJobMessage(job, "Cognito import completed")
	} else {
		s.setJobMessage(job, "Cognito import completed with unresolved users")
	}
	_ = s.jobRepo.Save(ctx, job)
	_ = s.importQueueRepo.Delete(ctx, &queue)
}

// enqueueImport は Cognito import 開始後にキューレコードを作成する。
func (s *JobService) enqueueImport(ctx context.Context, job *db.Job, providerJobID string, users []model.BatchUser) error {
	payloadBytes, err := json.Marshal(queuedImportPayload{Users: users})
	if err != nil {
		return err
	}

	queue := db.CognitoImportQueue{
		JobID:                 job.ID,
		ProviderMode:          s.cognitoService.Mode(),
		ProviderJobID:         providerJobID,
		State:                 db.ImportQueueStatePending,
		Payload:               string(payloadBytes),
		PreImportFailureCount: job.FailureCount,
		NextPollAt:            time.Now().Add(s.pollInterval),
	}
	return s.importQueueRepo.Create(ctx, &queue)
}

func decodePayload(raw string) (*queuedImportPayload, error) {
	var payload queuedImportPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// sleepStep はデモ用にステップ間にスリープを挿入する。
func (s *JobService) sleepStep() {
	if s.processDelay <= 0 {
		return
	}
	time.Sleep(s.processDelay)
}

// buildAuditCSV は新規ユーザー一覧を監査用 CSV に変換する。
func buildAuditCSV(users []model.BatchUser) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	if err := writer.Write([]string{"email", "username", "name"}); err != nil {
		return nil, err
	}
	for _, user := range users {
		if err := writer.Write([]string{user.Email, user.Username, user.Name}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// createImportedUser は Cognito import 完了後にユーザー情報をローカル DB へ新規作成する。
func (s *JobService) createImportedUser(ctx context.Context, source model.BatchUser, resolved ImportedUser) error {
	cognitoID := resolved.CognitoID
	email := resolved.Email
	if email == "" {
		email = source.Email
	}
	name := resolved.Name
	if name == "" {
		name = source.Name
	}

	user, err := s.userRepo.GetByUsername(ctx, source.Username)
	if err != nil {
		return err
	}
	if user != nil {
		return fmt.Errorf("user already exists")
	}

	return s.userRepo.Create(ctx, &db.User{
		Email:     email,
		Username:  source.Username,
		Name:      name,
		CognitoID: &cognitoID,
	})
}

// recordBatchFailures は対象ユーザー全員を一括で失敗として記録するヘルパー。
func (s *JobService) recordBatchFailures(ctx context.Context, job *db.Job, jobID string, users []model.BatchUser, message string, failJob bool) {
	errors := buildResolutionErrors(jobID, users, message)
	if err := s.jobRepo.CreateErrors(ctx, errors); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	job.ProcessedCount += len(users)
	job.FailureCount += len(users)
	if failJob {
		job.Status = db.JobStatusFailed
	} else {
		job.Status = db.JobStatusCompleted
	}
	s.setJobMessage(job, message)
	if err := s.jobRepo.Save(ctx, job); err != nil {
		s.failJob(jobID, err.Error())
	}
}

// failJob はジョブを FAILED 状態にし、エラーメッセージを記録する最終手段。
func (s *JobService) failJob(jobID string, message string) {
	ctx := context.Background()
	_ = s.jobRepo.CreateErrors(ctx, []db.JobError{{
		JobID:     jobID,
		RowNumber: 0,
		Name:      "",
		Email:     "",
		Message:   message,
	}})
	_ = s.jobRepo.UpdateFields(ctx, jobID, map[string]any{
		"status":         db.JobStatusFailed,
		"status_message": message,
	})
}

func (s *JobService) refreshAbortState(ctx context.Context, jobID string) bool {
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return false
	}
	return s.isCanceled(job)
}

func (s *JobService) isCanceled(job *db.Job) bool {
	return job != nil && job.Status == db.JobStatusCanceled
}

func (s *JobService) cleanupJobArtifacts(ctx context.Context, job *db.Job, queue *db.CognitoImportQueue, payload *queuedImportPayload) error {
	if queue != nil {
		if err := s.cognitoService.StopImport(ctx, queue.ProviderJobID); err != nil && !isIgnorableStopError(err) {
			return err
		}

		if payload != nil {
			resolvedUsers, err := s.cognitoService.ResolveImportedUsers(ctx, usernamesFromBatchUsers(payload.Users))
			if err != nil {
				return err
			}
			resolvedUsernames := usernamesFromImportedUsers(resolvedUsers)
			if err := s.cognitoService.DeleteUsers(ctx, resolvedUsernames); err != nil {
				return err
			}
			if err := s.userRepo.DeleteByUsernames(ctx, resolvedUsernames); err != nil {
				return err
			}
		}

		if err := s.importQueueRepo.Delete(ctx, queue); err != nil {
			return err
		}
	}

	if job != nil && job.SourceObjectKey != nil && *job.SourceObjectKey != "" {
		if err := s.s3Service.Delete(ctx, *job.SourceObjectKey); err != nil {
			return err
		}
	}

	return nil
}

func usernamesFromImportedUsers(users []ImportedUser) []string {
	usernames := make([]string, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.Username) == "" {
			continue
		}
		usernames = append(usernames, user.Username)
	}
	return usernames
}

func isIgnorableStopError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "expired") || strings.Contains(msg, "stopped")
}

func (s *JobService) setJobMessage(job *db.Job, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		job.StatusMessage = nil
		return
	}
	job.StatusMessage = &message
}

func buildResolutionErrors(jobID string, users []model.BatchUser, message string) []db.JobError {
	errors := make([]db.JobError, 0, len(users))
	for _, user := range users {
		errors = append(errors, db.JobError{
			JobID:     jobID,
			RowNumber: user.RowNumber,
			Name:      user.Name,
			Email:     user.Email,
			Message:   message,
		})
	}
	return errors
}

func usernamesFromBatchUsers(users []model.BatchUser) []string {
	items := make([]string, 0, len(users))
	for _, user := range users {
		items = append(items, user.Username)
	}
	return items
}

func joinValidationErrors(errors []ValidationFieldError) string {
	messages := make([]string, 0, len(errors))
	for _, item := range errors {
		messages = append(messages, item.Message)
	}
	return strings.Join(messages, ", ")
}
