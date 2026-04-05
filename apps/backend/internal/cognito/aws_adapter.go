package cognito

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	awsclient "cognito-batch-backend/internal/aws"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/model"
)

// AWSAdapter は AWS Cognito User Import Job API を使った本番実装。
type AWSAdapter struct {
	client                *awsclient.CognitoClient
	cloudWatchLogsRoleArn string
}

func NewAWSAdapter(cfg config.CognitoConfig) (*AWSAdapter, error) {
	if cfg.CloudWatchLogsRoleArn == "" {
		return nil, fmt.Errorf("cognito config is incomplete: cloudWatchLogsRoleArn is required")
	}

	client, err := awsclient.NewCognitoClient(cfg)
	if err != nil {
		return nil, err
	}

	return &AWSAdapter{
		client:                client,
		cloudWatchLogsRoleArn: cfg.CloudWatchLogsRoleArn,
	}, nil
}

func (s *AWSAdapter) Mode() string {
	return "aws-import"
}

func (s *AWSAdapter) StartImport(ctx context.Context, users []model.BatchUser) (*ImportJobStartResult, error) {
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

func (s *AWSAdapter) DescribeImport(ctx context.Context, providerJobID string) (*ImportJobStatusResult, error) {
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

func (s *AWSAdapter) StopImport(ctx context.Context, providerJobID string) error {
	return s.client.StopUserImportJob(ctx, providerJobID)
}

func (s *AWSAdapter) DeleteUsers(ctx context.Context, usernames []string) error {
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

func (s *AWSAdapter) ResolveImportedUsers(ctx context.Context, usernames []string) ([]ImportedUser, error) {
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
