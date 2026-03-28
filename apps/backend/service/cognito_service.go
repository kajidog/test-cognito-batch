package service

import "context"

type CognitoCreateResult struct {
	RowNumber  int
	Email      string
	Name       string
	CognitoID  string
	ErrMessage string
}

type CognitoService interface {
	CreateUsers(ctx context.Context, users []BatchUser) ([]CognitoCreateResult, error)
}
