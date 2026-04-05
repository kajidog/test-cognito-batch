// Worker のエントリーポイント。
//
// 本番環境で Web サーバ���とは別のコンテナとしてデプロイされる。
// CognitoImportQueue テーブルを定期的にポーリングし、
// Cognito 側のインポートジョブの完了を検知してローカル DB を更新する。
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"cognito-batch-backend/internal/app"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	container := app.MustNewContainer(ctx, app.DatabasePath())
	w := container.Worker
	if w == nil {
		log.Fatal("worker is not configured")
	}
	w.Start(ctx)

	log.Println("Worker started")
	<-ctx.Done()
	log.Println("Worker shutting down")
}
