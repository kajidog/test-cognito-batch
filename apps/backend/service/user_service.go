package service

import (
	"cognito-batch-backend/db"

	"gorm.io/gorm"
)

// UserService はローカル DB 上のユーザー CRUD を提供する。
// GraphQL の users / userByName クエリや upsertUsers mutation から利用される。
type UserService struct {
	db *gorm.DB
}

func NewUserService(database *gorm.DB) *UserService {
	return &UserService{db: database}
}

// List は全ユーザーを username 昇順で取得する。
func (s *UserService) List() ([]db.User, error) {
	var users []db.User
	if err := s.db.Order("username asc, name asc").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// GetByName は name でユーザーを検索する。見つからない場合は nil を返す。
func (s *UserService) GetByName(name string) (*db.User, error) {
	var user db.User
	if err := s.db.Where("name = ?", name).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}
