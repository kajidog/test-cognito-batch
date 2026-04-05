package service

import (
	"cognito-batch-backend/db"
	"cognito-batch-backend/internal/repository"
	"context"
)

// UserService はローカル DB 上のユーザー CRUD を提供する。
// GraphQL の users / userByName クエリから利用される。
type UserService struct {
	userRepo repository.UserRepository
}

func NewUserService(userRepo repository.UserRepository) *UserService {
	return &UserService{userRepo: userRepo}
}

// List は全ユーザーを username 昇順で取得する。
func (s *UserService) List(ctx context.Context) ([]db.User, error) {
	return s.userRepo.List(ctx)
}

// GetByName は name でユーザーを検索する。見つからない場合は nil を返す。
func (s *UserService) GetByName(ctx context.Context, name string) (*db.User, error) {
	return s.userRepo.GetByName(ctx, name)
}
