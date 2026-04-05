// Web サーバーのエントリーポイント。
//
// GraphQL API を提供し、フロントエンドからのバッチ処理リクエストを受け付ける。
// COGNITO_MODE=mock (デフォルト) の場合は Worker もインプロセスで起動する
// (MockCognitoService がインメモリで状態を持つため同一プロセスで動作する必要がある)。
// COGNITO_MODE=aws-import の場合は Worker を起動しない (別コンテナで実行される想定)。
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"cognito-batch-backend/db"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/internal/repository"
	"cognito-batch-backend/service"
	"cognito-batch-backend/web/graph"
	"cognito-batch-backend/worker"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
)

func main() {
	// --- 1. データベース初期化 ---
	database, err := db.NewDatabase("data/app.db")
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// --- 2. リポジトリ層の組み立て ---
	userRepo := repository.NewGormUserRepository(database)
	jobRepo := repository.NewGormJobRepository(database)
	importQueueRepo := repository.NewGormImportQueueRepository(database)

	// --- 3. サービス層の組み立て ---
	userService := service.NewUserService(userRepo)
	validationService := service.NewValidationService(userRepo)

	s3Cfg := config.LoadS3Config()
	s3Service := service.NewS3Service(s3Cfg)

	jobCfg := config.LoadJobConfig()
	cognitoService, err := newCognitoService(jobCfg)
	if err != nil {
		log.Fatalf("failed to initialize cognito service: %v", err)
	}
	jobService := service.NewJobService(jobCfg, userRepo, jobRepo, importQueueRepo, validationService, s3Service, cognitoService)

	// --- 4. Worker の起動 (mock モード時のみ) ---
	// MockCognitoService はインメモリで状態を持つため、同一プロセスで Worker を動かす必要がある。
	// aws-import モードでは Worker は別コンテナで実行されるため、ここでは起動しない。
	mode := strings.TrimSpace(os.Getenv("COGNITO_MODE"))
	if mode == "" || mode == "mock" {
		w := worker.New(jobService, jobCfg.PollInterval)
		w.Start(context.Background())
		log.Println("Worker started in-process (mock mode)")
	}

	// --- 5. HTTP サーバー設定 ---
	r := chi.NewRouter()

	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}).Handler)

	resolver := &graph.Resolver{
		UserService:       userService,
		ValidationService: validationService,
		JobService:        jobService,
	}

	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	r.Handle("/graphql", srv)
	r.Handle("/", playground.Handler("GraphQL Playground", "/graphql"))

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

// newCognitoService は環境変数 COGNITO_MODE に応じて Cognito サービスの実装を切り替える。
func newCognitoService(jobCfg config.JobConfig) (service.CognitoService, error) {
	mode := strings.TrimSpace(os.Getenv("COGNITO_MODE"))
	if mode == "" || mode == "mock" {
		return service.NewMockCognitoService(config.MockCognitoConfig{
			StepDelay: jobCfg.ProcessDelay,
		}), nil
	}
	if mode == "aws-import" {
		cogCfg := config.LoadCognitoConfig()
		return service.NewAwsCognitoService(cogCfg)
	}
	return nil, fmt.Errorf("unsupported COGNITO_MODE: %s", mode)
}
