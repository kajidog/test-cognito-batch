package service

import (
	"cognito-batch-backend/db"

	"gorm.io/gorm"
)

type UserService struct {
	db *gorm.DB
}

func NewUserService(database *gorm.DB) *UserService {
	return &UserService{db: database}
}

func (s *UserService) List() ([]db.User, error) {
	var users []db.User
	if err := s.db.Order("name asc").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

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

func (s *UserService) UpsertUsers(inputs []db.User) ([]db.User, error) {
	users := make([]db.User, 0, len(inputs))

	for _, input := range inputs {
		user, err := s.GetByName(input.Name)
		if err != nil {
			return nil, err
		}

		if user == nil {
			user = &db.User{
				Email:     input.Email,
				Name:      input.Name,
				CognitoID: input.CognitoID,
			}
			if err := s.db.Create(user).Error; err != nil {
				return nil, err
			}
		} else {
			user.Email = input.Email
			user.CognitoID = input.CognitoID
			if err := s.db.Save(user).Error; err != nil {
				return nil, err
			}
		}

		users = append(users, *user)
	}

	return users, nil
}
