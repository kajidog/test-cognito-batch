package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cognito-batch-backend/db"
	cognitoport "cognito-batch-backend/internal/cognito"
	"cognito-batch-backend/internal/repository"
	"cognito-batch-backend/model"
)

type importPollingService struct {
	userRepo        repository.UserRepository
	jobRepo         repository.JobRepository
	importQueueRepo repository.ImportQueueRepository
	cognitoService  cognitoport.Service
	pollInterval    time.Duration
}

func newImportPollingService(
	userRepo repository.UserRepository,
	jobRepo repository.JobRepository,
	importQueueRepo repository.ImportQueueRepository,
	cognitoService cognitoport.Service,
	pollInterval time.Duration,
) *importPollingService {
	return &importPollingService{
		userRepo:        userRepo,
		jobRepo:         jobRepo,
		importQueueRepo: importQueueRepo,
		cognitoService:  cognitoService,
		pollInterval:    pollInterval,
	}
}

func (s *importPollingService) ProcessPendingImports(ctx context.Context) {
	queues, err := s.importQueueRepo.FindDue(ctx, time.Now())
	if err != nil {
		return
	}

	for _, queue := range queues {
		s.processImportQueue(ctx, queue)
	}
}

func (s *importPollingService) processImportQueue(ctx context.Context, queue db.CognitoImportQueue) {
	payload, err := decodePayload(queue.Payload)
	if err != nil {
		s.failJob(ctx, queue.JobID, err.Error())
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	job, err := s.jobRepo.GetByID(ctx, queue.JobID)
	if err != nil {
		return
	}
	if isCanceled(job) {
		return
	}

	status, err := s.cognitoService.DescribeImport(ctx, queue.ProviderJobID)
	if err != nil {
		setJobMessage(job, fmt.Sprintf("poll failed: %v", err))
		job.Status = db.JobStatusRunning
		_ = s.jobRepo.Save(ctx, job)
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.importQueueRepo.Save(ctx, &queue)
		return
	}

	newUserCount := len(payload.Users)
	preImportProcessed := job.TotalCount - newUserCount
	preImportSuccess := preImportProcessed - queue.PreImportFailureCount

	job.ExternalJobID = &queue.ProviderJobID
	setJobMessage(job, status.Message)
	job.ProcessedCount = preImportProcessed + status.ImportedUsers + status.FailedUsers
	job.SuccessCount = preImportSuccess + status.ImportedUsers
	job.FailureCount = queue.PreImportFailureCount + status.FailedUsers

	if status.State == cognitoport.ImportJobStatePending || status.State == cognitoport.ImportJobStateRunning {
		job.Status = db.JobStatusRunning
		_ = s.jobRepo.Save(ctx, job)
		queue.State = db.ImportQueueStateActive
		queue.AttemptCount++
		queue.NextPollAt = time.Now().Add(s.pollInterval)
		_ = s.importQueueRepo.Save(ctx, &queue)
		return
	}

	if status.State == cognitoport.ImportJobStateFailed {
		message := status.Message
		if message == "" {
			message = "cognito import failed"
		}
		_ = s.jobRepo.CreateErrors(ctx, buildResolutionErrors(queue.JobID, payload.Users, message))
		job.Status = db.JobStatusFailed
		job.ProcessedCount = preImportProcessed + newUserCount
		job.FailureCount = queue.PreImportFailureCount + newUserCount
		job.SuccessCount = preImportSuccess
		setJobMessage(job, message)
		_ = s.jobRepo.Save(ctx, job)
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	resolvedUsers, err := s.cognitoService.ResolveImportedUsers(ctx, usernamesFromBatchUsers(payload.Users))
	if err != nil {
		s.failJob(ctx, queue.JobID, err.Error())
		_ = s.importQueueRepo.Delete(ctx, &queue)
		return
	}

	resolvedByUsername := make(map[string]cognitoport.ImportedUser, len(resolvedUsers))
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
		setJobMessage(job, "Cognito import completed")
	} else {
		setJobMessage(job, "Cognito import completed with unresolved users")
	}
	_ = s.jobRepo.Save(ctx, job)
	_ = s.importQueueRepo.Delete(ctx, &queue)
}

func (s *importPollingService) createImportedUser(ctx context.Context, source model.BatchUser, resolved cognitoport.ImportedUser) error {
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

func (s *importPollingService) failJob(ctx context.Context, jobID string, message string) {
	_ = s.jobRepo.CreateErrors(ctx, []db.JobError{{
		JobID:     jobID,
		RowNumber: 0,
		Name:      "",
		Email:     "",
		Message:   message,
	}})
	_ = s.jobRepo.UpdateFields(ctx, jobID, map[string]any{
		"status":         db.JobStatusFailed,
		"status_message": strings.TrimSpace(message),
	})
}
