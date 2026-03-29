package service

import (
	"bytes"
	"cognito-batch-backend/model"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Service は監査・デバッグ用の CSV アップロードを担当する。
// Cognito import 自体は Cognito 側の presigned URL を使うため、
// この S3 はアプリ独自のバックアップ/確認用途。
// docker-compose では Garage (S3 互換ストレージ) に接続する。
type S3Service struct {
	client    *s3.Client
	bucket    string
	keyPrefix string
}

// NewS3Service は環境変数から S3 の接続情報を読み取り、S3 クライアントを初期化する。
// 認証情報が見つからない場合は client=nil で返し、UploadCSV 時にエラーとなる。
func NewS3Service() *S3Service {
	endpoint := getEnvOrDefault("S3_ENDPOINT", "http://s3:3900")
	region := getEnvOrDefault("S3_REGION", "garage")
	bucket := getEnvOrDefault("S3_BUCKET", "cognito-csv")
	keyPrefix := strings.Trim(strings.TrimSpace(getEnvOrDefault("S3_KEY_PREFIX", "batch-jobs")), "/")

	accessKey, secretKey := loadS3Credentials()
	if accessKey == "" || secretKey == "" {
		return &S3Service{
			bucket:    bucket,
			keyPrefix: keyPrefix,
		}
	}

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return &S3Service{
			bucket:    bucket,
			keyPrefix: keyPrefix,
		}
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

	return &S3Service{
		client:    client,
		bucket:    bucket,
		keyPrefix: keyPrefix,
	}
}

// UploadCSV は新規ユーザー一覧を CSV 形式で S3 にアップロードする。
// オブジェクトキーは "{prefix}/{jobID}/new-users.csv" の形式。
func (s *S3Service) UploadCSV(ctx context.Context, jobID string, users []model.BatchUser) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("s3 credentials are not configured")
	}

	objectKey := fmt.Sprintf("%s/%s/new-users.csv", s.keyPrefix, jobID)

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	if err := writer.Write([]string{"email", "username", "name"}); err != nil {
		return "", err
	}

	for _, user := range users {
		if err := writer.Write([]string{user.Email, user.Username, user.Name}); err != nil {
			return "", err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(buffer.Bytes()),
		ContentType: aws.String("text/csv"),
	})
	if err != nil {
		return "", err
	}

	return objectKey, nil
}

func getEnvOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// loadS3Credentials は S3 の認証情報を以下の優先順で探索する:
//  1. 環境変数 AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
//  2. ファイルパス候補 (S3_CREDENTIALS_FILE, /s3-config/credentials.env, ../s3/credentials.env 等)
func loadS3Credentials() (string, string) {
	accessKey := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKey != "" && secretKey != "" {
		return accessKey, secretKey
	}

	for _, candidate := range credentialFileCandidates() {
		if candidate == "" {
			continue
		}

		values, err := parseEnvFile(candidate)
		if err != nil {
			continue
		}

		accessKey = strings.TrimSpace(values["AWS_ACCESS_KEY_ID"])
		secretKey = strings.TrimSpace(values["AWS_SECRET_ACCESS_KEY"])
		if accessKey != "" && secretKey != "" {
			return accessKey, secretKey
		}
	}

	return "", ""
}

func credentialFileCandidates() []string {
	return []string{
		strings.TrimSpace(os.Getenv("S3_CREDENTIALS_FILE")),
		"/s3-config/credentials.env",
		filepath.Clean("../s3/credentials.env"),
		filepath.Clean("../../apps/s3/credentials.env"),
	}
}

func parseEnvFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}

	return values, nil
}
