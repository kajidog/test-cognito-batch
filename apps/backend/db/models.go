// db パッケージ — GORM モデル定義。SQLite 上のテーブルスキーマに対応する。
package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User はローカル DB 上のユーザーレコード。
// CSV アップロード時にまずここに保存され、Cognito import 完了後に CognitoID が紐付く。
type User struct {
	ID        string  `gorm:"type:text;primaryKey"`
	Email     string  `gorm:"not null"`
	Username  string  `gorm:"not null;default:'';index"` // Cognito との突合キー
	Name      string  `gorm:"not null"`
	CognitoID *string `gorm:"column:cognito_id"` // Cognito 側の sub (import 完了後にセット)
}

// JobStatus はバッチジョブのライフサイクル状態。
//
//	QUEUED → RUNNING → COMPLETED or FAILED
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"    // ジョブ作成直後。まだ処理が始まっていない。
	JobStatusRunning   JobStatus = "RUNNING"   // バリデーション・更新・Cognito import のいずれかが進行中。
	JobStatusCompleted JobStatus = "COMPLETED" // 全行の処理が完了 (一部失敗を含む場合もある)。
	JobStatusFailed    JobStatus = "FAILED"    // 復旧不能なエラーにより中断。
)

// Job はバッチ処理の進捗を追跡するレコード。
// フロントエンドは このレコードをポーリングして進捗バーや結果画面を表示する。
type Job struct {
	ID              string    `gorm:"type:text;primaryKey"`
	Status          JobStatus `gorm:"type:text;not null"`
	TotalCount      int       `gorm:"not null"`                  // CSV の総行数
	ProcessedCount  int       `gorm:"not null"`                  // 処理済み行数 (成功 + 失敗)
	SuccessCount    int       `gorm:"not null"`                  // 成功行数
	FailureCount    int       `gorm:"not null"`                  // 失敗行数
	SourceObjectKey *string   `gorm:"column:source_object_key"`  // S3 に保存した CSV のオブジェクトキー (監査用)
	ExternalJobID   *string   `gorm:"column:external_job_id"`    // Cognito UserImportJob の ID
	StatusMessage   *string   `gorm:"column:status_message"`     // 現在のステップを示すメッセージ (UI 表示用)
	Errors          []JobError                                    // 行ごとのエラー詳細 (HasMany)
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// JobError はバッチ処理中に発生した行単位のエラー。
// バリデーションエラー、更新失敗、Cognito import 失敗などを記録する。
type JobError struct {
	ID        string `gorm:"type:text;primaryKey"`
	JobID     string `gorm:"type:text;not null;index"`
	RowNumber int    `gorm:"not null"` // CSV の行番号 (ヘッダー行=1 なので 2 始まり)。0 はシステムエラー。
	Name      string `gorm:"not null"`
	Email     string `gorm:"not null"`
	Message   string `gorm:"not null"` // ユーザーに表示するエラーメッセージ
	CreatedAt time.Time
}

type ImportQueueState string

const (
	ImportQueueStatePending ImportQueueState = "PENDING" // キューに入った直後
	ImportQueueStateActive  ImportQueueState = "ACTIVE"  // Worker が少なくとも 1 回ポーリング済み
)

// CognitoImportQueue は Cognito import ジョブの非同期ポーリングを管理するキューテーブル。
//
// prepareBatch で Cognito import を開始した後、このテーブルにレコードを作成する。
// バックグラウンド Worker (StartWorker) が NextPollAt を見て定期的にポーリングし、
// Cognito 側のジョブが完了したらローカル DB を更新してキューレコードを削除する。
//
// PreImportFailureCount は Cognito import 開始前 (バリデーション・ローカル更新) で
// 発生した失敗件数を保持する。Worker は Job.TotalCount と Payload のユーザー数から
// import 前の処理済み件数を逆算し、Cognito の進捗と合算して Job 全体の進捗を算出する。
type CognitoImportQueue struct {
	ID                    string           `gorm:"type:text;primaryKey"`
	JobID                 string           `gorm:"type:text;not null;index"`     // 親の Job ID
	ProviderMode          string           `gorm:"type:text;not null"`           // "mock" or "aws-import"
	ProviderJobID         string           `gorm:"type:text;not null;index"`     // Cognito 側のジョブ ID
	State                 ImportQueueState `gorm:"type:text;not null"`
	Payload               string           `gorm:"type:text;not null"`           // JSON: import 対象ユーザー一覧 (完了後の resolve に使う)
	PreImportFailureCount int              `gorm:"not null"`                     // import 開始前の失敗件数
	NextPollAt            time.Time        `gorm:"not null;index"`              // 次にポーリングする時刻
	AttemptCount          int              `gorm:"not null"`                     // ポーリング試行回数
	CreatedAt             time.Time
	UpdatedAt             time.Time
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

func (q *CognitoImportQueue) BeforeCreate(_ *gorm.DB) error {
	if q.ID == "" {
		q.ID = uuid.NewString()
	}
	return nil
}
