package service

import (
	"bytes"
	"cognito-batch-backend/model"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
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
	client                *cognitoidentityprovider.Client
	userPoolID            string
	cloudWatchLogsRoleArn string // Cognito が CloudWatch Logs に書き込むための IAM ロール ARN
	httpClient            *http.Client
}

// NewAwsCognitoService は環境変数から設定を読み取り、AWS クライアントを初期化する。
// 必須環境変数: COGNITO_REGION, COGNITO_USER_POOL_ID, COGNITO_CLOUDWATCH_LOGS_ROLE_ARN
func NewAwsCognitoService() (*AwsCognitoService, error) {
	region := strings.TrimSpace(os.Getenv("COGNITO_REGION"))
	userPoolID := strings.TrimSpace(os.Getenv("COGNITO_USER_POOL_ID"))
	logsRoleArn := strings.TrimSpace(os.Getenv("COGNITO_CLOUDWATCH_LOGS_ROLE_ARN"))
	if region == "" || userPoolID == "" || logsRoleArn == "" {
		return nil, fmt.Errorf("cognito env is incomplete")
	}

	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	accessKey := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKey != "" && secretKey != "" {
		loaders = append(loaders, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), loaders...)
	if err != nil {
		return nil, err
	}

	return &AwsCognitoService{
		client:                cognitoidentityprovider.NewFromConfig(cfg),
		userPoolID:            userPoolID,
		cloudWatchLogsRoleArn: logsRoleArn,
		httpClient:            &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *AwsCognitoService) Mode() string {
	return "aws-import"
}

// StartImport は Cognito User Import Job を作成・開始する。
// 手順: GetCSVHeader → CSV 構築 → CreateUserImportJob → presigned URL に CSV アップロード → StartUserImportJob
func (s *AwsCognitoService) StartImport(ctx context.Context, users []model.BatchUser) (*ImportJobStartResult, error) {
	// User Pool のスキーマから CSV ヘッダー (列順) を取得。
	// pool 設定に依存する列順をアプリ側でハードコードしないための処理。
	headersOutput, err := s.client.GetCSVHeader(ctx, &cognitoidentityprovider.GetCSVHeaderInput{
		UserPoolId: aws.String(s.userPoolID),
	})
	if err != nil {
		return nil, err
	}

	body, err := buildCognitoImportCSV(headersOutput.CSVHeader, users)
	if err != nil {
		return nil, err
	}

	createOutput, err := s.client.CreateUserImportJob(ctx, &cognitoidentityprovider.CreateUserImportJobInput{
		CloudWatchLogsRoleArn: aws.String(s.cloudWatchLogsRoleArn),
		JobName:               aws.String("batch-import-" + time.Now().Format("20060102-150405")),
		UserPoolId:            aws.String(s.userPoolID),
	})
	if err != nil {
		return nil, err
	}
	if createOutput.UserImportJob == nil || createOutput.UserImportJob.JobId == nil || createOutput.UserImportJob.PreSignedUrl == nil {
		return nil, fmt.Errorf("cognito import job response is incomplete")
	}

	// Cognito import は任意の S3 bucket を参照する方式ではなく、
	// job 作成時に返る presigned URL へ CSV を upload する。
	if err := s.uploadImportCSV(ctx, *createOutput.UserImportJob.PreSignedUrl, body); err != nil {
		return nil, err
	}

	startOutput, err := s.client.StartUserImportJob(ctx, &cognitoidentityprovider.StartUserImportJobInput{
		JobId:      createOutput.UserImportJob.JobId,
		UserPoolId: aws.String(s.userPoolID),
	})
	if err != nil {
		return nil, err
	}

	message := "cognito import job started"
	if startOutput.UserImportJob != nil && startOutput.UserImportJob.Status != "" {
		message = fmt.Sprintf("cognito import %s", startOutput.UserImportJob.Status)
	}

	return &ImportJobStartResult{
		ProviderJobID: *createOutput.UserImportJob.JobId,
		Message:       message,
	}, nil
}

// DescribeImport は Cognito のインポートジョブの現在の状態を取得する。
// Worker のポーリングから呼ばれ、ImportedUsers / FailedUsers / SkippedUsers を返す。
func (s *AwsCognitoService) DescribeImport(ctx context.Context, providerJobID string) (*ImportJobStatusResult, error) {
	output, err := s.client.DescribeUserImportJob(ctx, &cognitoidentityprovider.DescribeUserImportJobInput{
		JobId:      aws.String(providerJobID),
		UserPoolId: aws.String(s.userPoolID),
	})
	if err != nil {
		return nil, err
	}
	if output.UserImportJob == nil {
		return nil, fmt.Errorf("cognito import job not found")
	}

	status := output.UserImportJob.Status
	message := strings.TrimSpace(aws.ToString(output.UserImportJob.CompletionMessage))
	if message == "" {
		message = fmt.Sprintf("cognito import %s", status)
	}

	return &ImportJobStatusResult{
		State:         mapUserImportJobState(status),
		ImportedUsers: int(output.UserImportJob.ImportedUsers),
		FailedUsers:   int(output.UserImportJob.FailedUsers + output.UserImportJob.SkippedUsers),
		Message:       message,
	}, nil
}

// ResolveImportedUsers は import 完了後に username でユーザーを個別取得する。
// Cognito の import API は成功行ごとの sub を返さないため、
// AdminGetUser で 1 ユーザーずつ取得して CognitoID (sub) を収集する。
// 取得に失敗したユーザーはスキップされ、呼び出し元で "unresolved" として扱われる。
func (s *AwsCognitoService) ResolveImportedUsers(ctx context.Context, usernames []string) ([]ImportedUser, error) {
	results := make([]ImportedUser, 0, len(usernames))
	for _, username := range usernames {
		output, err := s.client.AdminGetUser(ctx, &cognitoidentityprovider.AdminGetUserInput{
			UserPoolId: aws.String(s.userPoolID),
			Username:   aws.String(username),
		})
		if err != nil {
			continue
		}

		item := ImportedUser{Username: username}
		for _, attribute := range output.UserAttributes {
			switch aws.ToString(attribute.Name) {
			case "email":
				item.Email = aws.ToString(attribute.Value)
			case "name":
				item.Name = aws.ToString(attribute.Value)
			case "sub":
				item.CognitoID = aws.ToString(attribute.Value)
			}
		}
		if item.CognitoID == "" {
			continue
		}
		results = append(results, item)
	}
	return results, nil
}

// uploadImportCSV は Cognito が返した presigned URL に CSV を PUT アップロードする。
func (s *AwsCognitoService) uploadImportCSV(ctx context.Context, presignedURL string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "text/csv")

	response, err := s.httpClient.Do(request)
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

// buildCognitoImportCSV は Cognito の User Pool スキーマに合わせた CSV を生成する。
// headers は GetCSVHeader で取得した列名リスト。アプリが扱わない列は空文字で出力する。
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
			// pool に定義されているがこのアプリでは扱わない列は空で流す。
			// 余計なダミー値を入れず、header 契約だけを満たす。
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

// mapUserImportJobState は Cognito のステータスをアプリ内部の 4 状態にマッピングする。
// Created/Pending → PENDING, InProgress/Stopping → RUNNING, Succeeded → COMPLETED, それ以外 → FAILED
func mapUserImportJobState(status cognitotypes.UserImportJobStatusType) ImportJobState {
	switch status {
	case cognitotypes.UserImportJobStatusTypeCreated, cognitotypes.UserImportJobStatusTypePending:
		return ImportJobStatePending
	case cognitotypes.UserImportJobStatusTypeInProgress, cognitotypes.UserImportJobStatusTypeStopping:
		return ImportJobStateRunning
	case cognitotypes.UserImportJobStatusTypeSucceeded:
		return ImportJobStateCompleted
	default:
		return ImportJobStateFailed
	}
}
