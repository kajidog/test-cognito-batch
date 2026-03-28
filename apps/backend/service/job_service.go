package service

import (
	"cognito-batch-backend/db"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type JobService struct {
	db                *gorm.DB
	validationService *ValidationService
	s3Service         *S3Service
	cognitoService    CognitoService
	processDelay      time.Duration
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
	}
}

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

	go s.processBatch(job.ID, inputs)

	return job, nil
}

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

func (s *JobService) processBatch(jobID string, inputs []db.User) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.failJob(jobID, fmt.Sprintf("panic: %v", recovered))
		}
	}()

	job := &db.Job{}
	if err := s.db.First(job, "id = ?", jobID).Error; err != nil {
		return
	}

	job.Status = db.JobStatusRunning
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	validationResult, err := s.validationService.ValidateUsers(inputs)
	if err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	updateTargets := make([]BatchUser, 0)
	newTargets := make([]BatchUser, 0)
	validationErrors := make([]db.JobError, 0)

	for index, row := range validationResult.Rows {
		input := inputs[index]
		batchUser := BatchUser{
			RowNumber: row.RowNumber,
			Email:     strings.TrimSpace(input.Email),
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

	if err := s.appendJobErrors(validationErrors); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	job.ProcessedCount = len(validationErrors)
	job.FailureCount = len(validationErrors)
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
		return
	}

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

		if err := s.db.Save(job).Error; err != nil {
			s.failJob(jobID, err.Error())
			return
		}
	}

	if len(newTargets) > 0 {
		objectKey, err := s.s3Service.UploadCSV(context.Background(), jobID, newTargets)
		if err != nil {
			s.recordBatchFailures(job, jobID, newTargets, fmt.Sprintf("s3 upload failed: %v", err))
			return
		}

		job.SourceObjectKey = &objectKey
		if err := s.db.Save(job).Error; err != nil {
			s.failJob(jobID, err.Error())
			return
		}

		results, err := s.cognitoService.CreateUsers(context.Background(), newTargets)
		if err != nil {
			s.recordBatchFailures(job, jobID, newTargets, fmt.Sprintf("cognito batch failed: %v", err))
			return
		}

		for _, result := range results {
			s.sleepStep()

			if result.ErrMessage != "" {
				job.ProcessedCount++
				job.FailureCount++
				if appendErr := s.appendJobErrors([]db.JobError{{
					JobID:     jobID,
					RowNumber: result.RowNumber,
					Name:      result.Name,
					Email:     result.Email,
					Message:   result.ErrMessage,
				}}); appendErr != nil {
					s.failJob(jobID, appendErr.Error())
					return
				}
			} else if err := s.createNewUser(result); err != nil {
				job.ProcessedCount++
				job.FailureCount++
				if appendErr := s.appendJobErrors([]db.JobError{{
					JobID:     jobID,
					RowNumber: result.RowNumber,
					Name:      result.Name,
					Email:     result.Email,
					Message:   fmt.Sprintf("db save failed: %v", err),
				}}); appendErr != nil {
					s.failJob(jobID, appendErr.Error())
					return
				}
			} else {
				job.ProcessedCount++
				job.SuccessCount++
			}

			if err := s.db.Save(job).Error; err != nil {
				s.failJob(jobID, err.Error())
				return
			}
		}
	}

	job.Status = db.JobStatusCompleted
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
	}
}

func (s *JobService) sleepStep() {
	if s.processDelay <= 0 {
		return
	}
	time.Sleep(s.processDelay)
}

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

func (s *JobService) updateExistingUser(user BatchUser) error {
	result := s.db.Model(&db.User{}).
		Where("name = ?", user.Name).
		Updates(map[string]any{"email": user.Email})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *JobService) createNewUser(result CognitoCreateResult) error {
	cognitoID := result.CognitoID
	return s.db.Create(&db.User{
		Email:     result.Email,
		Name:      result.Name,
		CognitoID: &cognitoID,
	}).Error
}

func (s *JobService) appendJobErrors(errors []db.JobError) error {
	if len(errors) == 0 {
		return nil
	}
	return s.db.Create(&errors).Error
}

func (s *JobService) recordBatchFailures(job *db.Job, jobID string, users []BatchUser, message string) {
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

	if err := s.appendJobErrors(errors); err != nil {
		s.failJob(jobID, err.Error())
		return
	}

	job.ProcessedCount += len(users)
	job.FailureCount += len(users)
	job.Status = db.JobStatusCompleted
	if err := s.db.Save(job).Error; err != nil {
		s.failJob(jobID, err.Error())
	}
}

func (s *JobService) failJob(jobID string, message string) {
	_ = s.appendJobErrors([]db.JobError{{
		JobID:     jobID,
		RowNumber: 0,
		Name:      "",
		Email:     "",
		Message:   message,
	}})
	_ = s.db.Model(&db.Job{}).Where("id = ?", jobID).Update("status", db.JobStatusFailed).Error
}

func joinValidationErrors(errors []ValidationFieldError) string {
	messages := make([]string, 0, len(errors))
	for _, item := range errors {
		messages = append(messages, item.Message)
	}
	return strings.Join(messages, ", ")
}
