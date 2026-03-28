package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type MockCognitoService struct{}

func NewMockCognitoService() *MockCognitoService {
	return &MockCognitoService{}
}

func (s *MockCognitoService) CreateUsers(_ context.Context, users []BatchUser) ([]CognitoCreateResult, error) {
	results := make([]CognitoCreateResult, 0, len(users))

	for _, user := range users {
		result := CognitoCreateResult{
			RowNumber: user.RowNumber,
			Email:     user.Email,
			Name:      user.Name,
		}

		normalized := strings.ToLower(user.Name + " " + user.Email)
		if strings.Contains(normalized, "fail") {
			result.ErrMessage = "mock cognito error"
		} else {
			result.CognitoID = uuid.NewString()
		}

		results = append(results, result)
	}

	return results, nil
}
