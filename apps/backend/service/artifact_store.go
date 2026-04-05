package service

import (
	"context"
	"strings"

	awsclient "cognito-batch-backend/internal/aws"
	"cognito-batch-backend/internal/config"
)

// JobArtifactStore はバッチ処理の補助ファイル保存先を抽象化する。
type JobArtifactStore interface {
	ObjectKey(parts ...string) string
	Upload(ctx context.Context, objectKey string, data []byte, contentType string) error
	Delete(ctx context.Context, objectKey string) error
}

// S3JobArtifactStore は S3 互換ストレージへの保存を担当する。
type S3JobArtifactStore struct {
	client    *awsclient.S3Client
	bucket    string
	keyPrefix string
}

func NewS3JobArtifactStore(client *awsclient.S3Client, cfg config.S3Config) *S3JobArtifactStore {
	return &S3JobArtifactStore{
		client:    client,
		bucket:    cfg.Bucket,
		keyPrefix: cfg.KeyPrefix,
	}
}

func (s *S3JobArtifactStore) ObjectKey(parts ...string) string {
	elements := append([]string{s.keyPrefix}, parts...)
	return strings.Join(elements, "/")
}

func (s *S3JobArtifactStore) Upload(ctx context.Context, objectKey string, data []byte, contentType string) error {
	return s.client.PutObject(ctx, s.bucket, objectKey, data, contentType)
}

func (s *S3JobArtifactStore) Delete(ctx context.Context, objectKey string) error {
	return s.client.DeleteObject(ctx, s.bucket, objectKey)
}
