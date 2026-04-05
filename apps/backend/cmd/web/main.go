// Web サーバーのエントリーポイント。
//
// GraphQL API を提供し、フロントエンドからのバッチ処理リクエストを受け付ける。
package main

import (
	"context"
	"log"
	"net/http"

	"cognito-batch-backend/internal/app"
	"cognito-batch-backend/web/graph"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
)

func main() {
	container := app.MustNewContainer(context.Background(), app.DatabasePath())

	r := chi.NewRouter()

	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}).Handler)

	resolver := &graph.Resolver{
		UserService:       container.UserService,
		ValidationService: container.ValidationService,
		JobService:        container.JobService,
	}

	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	r.Handle("/graphql", srv)
	r.Handle("/", playground.Handler("GraphQL Playground", "/graphql"))

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
