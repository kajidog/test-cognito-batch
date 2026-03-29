package service

import (
	"cognito-batch-backend/model"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// mockImportJob はモック内でシミュレートされるインポートジョブの状態。
type mockImportJob struct {
	createdAt time.Time
	users     []model.BatchUser
	idsByUser map[string]string // username → 生成した CognitoID (sub)
}

// MockCognitoService はローカル開発用の CognitoService 実装。
// インメモリで import ジョブをシミュレートし、経過時間ベースで進捗を返す。
// 名前・email・username に "fail" を含むユーザーは意図的に失敗させる (テスト用)。
type MockCognitoService struct {
	mu        sync.Mutex
	jobs      map[string]*mockImportJob
	stepDelay time.Duration // 1 ユーザーあたりの処理時間 (進捗シミュレーション用)
}

func NewMockCognitoService() *MockCognitoService {
	return &MockCognitoService{
		jobs:      make(map[string]*mockImportJob),
		stepDelay: loadProcessDelay(),
	}
}

func (s *MockCognitoService) Mode() string {
	return "mock"
}

// StartImport はモックのインポートジョブを作成する。
// 各ユーザーに UUID を事前生成しておき、ResolveImportedUsers で返す。
// "fail" を含むユーザーには ID を生成しない (= import 失敗扱い)。
func (s *MockCognitoService) StartImport(_ context.Context, users []model.BatchUser) (*ImportJobStartResult, error) {
	providerJobID := "mock-" + uuid.NewString()
	idsByUser := make(map[string]string, len(users))
	for _, user := range users {
		if isMockFailure(user) {
			continue
		}
		idsByUser[user.Username] = uuid.NewString()
	}

	s.mu.Lock()
	s.jobs[providerJobID] = &mockImportJob{
		createdAt: time.Now(),
		users:     append([]model.BatchUser(nil), users...),
		idsByUser: idsByUser,
	}
	s.mu.Unlock()

	return &ImportJobStartResult{
		ProviderJobID: providerJobID,
		Message:       "mock import job created",
	}, nil
}

// DescribeImport は経過時間に基づいて進捗をシミュレートする。
// createdAt からの経過時間 / stepDelay で "処理済みユーザー数" を算出し、
// 全ユーザーが処理されたら COMPLETED を返す。
func (s *MockCognitoService) DescribeImport(_ context.Context, providerJobID string) (*ImportJobStatusResult, error) {
	s.mu.Lock()
	job := s.jobs[providerJobID]
	s.mu.Unlock()
	if job == nil {
		return &ImportJobStatusResult{
			State:   ImportJobStateFailed,
			Message: "mock import job not found",
		}, nil
	}

	total := len(job.users)
	if total == 0 {
		return &ImportJobStatusResult{
			State:         ImportJobStateCompleted,
			ImportedUsers: 0,
			FailedUsers:   0,
			Message:       "mock import completed",
		}, nil
	}

	stepDelay := s.stepDelay
	if stepDelay <= 0 {
		stepDelay = 10 * time.Millisecond
	}

	processed := int(time.Since(job.createdAt) / stepDelay)
	if processed > total {
		processed = total
	}

	imported := 0
	failed := 0
	for index := 0; index < processed; index++ {
		if isMockFailure(job.users[index]) {
			failed++
			continue
		}
		imported++
	}

	state := ImportJobStateRunning
	message := "mock import in progress"
	if processed == 0 {
		state = ImportJobStatePending
		message = "mock import queued"
	}
	if processed >= total {
		state = ImportJobStateCompleted
		message = "mock import completed"
	}

	return &ImportJobStatusResult{
		State:         state,
		ImportedUsers: imported,
		FailedUsers:   failed,
		Message:       message,
	}, nil
}

// ResolveImportedUsers は StartImport 時に事前生成した CognitoID を返す。
// isMockFailure で失敗扱いのユーザーは idsByUser に含まれないため、結果から除外される。
func (s *MockCognitoService) ResolveImportedUsers(_ context.Context, usernames []string) ([]ImportedUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make([]ImportedUser, 0, len(usernames))
	for _, job := range s.jobs {
		for _, user := range job.users {
			cognitoID, ok := job.idsByUser[user.Username]
			if !ok {
				continue
			}
			for _, username := range usernames {
				if username != user.Username {
					continue
				}
				results = append(results, ImportedUser{
					Username:  user.Username,
					Email:     user.Email,
					Name:      user.Name,
					CognitoID: cognitoID,
				})
				break
			}
		}
	}
	return results, nil
}

// isMockFailure はテスト用の失敗判定。name, email, username のいずれかに "fail" を含むと失敗。
func isMockFailure(user model.BatchUser) bool {
	normalized := strings.ToLower(user.Name + " " + user.Email + " " + user.Username)
	return strings.Contains(normalized, "fail")
}
