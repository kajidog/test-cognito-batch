package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID        string  `gorm:"type:text;primaryKey"`
	Email     string  `gorm:"not null"`
	Name      string  `gorm:"not null;uniqueIndex"`
	CognitoID *string `gorm:"column:cognito_id"`
}

type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
)

type Job struct {
	ID              string    `gorm:"type:text;primaryKey"`
	Status          JobStatus `gorm:"type:text;not null"`
	TotalCount      int       `gorm:"not null"`
	ProcessedCount  int       `gorm:"not null"`
	SuccessCount    int       `gorm:"not null"`
	FailureCount    int       `gorm:"not null"`
	SourceObjectKey *string   `gorm:"column:source_object_key"`
	Errors          []JobError
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type JobError struct {
	ID        string `gorm:"type:text;primaryKey"`
	JobID     string `gorm:"type:text;not null;index"`
	RowNumber int    `gorm:"not null"`
	Name      string `gorm:"not null"`
	Email     string `gorm:"not null"`
	Message   string `gorm:"not null"`
	CreatedAt time.Time
}

func (u *User) BeforeCreate(_ *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	return nil
}

func (j *Job) BeforeCreate(_ *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	return nil
}

func (e *JobError) BeforeCreate(_ *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	return nil
}
