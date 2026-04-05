package service

import (
	"context"
	"strings"

	awsclient "cognito-batch-backend/internal/aws"
	"cognito-batch-backend/internal/config"
)

// S3Service は S3 互換ストレージへのアップロードを担当する。
// ユーザーデータや CSV フォーマットについての知識は持たず、
// 与えられたバイト列をそのままアップロードする純粋なストレージサービス。
type S3Service struct {
	client    *awsclient.S3Client
	bucket    string
	keyPrefix string
}

// NewS3Service は S3Config から S3 クライアントを初期化する。
func NewS3Service(cfg config.S3Config) *S3Service {
	return &S3Service{
		client:    awsclient.NewS3Client(cfg),
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
	return s.client.PutObject(ctx, s.bucket, objectKey, data, contentType)
}

// Delete は指定されたオブジェクトキーを削除する。
func (s *S3Service) Delete(ctx context.Context, objectKey string) error {
	return s.client.DeleteObject(ctx, s.bucket, objectKey)
}
