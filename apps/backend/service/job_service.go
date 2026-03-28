// service パッケージ — バッチ処理のビジネスロジック。
//
// JobService がバッチ処理の中核を担い、以下のフローを管理する:
//
//	[フロントエンド] → startBatchUpsert mutation
//	  ↓
//	[StartBatchUpsert] Job レコード作成 (QUEUED) → goroutine で prepareBatch 開始
//	  ↓
//	[prepareBatch] ① バリデーション → ② 既存ユーザー更新 → ③ 新規ユーザーの Cognito import 開始
//	  ↓
//	[enqueueImport] CognitoImportQueue にレコード追加
//	  ↓
//	[StartWorker → processPendingImports] 定期ポーリングで Cognito ジョブの完了を検知
//	  ↓
//	[processImportQueue] 完了検知 → ユーザー resolve → ローカル DB upsert → Job を COMPLETED に
package service

import (
	"cognito-batch-backend/db"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// queuedImportPayload は CognitoImportQueue.Payload に JSON として保存される構造体。
// Cognito import 完了後に、どのユーザーが対象だったかを復元するために使う。
type queuedImportPayload struct {
	Users []BatchUser `json:"users"`
}

// JobService はバッチ処理のオーケストレーター。
// バリデーション、DB 更新、S3 アップロード、Cognito import の呼び出しを統括する。
type JobService struct {
	db                *gorm.DB
	validationService *ValidationService
	s3Service         *S3Service
	cognitoService    CognitoService
	processDelay      time.Duration // 各ステップ間のスリープ (デモ用の進捗可視化)
	pollInterval      time.Duration // Cognito import ジョブのポーリング間隔
}

func NewJobService(
	database *gorm.DB,
	validationService *ValidationService,
	s3Service *S3Service,
	cognitoService CognitoService,
) *JobService {
	return &JobService{
		db:                database,
		validationService: validationService,
		s3Service:         s3Service,
		cognitoService:    cognitoService,
		processDelay:      loadProcessDelay(),
		pollInterval:      loadImportPollInterval(),
	}
}

// StartWorker はバックグラウンドの Worker goroutine を起動する。
// pollInterval ごとに CognitoImportQueue を確認し、
// ポーリング時刻に達したキューの Cognito ジョブ状態を問い合わせる。
func (s *JobService) StartWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processPendingImports()
			}
		}
	}()
}

// StartBatchUpsert は GraphQL mutation から呼ばれるエントリーポイント。
// Job レコードを QUEUED 状態で作成し、即座にフロントエンドへ Job ID を返す。
// 実際の処理は goroutine (prepareBatch) でバックグラウンド実行される。
func (s *JobService) StartBatchUpsert(ctx context.Context, inputs []db.User) (*db.Job, error) {
	job := &db.Job{
		Status:         db.JobStatusQueued,
		TotalCount:     len(inputs),
		ProcessedCount: 0,
		SuccessCount:   0,
		FailureCount:   0,
	}

	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
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
	var job db.Job
	err := s.db.WithContext(ctx).
		Preload("Errors", func(tx *gorm.DB) *gorm.DB {
			return tx.Order("row_number asc, created_at asc")
		}).
		First(&job, "id = ?", jobID).Error
	if err != nil {
		return nil, err
	}

	return &job, nil
}

// prepareBatch はバッチ処理のメインロジック。goroutine で実行される。
// 処理は 3 フェーズに分かれる:
//   フェーズ 1: バリデーション — 全行を検証し、エラー行 / 更新行 / 新規行に分類
//   フェーズ 2: 既存ユーザー更新 — DB 上に既に存在するユーザーの情報を更新
//   フェーズ 3: Cognito import — 新規ユーザーを S3 経由で Cognito にインポート開始
func (s *JobService) prepareBatch(jobID string, inputs []db.User) {
	// panic が発生してもジョブを FAILED にして安全に終了させる
	defer func() {
		if recovered := recover(); recovered != nil {
			s.failJob(jobID, fmt.Sprintf("panic: %v", recovered))
		}
	}()

	job, err := s.loadJob(jobID)
	if err != nil {
		return
	}

	// === フェーズ 1: バリデーション ===
	job.Status = db.JobStatusRunning
	s.setJobMessage(job, "CSV validation started")
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	validationResult, err := s.validationService.ValidateUsers(inputs)
	if err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	updateTargets := make([]BatchUser, 0)  // DB に既存 → ローカル更新のみ
	newTargets := make([]BatchUser, 0)      // DB に未存在 → Cognito import 対象
	validationErrors := make([]db.JobError, 0)

	// バリデーション結果を 3 カテゴリに分類。
	// この分類により、即時完了するローカル更新と非同期の Cognito import を分離する。
	for index, row := range validationResult.Rows {
		input := inputs[index]
		batchUser := BatchUser{
			RowNumber: row.RowNumber,
			Email:     strings.TrimSpace(input.Email),
			Username:  strings.TrimSpace(input.Username),
			Name:      strings.TrimSpace(input.Name),
		}

		switch row.Status {
		case ValidationStatusUpdate:
			updateTargets = append(updateTargets, batchUser)
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
	if err := s.appendJobErrors(validationErrors); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	// バリデーションエラー分を処理済みとしてカウント
	job.ProcessedCount = len(validationErrors)
	job.FailureCount = len(validationErrors)
	s.setJobMessage(job, "Validation completed")
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	// === フェーズ 2: 既存ユーザー更新 ===
	// username で既存レコードを検索し、email / name を上書きする。
	for _, user := range updateTargets {
		s.sleepStep()

		if err := s.updateExistingUser(user); err != nil {
			job.ProcessedCount++
			job.FailureCount++
			if appendErr := s.appendJobErrors([]db.JobError{{
				JobID:     jobID,
				RowNumber: user.RowNumber,
				Name:      user.Name,
				Email:     user.Email,
				Message:   fmt.Sprintf("update failed: %v", err),
			}}); appendErr != nil {
				s.failJob(jobID, appendErr.Error())
				return
			}
		} else {
			job.ProcessedCount++
			job.SuccessCount++
		}

		s.setJobMessage(job, "Updating existing local users")
		if err := s.db.Save(job).Error; err != nil {
			s.failJob(jobID, err.Error())
			return
		}
	}

	// === フェーズ 3: 新規ユーザーの Cognito import ===
	// 新規ユーザーがいなければここで完了
	if len(newTargets) == 0 {
		job.Status = db.JobStatusCompleted
		s.setJobMessage(job, "No new Cognito users to import")
		if err := s.db.Save(job).Error; err != nil {
			s.failJob(jobID, err.Error())
		}
		return
	}

	// S3 に CSV を保存 (監査・デバッグ用)。
	// ※ Cognito import 自体は Cognito が返す presigned URL に直接アップロードするため、
	//   この S3 オブジェクトは Cognito からは参照されない。
	objectKey, err := s.s3Service.UploadCSV(context.Background(), jobID, newTargets)
	if err != nil {
		s.recordBatchFailures(job, jobID, newTargets, fmt.Sprintf("s3 upload failed: %v", err), true)
		return
	}
	job.SourceObjectKey = &objectKey

	// Cognito User Import Job を開始。
	// mock モードではインメモリで即時シミュレーション、
	// aws-import モードでは実際の Cognito API を呼び出す。
	startResult, err := s.cognitoService.StartImport(context.Background(), newTargets)
	if err != nil {
		s.recordBatchFailures(job, jobID, newTargets, fmt.Sprintf("cognito import start failed: %v", err), true)
		return
	}

	job.ExternalJobID = &startResult.ProviderJobID
	s.setJobMessage(job, startResult.Message)
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	// import ジョブをキューに登録。以降は Worker のポーリングに処理を委譲する。
	// prepareBatch の goroutine はここで終了し、Worker が完了を検知する。
	if err := s.enqueueImport(job, startResult.ProviderJobID, newTargets); err != nil {
		s.failJob(jobID, err.Error())
		return
	}
}

// processPendingImports はポーリング時刻に達したキューを順に処理する。
// StartWorker の ticker から定期的に呼ばれる。
func (s *JobService) processPendingImports() {
	queues := make([]db.CognitoImportQueue, 0)
	if err := s.db.
		Where("next_poll_at <= ?", time.Now()).
		Order("created_at asc").
		Find(&queues).Error; err != nil {
		return
	}

	for _, queue := range queues {
		s.processImportQueue(queue)
	}
}

// processImportQueue は個別のキューレコードを処理する。
// Cognito ジョブの状態を問い合わせ、状態に応じて以下の分岐を行う:
//   - PENDING/RUNNING: 次のポーリング時刻を設定してリトライ
//   - FAILED: 全対象ユーザーを失敗として記録し、Job を FAILED に
//   - COMPLETED: ResolveImportedUsers で Cognito 上のユーザーを取得し、ローカル DB に upsert
func (s *JobService) processImportQueue(queue db.CognitoImportQueue) {
	// キューに保存されている対象ユーザー一覧を復元
	payload, err := s.decodePayload(queue.Payload)
	if err != nil {
		s.failJob(queue.JobID, err.Error())
		_ = s.db.Delete(&queue).Error
		return
	}

	job, err := s.loadJob(queue.JobID)
	if err != nil {
		return
	}

	// Cognito 側のジョブ状態を問い合わせ
	status, err := s.cognitoService.DescribeImport(context.Background(), queue.ProviderJobID)
	if err != nil {
		// ポーリング失敗時はリトライ。ジョブ自体は RUNNING のまま維持する。
		s.setJobMessage(job, fmt.Sprintf("poll failed: %v", err))
		job.Status = db.JobStatusRunning
		_ = s.db.Save(job).Error
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.db.Save(&queue).Error
		return
	}

	// Cognito の進捗をベース値に加算して Job の進捗を更新
	job.ExternalJobID = &queue.ProviderJobID
	s.setJobMessage(job, status.Message)
	job.ProcessedCount = queue.BaseProcessedCount + status.ImportedUsers + status.FailedUsers
	job.SuccessCount = queue.BaseSuccessCount + status.ImportedUsers
	job.FailureCount = queue.BaseFailureCount + status.FailedUsers

	// まだ進行中 → 次のポーリングをスケジュール
	if status.State == ImportJobStatePending || status.State == ImportJobStateRunning {
		job.Status = db.JobStatusRunning
		_ = s.db.Save(job).Error
		queue.State = db.ImportQueueStateActive
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.db.Save(&queue).Error
		return
	}

	// Cognito 側で失敗 → 全対象ユーザーを失敗として記録
	if status.State == ImportJobStateFailed {
		message := status.Message
		if message == "" {
			message = "cognito import failed"
		}
		errors := buildResolutionErrors(queue.JobID, payload.Users, message)
		_ = s.appendJobErrors(errors)
		job.Status = db.JobStatusFailed
		job.ProcessedCount = queue.BaseProcessedCount + len(payload.Users)
		job.FailureCount = queue.BaseFailureCount + len(payload.Users)
		job.SuccessCount = queue.BaseSuccessCount
		s.setJobMessage(job, message)
		_ = s.db.Save(job).Error
		_ = s.db.Delete(&queue).Error
		return
	}

	// === Cognito import 完了 → ユーザー解決フェーズ ===
	// Cognito の import job は成功行ごとの `sub` (CognitoID) を返さないため、
	// 完了後に username で個別に AdminGetUser して sub を取得し、
	// ローカル DB のユーザーレコードと紐付ける。
	resolvedUsers, err := s.cognitoService.ResolveImportedUsers(context.Background(), usernamesFromBatchUsers(payload.Users))
	if err != nil {
		s.failJob(queue.JobID, err.Error())
		_ = s.db.Delete(&queue).Error
		return
	}

	// username → ImportedUser のマップを作成し、対象ユーザーを順に照合
	resolvedByUsername := make(map[string]ImportedUser, len(resolvedUsers))
	for _, resolved := range resolvedUsers {
		resolvedByUsername[resolved.Username] = resolved
	}

	// Cognito 上で見つかったユーザーをローカル DB に upsert。
	// 見つからなかったユーザーは unresolved として記録する。
	unresolved := make([]BatchUser, 0)
	for _, user := range payload.Users {
		resolved, ok := resolvedByUsername[user.Username]
		if !ok {
			unresolved = append(unresolved, user)
			continue
		}
		if err := s.upsertImportedUser(user, resolved); err != nil {
			unresolved = append(unresolved, user)
		}
	}

	// 再検索できなかった行は失敗として扱う。
	// これで、Cognito 側が集計件数しか返さない場合でも completion 画面の
	// 成功件数 / 失敗件数を安定して表示できる。
	if len(unresolved) > 0 {
		_ = s.appendJobErrors(buildResolutionErrors(
			queue.JobID,
			unresolved,
			"cognito import completed but user could not be resolved by username",
		))
	}

	job.Status = db.JobStatusCompleted
	job.ProcessedCount = queue.BaseProcessedCount + len(payload.Users)
	job.SuccessCount = queue.BaseSuccessCount + len(payload.Users) - len(unresolved)
	job.FailureCount = queue.BaseFailureCount + len(unresolved)
	if len(unresolved) == 0 {
		s.setJobMessage(job, "Cognito import completed")
	} else {
		s.setJobMessage(job, "Cognito import completed with unresolved users")
	}
	_ = s.db.Save(job).Error
	_ = s.db.Delete(&queue).Error
}

// enqueueImport は Cognito import 開始後にキューレコードを作成する。
// このレコードが存在する限り、Worker が定期的にポーリングを行う。
func (s *JobService) enqueueImport(job *db.Job, providerJobID string, users []BatchUser) error {
	payloadBytes, err := json.Marshal(queuedImportPayload{Users: users})
	if err != nil {
		return err
	}

	queue := db.CognitoImportQueue{
		JobID:         job.ID,
		ProviderMode:  s.cognitoService.Mode(),
		ProviderJobID: providerJobID,
		State:         db.ImportQueueStatePending,
		Payload:       string(payloadBytes),
		// import 開始前の件数を保持しておくと、worker 側で
		// 既存の validation / update 結果に provider の進捗を加算できる。
		BaseProcessedCount: job.ProcessedCount,
		BaseSuccessCount:   job.SuccessCount,
		BaseFailureCount:   job.FailureCount,
		NextPollAt:         time.Now().Add(s.pollInterval),
	}
	return s.db.Create(&queue).Error
}

func (s *JobService) decodePayload(raw string) (*queuedImportPayload, error) {
	var payload queuedImportPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *JobService) loadJob(jobID string) (*db.Job, error) {
	job := &db.Job{}
	if err := s.db.First(job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// sleepStep はデモ用にステップ間にスリープを挿入する。
// 環境変数 JOB_STEP_DELAY_MS で制御。0 にすると即座に処理が進む。
func (s *JobService) sleepStep() {
	if s.processDelay <= 0 {
		return
	}
	time.Sleep(s.processDelay)
}

// loadProcessDelay は環境変数 JOB_STEP_DELAY_MS からスリープ時間を読み込む (デフォルト: 1500ms)。
func loadProcessDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("JOB_STEP_DELAY_MS"))
	if raw == "" {
		return 1500 * time.Millisecond
	}

	delayMs, err := strconv.Atoi(raw)
	if err != nil || delayMs < 0 {
		return 1500 * time.Millisecond
	}

	return time.Duration(delayMs) * time.Millisecond
}

// loadImportPollInterval は環境変数 COGNITO_IMPORT_POLL_INTERVAL_MS からポーリング間隔を読み込む (デフォルト: 2秒)。
func loadImportPollInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("COGNITO_IMPORT_POLL_INTERVAL_MS"))
	if raw == "" {
		return 2 * time.Second
	}

	delayMs, err := strconv.Atoi(raw)
	if err != nil || delayMs <= 0 {
		return 2 * time.Second
	}

	return time.Duration(delayMs) * time.Millisecond
}

// updateExistingUser は既存ユーザーの email / name を username をキーに上書きする。
func (s *JobService) updateExistingUser(user BatchUser) error {
	result := s.db.Model(&db.User{}).
		Where("username = ?", user.Username).
		Updates(map[string]any{
			"email":    user.Email,
			"name":     user.Name,
			"username": user.Username,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// upsertImportedUser は Cognito import 完了後に、resolve されたユーザー情報をローカル DB に反映する。
// username で既存レコードを検索し、存在すれば更新、なければ新規作成する。
// CognitoID (sub) がここで初めてローカル DB に記録される。
func (s *JobService) upsertImportedUser(source BatchUser, resolved ImportedUser) error {
	cognitoID := resolved.CognitoID
	email := resolved.Email
	if email == "" {
		email = source.Email
	}
	name := resolved.Name
	if name == "" {
		name = source.Name
	}

	var user db.User
	err := s.db.Where("username = ?", source.Username).First(&user).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
		// import 完了後に初めてローカル DB へ作る経路があるため、
		// ここで行が存在しないこと自体は異常ではない。
		return s.db.Create(&db.User{
			Email:     email,
			Username:  source.Username,
			Name:      name,
			CognitoID: &cognitoID,
		}).Error
	}

	user.Email = email
	user.Name = name
	user.Username = source.Username
	user.CognitoID = &cognitoID
	return s.db.Save(&user).Error
}

func (s *JobService) appendJobErrors(errors []db.JobError) error {
	if len(errors) == 0 {
		return nil
	}
	return s.db.Create(&errors).Error
}

// recordBatchFailures は対象ユーザー全員を一括で失敗として記録するヘルパー。
// S3 アップロード失敗や Cognito import 開始失敗など、バッチ全体に影響するエラーで使う。
func (s *JobService) recordBatchFailures(job *db.Job, jobID string, users []BatchUser, message string, failJob bool) {
	errors := buildResolutionErrors(jobID, users, message)
	if err := s.appendJobErrors(errors); err != nil {
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
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
	}
}

// failJob はジョブを FAILED 状態にし、エラーメッセージを記録する最終手段。
func (s *JobService) failJob(jobID string, message string) {
	_ = s.appendJobErrors([]db.JobError{{
		JobID:     jobID,
		RowNumber: 0,
		Name:      "",
		Email:     "",
		Message:   message,
	}})
	_ = s.db.Model(&db.Job{}).
		Where("id = ?", jobID).
		Updates(map[string]any{
			"status":         db.JobStatusFailed,
			"status_message": message,
		}).Error
}

func (s *JobService) setJobMessage(job *db.Job, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		job.StatusMessage = nil
		return
	}
	job.StatusMessage = &message
}

func buildResolutionErrors(jobID string, users []BatchUser, message string) []db.JobError {
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

func usernamesFromBatchUsers(users []BatchUser) []string {
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
