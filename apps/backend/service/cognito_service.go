package service

import (
	"cognito-batch-backend/model"
	"context"
)

// ImportJobState は Cognito 側のインポートジョブの状態を抽象化した列挙型。
// AWS Cognito の UserImportJobStatusType をアプリ内部の 4 状態にマッピングする。
type ImportJobState string

const (
	ImportJobStatePending   ImportJobState = "PENDING"   // ジョブ作成済みだがまだ開始していない
	ImportJobStateRunning   ImportJobState = "RUNNING"   // インポート処理中
	ImportJobStateCompleted ImportJobState = "COMPLETED" // 正常完了
	ImportJobStateFailed    ImportJobState = "FAILED"    // 失敗
)

// ImportJobStartResult は StartImport の戻り値。
// ProviderJobID を保持し、以降の DescribeImport で状態を問い合わせるのに使う。
type ImportJobStartResult struct {
	ProviderJobID string // Cognito 側のジョブ ID (mock の場合は "mock-" プレフィクス付き UUID)
	Message       string // UI 表示用メッセージ
}

// ImportJobStatusResult は DescribeImport の戻り値。
// Cognito 側の進捗件数と状態を返す。
type ImportJobStatusResult struct {
	State         ImportJobState
	ImportedUsers int // 成功件数
	FailedUsers   int // 失敗件数 (Cognito の SkippedUsers も含む)
	Message       string
}

// ImportedUser は Cognito から取得したユーザー情報。
// ResolveImportedUsers で import 完了後に username をキーに取得する。
type ImportedUser struct {
	Username  string
	Email     string
	Name      string
	CognitoID string // Cognito の sub 属性
}

// CognitoService はユーザーインポートの provider インターフェース。
// 実装は以下の 2 種類:
//   - MockCognitoService: ローカル開発用。インメモリで即座にシミュレーション。
//   - AwsCognitoService:  本番用。AWS Cognito User Import Job API を使用。
type CognitoService interface {
	// Mode は実装の識別子を返す ("mock" or "aws-import")
	Mode() string
	// StartImport は新規ユーザーの Cognito import ジョブを開始する
	StartImport(ctx context.Context, users []model.BatchUser) (*ImportJobStartResult, error)
	// DescribeImport は実行中の import ジョブの状態を問い合わせる
	DescribeImport(ctx context.Context, providerJobID string) (*ImportJobStatusResult, error)
	// ResolveImportedUsers は import 完了後に username でユーザーを検索し、sub 等を取得する
	ResolveImportedUsers(ctx context.Context, usernames []string) ([]ImportedUser, error)
}
