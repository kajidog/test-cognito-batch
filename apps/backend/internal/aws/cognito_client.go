package awsclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cognito-batch-backend/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
)

// ImportJobStatus は Cognito User Import Job のステータス文字列。
type ImportJobStatus string

const (
	ImportJobStatusCreated    ImportJobStatus = "Created"
	ImportJobStatusPending    ImportJobStatus = "Pending"
	ImportJobStatusInProgress ImportJobStatus = "InProgress"
	ImportJobStatusStopping   ImportJobStatus = "Stopping"
	ImportJobStatusStopped    ImportJobStatus = "Stopped"
	ImportJobStatusSucceeded  ImportJobStatus = "Succeeded"
	ImportJobStatusExpired    ImportJobStatus = "Expired"
	ImportJobStatusFailed     ImportJobStatus = "Failed"
)

// ImportJobInfo は DescribeUserImportJob の結果。
type ImportJobInfo struct {
	Status            ImportJobStatus
	ImportedUsers     int64
	FailedUsers       int64
	SkippedUsers      int64
	CompletionMessage string
}

// UserInfo は AdminGetUser の結果。
type UserInfo struct {
	Username   string
	Attributes map[string]string // "email", "name", "sub" など
}

// CognitoClient は AWS Cognito User Pool API の薄ラッパー。
type CognitoClient struct {
	client     *cognitoidentityprovider.Client
	userPoolID string
	httpClient *http.Client
}

// NewCognitoClient は CognitoConfig から Cognito クライアントを初期化する。
func NewCognitoClient(cfg config.CognitoConfig) (*CognitoClient, error) {
	if cfg.Region == "" || cfg.UserPoolID == "" {
		return nil, fmt.Errorf("cognito config is incomplete: region and userPoolID are required")
	}

	loaders := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loaders = append(loaders, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loaders...)
	if err != nil {
		return nil, err
	}

	return &CognitoClient{
		client:     cognitoidentityprovider.NewFromConfig(awsCfg),
		userPoolID: cfg.UserPoolID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// GetCSVHeader は User Pool のスキーマに合った CSV ヘッダーを取得する。
func (c *CognitoClient) GetCSVHeader(ctx context.Context) ([]string, error) {
	output, err := c.client.GetCSVHeader(ctx, &cognitoidentityprovider.GetCSVHeaderInput{
		UserPoolId: aws.String(c.userPoolID),
	})
	if err != nil {
		return nil, err
	}
	return output.CSVHeader, nil
}

// CreateUserImportJob は import ジョブを作成し、ジョブ ID と presigned URL を返す。
func (c *CognitoClient) CreateUserImportJob(ctx context.Context, cloudWatchLogsRoleArn, jobName string) (jobID, preSignedURL string, err error) {
	output, err := c.client.CreateUserImportJob(ctx, &cognitoidentityprovider.CreateUserImportJobInput{
		CloudWatchLogsRoleArn: aws.String(cloudWatchLogsRoleArn),
		JobName:               aws.String(jobName),
		UserPoolId:            aws.String(c.userPoolID),
	})
	if err != nil {
		return "", "", err
	}
	if output.UserImportJob == nil || output.UserImportJob.JobId == nil || output.UserImportJob.PreSignedUrl == nil {
		return "", "", fmt.Errorf("cognito import job response is incomplete")
	}
	return *output.UserImportJob.JobId, *output.UserImportJob.PreSignedUrl, nil
}

// UploadToPreSignedURL は presigned URL に CSV を PUT アップロードする。
func (c *CognitoClient) UploadToPreSignedURL(ctx context.Context, preSignedURL string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, preSignedURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("x-amz-server-side-encryption", "aws:kms")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("cognito import upload failed: %s", strings.TrimSpace(string(payload)))
	}
	return nil
}

// StartUserImportJob は import 処理を開始する。ステータスメッセージを返す。
func (c *CognitoClient) StartUserImportJob(ctx context.Context, jobID string) (string, error) {
	output, err := c.client.StartUserImportJob(ctx, &cognitoidentityprovider.StartUserImportJobInput{
		JobId:      aws.String(jobID),
		UserPoolId: aws.String(c.userPoolID),
	})
	if err != nil {
		return "", err
	}
	message := "cognito import job started"
	if output.UserImportJob != nil && output.UserImportJob.Status != "" {
		message = fmt.Sprintf("cognito import %s", output.UserImportJob.Status)
	}
	return message, nil
}

// DescribeUserImportJob は import ジョブの現在の状態を取得する。
func (c *CognitoClient) DescribeUserImportJob(ctx context.Context, jobID string) (*ImportJobInfo, error) {
	output, err := c.client.DescribeUserImportJob(ctx, &cognitoidentityprovider.DescribeUserImportJobInput{
		JobId:      aws.String(jobID),
		UserPoolId: aws.String(c.userPoolID),
	})
	if err != nil {
		return nil, err
	}
	if output.UserImportJob == nil {
		return nil, fmt.Errorf("cognito import job not found")
	}

	return &ImportJobInfo{
		Status:            ImportJobStatus(output.UserImportJob.Status),
		ImportedUsers:     output.UserImportJob.ImportedUsers,
		FailedUsers:       output.UserImportJob.FailedUsers,
		SkippedUsers:      output.UserImportJob.SkippedUsers,
		CompletionMessage: aws.ToString(output.UserImportJob.CompletionMessage),
	}, nil
}

// AdminGetUser は username でユーザー属性を取得する。
func (c *CognitoClient) AdminGetUser(ctx context.Context, username string) (*UserInfo, error) {
	output, err := c.client.AdminGetUser(ctx, &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(c.userPoolID),
		Username:   aws.String(username),
	})
	if err != nil {
		return nil, err
	}

	attrs := make(map[string]string, len(output.UserAttributes))
	for _, attr := range output.UserAttributes {
		attrs[aws.ToString(attr.Name)] = aws.ToString(attr.Value)
	}

	return &UserInfo{
		Username:   username,
		Attributes: attrs,
	}, nil
}
