package service

import (
	"bytes"
	"cognito-batch-backend/model"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	awsclient "cognito-batch-backend/internal/aws"
	"cognito-batch-backend/internal/config"
)

// AwsCognitoService は AWS Cognito User Import Job API を使った CognitoService の本番実装。
//
// Cognito の import フロー:
//  1. GetCSVHeader → User Pool のスキーマに合った CSV ヘッダーを取得
//  2. CreateUserImportJob → import ジョブを作成し、presigned URL を取得
//  3. presigned URL に CSV を PUT アップロード
//  4. StartUserImportJob → import 処理を開始
//  5. DescribeUserImportJob → ポーリングで完了を検知
//  6. AdminGetUser → import されたユーザーの sub を取得
type AwsCognitoService struct {
	client                *awsclient.CognitoClient
	cloudWatchLogsRoleArn string
}

// NewAwsCognitoService は CognitoConfig から AWS クライアントを初期化する。
// Region, UserPoolID, CloudWatchLogsRoleArn は必須。
func NewAwsCognitoService(cfg config.CognitoConfig) (*AwsCognitoService, error) {
	if cfg.CloudWatchLogsRoleArn == "" {
		return nil, fmt.Errorf("cognito config is incomplete: cloudWatchLogsRoleArn is required")
	}

	client, err := awsclient.NewCognitoClient(cfg)
	if err != nil {
		return nil, err
	}

	return &AwsCognitoService{
		client:                client,
		cloudWatchLogsRoleArn: cfg.CloudWatchLogsRoleArn,
	}, nil
}

func (s *AwsCognitoService) Mode() string {
	return "aws-import"
}

// StartImport は Cognito User Import Job を作成・開始する。
func (s *AwsCognitoService) StartImport(ctx context.Context, users []model.BatchUser) (*ImportJobStartResult, error) {
	headers, err := s.client.GetCSVHeader(ctx)
	if err != nil {
		return nil, err
	}

	body, err := buildCognitoImportCSV(headers, users)
	if err != nil {
		return nil, err
	}

	jobName := "batch-import-" + time.Now().Format("20060102-150405")
	jobID, preSignedURL, err := s.client.CreateUserImportJob(ctx, s.cloudWatchLogsRoleArn, jobName)
	if err != nil {
		return nil, err
	}

	if err := s.client.UploadToPreSignedURL(ctx, preSignedURL, body); err != nil {
		return nil, err
	}

	message, err := s.client.StartUserImportJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	return &ImportJobStartResult{
		ProviderJobID: jobID,
		Message:       message,
	}, nil
}

// DescribeImport は Cognito のインポートジョブの現在の状態を取得する。
func (s *AwsCognitoService) DescribeImport(ctx context.Context, providerJobID string) (*ImportJobStatusResult, error) {
	info, err := s.client.DescribeUserImportJob(ctx, providerJobID)
	if err != nil {
		return nil, err
	}

	message := strings.TrimSpace(info.CompletionMessage)
	if message == "" {
		message = fmt.Sprintf("cognito import %s", info.Status)
	}

	return &ImportJobStatusResult{
		State:         mapImportJobStatus(info.Status),
		ImportedUsers: int(info.ImportedUsers),
		FailedUsers:   int(info.FailedUsers + info.SkippedUsers),
		Message:       message,
	}, nil
}

func (s *AwsCognitoService) StopImport(ctx context.Context, providerJobID string) error {
	return s.client.StopUserImportJob(ctx, providerJobID)
}

func (s *AwsCognitoService) DeleteUsers(ctx context.Context, usernames []string) error {
	var firstErr error
	seen := make(map[string]struct{}, len(usernames))
	for _, username := range usernames {
		username = strings.TrimSpace(username)
		if username == "" {
			continue
		}
		if _, ok := seen[username]; ok {
			continue
		}
		seen[username] = struct{}{}
		if err := s.client.AdminDeleteUser(ctx, username); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ResolveImportedUsers は import 完了後に username でユーザーを個別取得する。
func (s *AwsCognitoService) ResolveImportedUsers(ctx context.Context, usernames []string) ([]ImportedUser, error) {
	results := make([]ImportedUser, 0, len(usernames))
	for _, username := range usernames {
		info, err := s.client.AdminGetUser(ctx, username)
		if err != nil {
			continue
		}

		cognitoID := info.Attributes["sub"]
		if cognitoID == "" {
			continue
		}
		results = append(results, ImportedUser{
			Username:  username,
			Email:     info.Attributes["email"],
			Name:      info.Attributes["name"],
			CognitoID: cognitoID,
		})
	}
	return results, nil
}

// buildCognitoImportCSV は Cognito の User Pool スキーマに合わせた CSV を生成する。
func buildCognitoImportCSV(headers []string, users []model.BatchUser) ([]byte, error) {
	if len(headers) == 0 {
		return nil, fmt.Errorf("cognito csv header is empty")
	}

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	if err := writer.Write(headers); err != nil {
		return nil, err
	}

	for _, user := range users {
		record := make([]string, len(headers))
		for index, header := range headers {
			switch header {
			case "cognito:username", "username":
				record[index] = user.Username
			case "email":
				record[index] = user.Email
			case "name":
				record[index] = user.Name
			case "email_verified":
				record[index] = "true"
			}
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// mapImportJobStatus は Cognito のステータスをアプリ内部の 4 状態にマッピングする。
func mapImportJobStatus(status awsclient.ImportJobStatus) ImportJobState {
	switch status {
	case awsclient.ImportJobStatusCreated, awsclient.ImportJobStatusPending:
		return ImportJobStatePending
	case awsclient.ImportJobStatusInProgress, awsclient.ImportJobStatusStopping:
		return ImportJobStateRunning
	case awsclient.ImportJobStatusSucceeded:
		return ImportJobStateCompleted
	default:
		return ImportJobStateFailed
	}
}
