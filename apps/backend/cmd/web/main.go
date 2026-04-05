// Web サーバーのエントリーポイント。
//
// GraphQL API を提供し、フロントエンドからのバッチ処理リクエストを受け付ける。
package main

import (
	"context"
	"log"
	"net/http"

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
	validationService := service.NewValidationService(userRepo, importQueueRepo)

	s3Cfg := config.LoadS3Config()
	s3Service := service.NewS3Service(s3Cfg)

	jobCfg := config.LoadJobConfig()
	cogCfg := config.LoadCognitoConfig()
	cognitoService, err := service.NewAwsCognitoService(cogCfg)
	if err != nil {
		log.Fatalf("failed to initialize cognito service: %v", err)
	}
	jobService := service.NewJobService(jobCfg, userRepo, jobRepo, importQueueRepo, validationService, s3Service, cognitoService)

	// --- 4. Worker の起動 ---
	w := worker.New(jobService, jobCfg.PollInterval)
	w.Start(context.Background())
	log.Println("Worker started in-process")

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
