package cognito

import (
	"context"

	"cognito-batch-backend/model"
)

type ImportJobState string

const (
	ImportJobStatePending   ImportJobState = "PENDING"
	ImportJobStateRunning   ImportJobState = "RUNNING"
	ImportJobStateCompleted ImportJobState = "COMPLETED"
	ImportJobStateFailed    ImportJobState = "FAILED"
)

type ImportJobStartResult struct {
	ProviderJobID string
	Message       string
}

type ImportJobStatusResult struct {
	State         ImportJobState
	ImportedUsers int
	FailedUsers   int
	Message       string
}

type ImportedUser struct {
	Username  string
	Email     string
	Name      string
	CognitoID string
}

// Service はジョブ処理が必要とする Cognito provider port。
type Service interface {
	Mode() string
	StartImport(ctx context.Context, users []model.BatchUser) (*ImportJobStartResult, error)
	DescribeImport(ctx context.Context, providerJobID string) (*ImportJobStatusResult, error)
	StopImport(ctx context.Context, providerJobID string) error
	DeleteUsers(ctx context.Context, usernames []string) error
	ResolveImportedUsers(ctx context.Context, usernames []string) ([]ImportedUser, error)
}
