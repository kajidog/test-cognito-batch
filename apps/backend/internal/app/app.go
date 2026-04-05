package app

import (
	"context"
	"log"
	"os"

	"cognito-batch-backend/db"
	awsclient "cognito-batch-backend/internal/aws"
	cognitoport "cognito-batch-backend/internal/cognito"
	"cognito-batch-backend/internal/config"
	"cognito-batch-backend/internal/repository"
	"cognito-batch-backend/service"
	"cognito-batch-backend/worker"
)

type Container struct {
	UserService       *service.UserService
	ValidationService *service.ValidationService
	JobService        *service.JobService
	Worker            *worker.Worker
}

func NewContainer(ctx context.Context, databasePath string) (*Container, error) {
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		return nil, err
	}

	userRepo := repository.NewGormUserRepository(database)
	jobRepo := repository.NewGormJobRepository(database)
	importQueueRepo := repository.NewGormImportQueueRepository(database)

	s3Cfg := config.LoadS3Config()
	cogCfg := config.LoadCognitoConfig()
	jobCfg := config.LoadJobConfig()

	s3Client := awsclient.NewS3Client(s3Cfg)
	artifactStore := service.NewS3JobArtifactStore(s3Client, s3Cfg)
	cognitoService, err := cognitoport.NewAWSAdapter(cogCfg)
	if err != nil {
		return nil, err
	}

	validationService := service.NewValidationService(userRepo, importQueueRepo)
	jobService := service.NewJobService(
		jobCfg,
		userRepo,
		jobRepo,
		importQueueRepo,
		validationService,
		artifactStore,
		cognitoService,
	)

	return &Container{
		UserService:       service.NewUserService(userRepo),
		ValidationService: validationService,
		JobService:        jobService,
		Worker:            worker.New(jobService, jobCfg.PollInterval),
	}, nil
}

func DatabasePath() string {
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/app.db"
	}
	return dbPath
}

func MustNewContainer(ctx context.Context, databasePath string) *Container {
	container, err := NewContainer(ctx, databasePath)
	if err != nil {
		log.Fatalf("failed to initialize application: %v", err)
	}
	return container
}
