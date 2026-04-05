// Worker のエントリーポイント。
//
// 本番環境で Web サーバ���とは別のコンテナとしてデプロイされる。
// CognitoImportQueue テーブルを定期的にポーリングし、
// Cognito 側のインポートジョブの完了を検知してローカル DB を更新する。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"cognito-batch-backend/db"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/internal/repository"
	"cognito-batch-backend/service"
	"cognito-batch-backend/worker"
)

func main() {
	// --- 1. データベース初期化 ---
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/app.db"
	}
	database, err := db.NewDatabase(dbPath)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// --- 2. リポジトリ層の組み立て ---
	userRepo := repository.NewGormUserRepository(database)
	jobRepo := repository.NewGormJobRepository(database)
	importQueueRepo := repository.NewGormImportQueueRepository(database)

	// --- 3. サービス層の組み立て ---
	validationService := service.NewValidationService(userRepo)

	s3Cfg := config.LoadS3Config()
	s3Service := service.NewS3Service(s3Cfg)

	jobCfg := config.LoadJobConfig()
	cogCfg := config.LoadCognitoConfig()
	cognitoService, err := service.NewAwsCognitoService(cogCfg)
	if err != nil {
		log.Fatalf("failed to initialize cognito service: %v", err)
	}

	jobService := service.NewJobService(jobCfg, userRepo, jobRepo, importQueueRepo, validationService, s3Service, cognitoService)

	// --- 4. Worker 起動 ---
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	w := worker.New(jobService, jobCfg.PollInterval)
	w.Start(ctx)

	log.Println("Worker started")
	<-ctx.Done()
	log.Println("Worker shutting down")
}
