// repository パッケージ — データアクセス層のインターフェースと実装。
package repository

import (
	"cognito-batch-backend/db"
	"context"
	"time"
)

// UserRepository はユーザーテーブルへのデータアクセスを定義する。
type UserRepository interface {
	List(ctx context.Context) ([]db.User, error)
	GetByName(ctx context.Context, name string) (*db.User, error)
	FindByUsernames(ctx context.Context, usernames []string) ([]db.User, error)
	GetByUsername(ctx context.Context, username string) (*db.User, error)
	Create(ctx context.Context, user *db.User) error
	UpdateByUsername(ctx context.Context, username string, fields map[string]any) (int64, error)
	Save(ctx context.Context, user *db.User) error
}

// JobRepository はジョブテーブルへのデータアクセスを定義する。
type JobRepository interface {
	Create(ctx context.Context, job *db.Job) error
	GetByID(ctx context.Context, id string) (*db.Job, error)
	GetByIDWithErrors(ctx context.Context, id string) (*db.Job, error)
	Save(ctx context.Context, job *db.Job) error
	UpdateFields(ctx context.Context, id string, fields map[string]any) error
	CreateErrors(ctx context.Context, errors []db.JobError) error
}

// ImportQueueRepository は Cognito import キューテーブルへのデータアクセスを定義する。
type ImportQueueRepository interface {
	Create(ctx context.Context, queue *db.CognitoImportQueue) error
	FindDue(ctx context.Context, now time.Time) ([]db.CognitoImportQueue, error)
	Save(ctx context.Context, queue *db.CognitoImportQueue) error
	Delete(ctx context.Context, queue *db.CognitoImportQueue) error
}
