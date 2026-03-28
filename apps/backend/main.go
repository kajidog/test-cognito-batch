package main

import (
	"log"
	"net/http"

	"cognito-batch-backend/db"
	"cognito-batch-backend/graph"
	"cognito-batch-backend/service"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
)

func main() {
	database, err := db.NewDatabase("data/app.db")
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	userService := service.NewUserService(database)
	validationService := service.NewValidationService(database)
	s3Service := service.NewS3Service()
	cognitoService := service.NewMockCognitoService()
	jobService := service.NewJobService(database, validationService, s3Service, cognitoService)

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
