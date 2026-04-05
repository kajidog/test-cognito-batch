package service

import (
	"context"
	"testing"
	"time"

	"cognito-batch-backend/db"
	cognitoport "cognito-batch-backend/internal/cognito"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/model"
)

func TestStartBatchCreateFailsWhenNoImportableUsers(t *testing.T) {
	jobRepo := &stubJobRepository{}
	service := NewJobService(
		config.JobConfig{PollInterval: time.Second},
		&stubUserRepository{},
		jobRepo,
		&stubImportQueueRepository{},
		stubValidator{
			result: &ValidationResult{
				Summary: ValidationSummary{ErrorCount: 1},
				Rows: []ValidationRow{{
					RowNumber: 2,
					Status:    ValidationStatusError,
					Errors: []ValidationFieldError{{
						Field:   "username",
						Message: "username は既に登録済みです",
					}},
				}},
			},
		},
		&stubArtifactStore{},
		stubCognitoService{},
	)

	job, err := service.StartBatchCreate(context.Background(), []db.User{{
		Email:    "dupe@example.com",
		Username: "dupe",
		Name:     "Dup",
	}})
	if err != nil {
		t.Fatalf("StartBatchCreate returned error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		saved, err := jobRepo.GetByID(context.Background(), job.ID)
		if err == nil && saved.Status == db.JobStatusFailed {
			if saved.ProcessedCount != 1 {
				t.Fatalf("ProcessedCount = %d, want 1", saved.ProcessedCount)
			}
			if saved.SuccessCount != 0 {
				t.Fatalf("SuccessCount = %d, want 0", saved.SuccessCount)
			}
			if saved.FailureCount != 1 {
				t.Fatalf("FailureCount = %d, want 1", saved.FailureCount)
			}
			if saved.StatusMessage == nil || *saved.StatusMessage != "Validation failed: no importable users" {
				t.Fatalf("StatusMessage = %v, want validation failure", saved.StatusMessage)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("job did not transition to FAILED")
}

type stubValidator struct {
	result *ValidationResult
	err    error
}

func (s stubValidator) ValidateUsers(context.Context, []db.User) (*ValidationResult, error) {
	return s.result, s.err
}

type stubArtifactStore struct{}

func (s *stubArtifactStore) ObjectKey(parts ...string) string { return "" }
func (s *stubArtifactStore) Upload(context.Context, string, []byte, string) error {
	return nil
}
func (s *stubArtifactStore) Delete(context.Context, string) error { return nil }

type stubCognitoService struct{}

func (s stubCognitoService) Mode() string { return "stub" }
func (s stubCognitoService) StartImport(context.Context, []model.BatchUser) (*cognitoport.ImportJobStartResult, error) {
	return &cognitoport.ImportJobStartResult{}, nil
}
func (s stubCognitoService) DescribeImport(context.Context, string) (*cognitoport.ImportJobStatusResult, error) {
	return &cognitoport.ImportJobStatusResult{}, nil
}
func (s stubCognitoService) StopImport(context.Context, string) error { return nil }
func (s stubCognitoService) DeleteUsers(context.Context, []string) error {
	return nil
}
func (s stubCognitoService) ResolveImportedUsers(context.Context, []string) ([]cognitoport.ImportedUser, error) {
	return nil, nil
}

type stubUserRepository struct{}

func (s *stubUserRepository) List(context.Context) ([]db.User, error) { return nil, nil }
func (s *stubUserRepository) GetByName(context.Context, string) (*db.User, error) {
	return nil, nil
}
func (s *stubUserRepository) FindByUsernames(context.Context, []string) ([]db.User, error) {
	return nil, nil
}
func (s *stubUserRepository) GetByUsername(context.Context, string) (*db.User, error) {
	return nil, nil
}
func (s *stubUserRepository) Create(context.Context, *db.User) error { return nil }
func (s *stubUserRepository) DeleteByUsernames(context.Context, []string) error {
	return nil
}
func (s *stubUserRepository) UpdateByUsername(context.Context, string, map[string]any) (int64, error) {
	return 0, nil
}
func (s *stubUserRepository) Save(context.Context, *db.User) error { return nil }

type stubJobRepository struct {
	job *db.Job
}

func (s *stubJobRepository) Create(_ context.Context, job *db.Job) error {
	copy := *job
	s.job = &copy
	return nil
}

func (s *stubJobRepository) GetByID(_ context.Context, id string) (*db.Job, error) {
	if s.job == nil || s.job.ID != id {
		return nil, context.Canceled
	}
	copy := *s.job
	return &copy, nil
}

func (s *stubJobRepository) GetByIDWithErrors(ctx context.Context, id string) (*db.Job, error) {
	return s.GetByID(ctx, id)
}

func (s *stubJobRepository) Save(_ context.Context, job *db.Job) error {
	copy := *job
	s.job = &copy
	return nil
}

func (s *stubJobRepository) UpdateFields(_ context.Context, _ string, fields map[string]any) error {
	if s.job == nil {
		s.job = &db.Job{}
	}
	if status, ok := fields["status"].(db.JobStatus); ok {
		s.job.Status = status
	}
	if message, ok := fields["status_message"].(string); ok {
		s.job.StatusMessage = &message
	}
	return nil
}

func (s *stubJobRepository) CreateErrors(context.Context, []db.JobError) error { return nil }

type stubImportQueueRepository struct{}

func (s *stubImportQueueRepository) Create(context.Context, *db.CognitoImportQueue) error {
	return nil
}
func (s *stubImportQueueRepository) FindDue(context.Context, time.Time) ([]db.CognitoImportQueue, error) {
	return nil, nil
}
func (s *stubImportQueueRepository) FindByJobID(context.Context, string) (*db.CognitoImportQueue, error) {
	return nil, nil
}
func (s *stubImportQueueRepository) ListActive(context.Context) ([]db.CognitoImportQueue, error) {
	return nil, nil
}
func (s *stubImportQueueRepository) Save(context.Context, *db.CognitoImportQueue) error {
	return nil
}
func (s *stubImportQueueRepository) Delete(context.Context, *db.CognitoImportQueue) error {
	return nil
}
