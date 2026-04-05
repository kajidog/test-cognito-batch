// awsclient パッケージ — AWS SDK の薄いラッパー。
// サービス層が AWS SDK を直接 import せずに済むようにする。
package awsclient

import (
	"bytes"
	"context"
	"fmt"

	"cognito-batch-backend/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client は S3 互換ストレージへのアクセスを提供する薄ラッパー。
type S3Client struct {
	client  *s3.Client
	initErr error
}

// NewS3Client は S3Config から S3 クライアントを初期化する。
// SDK 設定の読込に失敗した場合は initErr を保持し、PutObject 時にエラーを返す。
func NewS3Client(cfg config.S3Config) *S3Client {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return &S3Client{initErr: err}
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.UsePathStyle = true
		}
	})

	return &S3Client{client: client}
}

// PutObject は指定されたバケット/キーにデータをアップロードする。
func (c *S3Client) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	if c.client == nil {
		if c.initErr != nil {
			return fmt.Errorf("s3 client is not configured: %w", c.initErr)
		}
		return fmt.Errorf("s3 client is not configured")
	}

	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}
