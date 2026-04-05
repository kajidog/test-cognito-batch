package repository

import (
	"cognito-batch-backend/db"
	"context"
	"time"

	"gorm.io/gorm"
)

// --- UserRepository ---

type GormUserRepository struct {
	db *gorm.DB
}

func NewGormUserRepository(database *gorm.DB) *GormUserRepository {
	return &GormUserRepository{db: database}
}

func (r *GormUserRepository) List(ctx context.Context) ([]db.User, error) {
	var users []db.User
	if err := r.db.WithContext(ctx).Order("username asc, name asc").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *GormUserRepository) GetByName(ctx context.Context, name string) (*db.User, error) {
	var user db.User
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *GormUserRepository) FindByUsernames(ctx context.Context, usernames []string) ([]db.User, error) {
	var users []db.User
	if err := r.db.WithContext(ctx).Where("username IN ?", usernames).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *GormUserRepository) GetByUsername(ctx context.Context, username string) (*db.User, error) {
	var user db.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *GormUserRepository) Create(ctx context.Context, user *db.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *GormUserRepository) DeleteByUsernames(ctx context.Context, usernames []string) error {
	if len(usernames) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("username IN ?", usernames).Delete(&db.User{}).Error
}

func (r *GormUserRepository) UpdateByUsername(ctx context.Context, username string, fields map[string]any) (int64, error) {
	result := r.db.WithContext(ctx).Model(&db.User{}).Where("username = ?", username).Updates(fields)
	return result.RowsAffected, result.Error
}

func (r *GormUserRepository) Save(ctx context.Context, user *db.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

// --- JobRepository ---

type GormJobRepository struct {
	db *gorm.DB
}

func NewGormJobRepository(database *gorm.DB) *GormJobRepository {
	return &GormJobRepository{db: database}
}

func (r *GormJobRepository) Create(ctx context.Context, job *db.Job) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *GormJobRepository) GetByID(ctx context.Context, id string) (*db.Job, error) {
	var job db.Job
	if err := r.db.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *GormJobRepository) GetByIDWithErrors(ctx context.Context, id string) (*db.Job, error) {
	var job db.Job
	err := r.db.WithContext(ctx).
		Preload("Errors", func(tx *gorm.DB) *gorm.DB {
			return tx.Order("row_number asc, created_at asc")
		}).
		First(&job, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *GormJobRepository) Save(ctx context.Context, job *db.Job) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *GormJobRepository) UpdateFields(ctx context.Context, id string, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&db.Job{}).Where("id = ?", id).Updates(fields).Error
}

func (r *GormJobRepository) CreateErrors(ctx context.Context, errors []db.JobError) error {
	if len(errors) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&errors).Error
}

// --- ImportQueueRepository ---

type GormImportQueueRepository struct {
	db *gorm.DB
}

func NewGormImportQueueRepository(database *gorm.DB) *GormImportQueueRepository {
	return &GormImportQueueRepository{db: database}
}

func (r *GormImportQueueRepository) Create(ctx context.Context, queue *db.CognitoImportQueue) error {
	return r.db.WithContext(ctx).Create(queue).Error
}

func (r *GormImportQueueRepository) FindDue(ctx context.Context, now time.Time) ([]db.CognitoImportQueue, error) {
	var queues []db.CognitoImportQueue
	if err := r.db.WithContext(ctx).
		Where("next_poll_at <= ?", now).
		Order("created_at asc").
		Find(&queues).Error; err != nil {
		return nil, err
	}
	return queues, nil
}

func (r *GormImportQueueRepository) FindByJobID(ctx context.Context, jobID string) (*db.CognitoImportQueue, error) {
	var queue db.CognitoImportQueue
	if err := r.db.WithContext(ctx).Where("job_id = ?", jobID).First(&queue).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &queue, nil
}

func (r *GormImportQueueRepository) ListActive(ctx context.Context) ([]db.CognitoImportQueue, error) {
	var queues []db.CognitoImportQueue
	if err := r.db.WithContext(ctx).
		Where("state IN ?", []db.ImportQueueState{db.ImportQueueStatePending, db.ImportQueueStateActive}).
		Order("created_at asc").
		Find(&queues).Error; err != nil {
		return nil, err
	}
	return queues, nil
}

func (r *GormImportQueueRepository) Save(ctx context.Context, queue *db.CognitoImportQueue) error {
	return r.db.WithContext(ctx).Save(queue).Error
}

func (r *GormImportQueueRepository) Delete(ctx context.Context, queue *db.CognitoImportQueue) error {
	return r.db.WithContext(ctx).Delete(queue).Error
}
