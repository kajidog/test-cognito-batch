package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Service は S3 互換ストレージへのアップロードを担当する。
// ユーザーデータや CSV フォーマットについての知識は持たず、
// 与えられたバイト列をそのままアップロードする純粋なストレージサービス。
type S3Service struct {
	client    *s3.Client
	bucket    string
	keyPrefix string
	initErr   error
}

// NewS3Service は S3Config から S3 クライアントを初期化する。
// AWS SDK 設定の読込に失敗した場合は client=nil で返し、Upload 時にエラーとなる。
func NewS3Service(cfg S3Config) *S3Service {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return &S3Service{
			bucket:    cfg.Bucket,
			keyPrefix: cfg.KeyPrefix,
			initErr:   err,
		}
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.UsePathStyle = true
		}
	})

	return &S3Service{
		client:    client,
		bucket:    cfg.Bucket,
		keyPrefix: cfg.KeyPrefix,
	}
}

// ObjectKey は keyPrefix と指定されたパーツを結合してオブジェクトキーを生成する。
func (s *S3Service) ObjectKey(parts ...string) string {
	elements := append([]string{s.keyPrefix}, parts...)
	return strings.Join(elements, "/")
}

// Upload は指定されたオブジェクトキーにデータをアップロードする。
func (s *S3Service) Upload(ctx context.Context, objectKey string, data []byte, contentType string) error {
	if s.client == nil {
		if s.initErr != nil {
			return fmt.Errorf("s3 client is not configured: %w", s.initErr)
		}
		return fmt.Errorf("s3 client is not configured")
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}
