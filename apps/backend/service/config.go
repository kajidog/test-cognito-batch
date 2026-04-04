package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// S3Config は S3 互換ストレージへの接続設定。
type S3Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	KeyPrefix    string
	AccessKey    string
	SecretKey    string
	SessionToken string
}

// CognitoConfig は AWS Cognito User Pool への接続設定。
type CognitoConfig struct {
	Region                string
	UserPoolID            string
	CloudWatchLogsRoleArn string
	AccessKey             string
	SecretKey             string
}

// MockCognitoConfig はモック Cognito サービスの設定。
type MockCognitoConfig struct {
	StepDelay time.Duration
}

// JobConfig はバッチジョブ処理の設定。
type JobConfig struct {
	ProcessDelay time.Duration
	PollInterval time.Duration
}

// LoadS3Config は環境変数から S3 接続設定を読み込む。
// 認証情報は環境変数 → 認証ファイル の優先順で探索する。
func LoadS3Config() S3Config {
	accessKey, secretKey, sessionToken := loadS3Credentials()
	return S3Config{
		Endpoint:     strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		Region:       getEnvOrDefault("S3_REGION", "garage"),
		Bucket:       getEnvOrDefault("S3_BUCKET", "cognito-csv"),
		KeyPrefix:    strings.Trim(strings.TrimSpace(getEnvOrDefault("S3_KEY_PREFIX", "batch-jobs")), "/"),
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		SessionToken: sessionToken,
	}
}

// LoadCognitoConfig は環境変数から Cognito 接続設定を読み込む。
func LoadCognitoConfig() CognitoConfig {
	return CognitoConfig{
		Region:                strings.TrimSpace(os.Getenv("COGNITO_REGION")),
		UserPoolID:            strings.TrimSpace(os.Getenv("COGNITO_USER_POOL_ID")),
		CloudWatchLogsRoleArn: strings.TrimSpace(os.Getenv("COGNITO_CLOUDWATCH_LOGS_ROLE_ARN")),
		AccessKey:             strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
		SecretKey:             strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
	}
}

// LoadJobConfig は環境変数からジョブ処理設定を読み込む。
func LoadJobConfig() JobConfig {
	return JobConfig{
		ProcessDelay: parseDurationMs("JOB_STEP_DELAY_MS", 1500),
		PollInterval: parseDurationMs("COGNITO_IMPORT_POLL_INTERVAL_MS", 2000),
	}
}

func getEnvOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseDurationMs(envKey string, defaultMs int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw == "" {
		return time.Duration(defaultMs) * time.Millisecond
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return time.Duration(defaultMs) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// loadS3Credentials は S3 の認証情報を以下の優先順で探索する:
//  1. 環境変数 AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN
//  2. ファイルパス候補 (S3_CREDENTIALS_FILE, /s3-config/credentials.env, ../s3/credentials.env 等)
func loadS3Credentials() (string, string, string) {
	accessKey := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKey != "" && secretKey != "" {
		return accessKey, secretKey, strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN"))
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
			return accessKey, secretKey, strings.TrimSpace(values["AWS_SESSION_TOKEN"])
		}
	}

	return "", "", ""
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
