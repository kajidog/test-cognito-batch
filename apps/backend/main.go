// main パッケージ — Cognito バッチユーザー登録バックエンドのエントリーポイント。
//
// アプリケーション全体の処理フロー:
//   1. フロントエンドが CSV をアップロードし、GraphQL mutation (startBatchUpsert) を呼ぶ
//   2. バックエンドが CSV の各行をバリデーションし、既存ユーザーの更新 / 新規ユーザーの Cognito import を行う
//   3. Cognito import は非同期ジョブとして実行され、バックグラウンド Worker がポーリングで完了を検知する
//   4. フロントエンドは GraphQL query (job) でジョブの進捗・結果を取得する
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"cognito-batch-backend/db"
	"cognito-batch-backend/graph"
	"cognito-batch-backend/service"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
)

func main() {
	// --- 1. データベース初期化 ---
	// SQLite を使用。起動時に AutoMigrate でテーブルを自動作成する。
	database, err := db.NewDatabase("data/app.db")
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// --- 2. サービス層の組み立て ---
	userService := service.NewUserService(database)
	validationService := service.NewValidationService(database)

	s3Cfg := service.LoadS3Config()
	s3Service := service.NewS3Service(s3Cfg)

	jobCfg := service.LoadJobConfig()
	cognitoService, err := newCognitoService(jobCfg)
	if err != nil {
		log.Fatalf("failed to initialize cognito service: %v", err)
	}
	jobService := service.NewJobService(jobCfg, database, validationService, s3Service, cognitoService)

	// バックグラウンド Worker を起動。
	// 定期的に CognitoImportQueue テーブルをポーリングし、
	// Cognito 側のインポートジョブの完了を検知してローカル DB を更新する。
	jobService.StartWorker(context.Background())

	// --- 3. HTTP サーバー設定 ---
	r := chi.NewRouter()

	// フロントエンド (Vite dev server) からのリクエストを許可する CORS 設定
	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}).Handler)

	// GraphQL リゾルバに各サービスを注入
	resolver := &graph.Resolver{
		UserService:       userService,
		ValidationService: validationService,
		JobService:        jobService,
	}

	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	r.Handle("/graphql", srv)                                          // GraphQL API エンドポイント
	r.Handle("/", playground.Handler("GraphQL Playground", "/graphql")) // 開発用 Playground UI

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

// newCognitoService は環境変数 COGNITO_MODE に応じて Cognito サービスの実装を切り替える。
//   - "mock" (またはデフォルト): インメモリのモック実装。ローカル開発・テスト用。
//   - "aws-import":            実際の AWS Cognito User Import Job API を使う本番実装。
func newCognitoService(jobCfg service.JobConfig) (service.CognitoService, error) {
	mode := strings.TrimSpace(os.Getenv("COGNITO_MODE"))
	if mode == "" || mode == "mock" {
		return service.NewMockCognitoService(service.MockCognitoConfig{
			StepDelay: jobCfg.ProcessDelay,
		}), nil
	}
	if mode == "aws-import" {
		cogCfg := service.LoadCognitoConfig()
		return service.NewAwsCognitoService(cogCfg)
	}
	return nil, fmt.Errorf("unsupported COGNITO_MODE: %s", mode)
}
