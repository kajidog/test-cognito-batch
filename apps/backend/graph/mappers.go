package graph

import (
	"cognito-batch-backend/db"
	"cognito-batch-backend/graph/model"
	"cognito-batch-backend/service"
)

func toGraphQLUsers(users []db.User) []*model.User {
	items := make([]*model.User, 0, len(users))
	for _, user := range users {
		items = append(items, toGraphQLUser(user))
	}
	return items
}

func toGraphQLUser(user db.User) *model.User {
	return &model.User{
		ID:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Name:      user.Name,
		CognitoID: user.CognitoID,
	}
}

func toGraphQLValidationResult(result *service.ValidationResult) *model.ValidationResult {
	rows := make([]*model.RowValidation, 0, len(result.Rows))
	for _, row := range result.Rows {
		errors := make([]*model.FieldError, 0, len(row.Errors))
		for _, fieldError := range row.Errors {
			errors = append(errors, &model.FieldError{
				Field:   fieldError.Field,
				Message: fieldError.Message,
			})
		}

		rows = append(rows, &model.RowValidation{
			RowNumber: row.RowNumber,
			Status:    model.ValidationRowStatus(row.Status),
			Errors:    errors,
		})
	}

	return &model.ValidationResult{
		Summary: &model.ValidationSummary{
			NewCount:    result.Summary.NewCount,
			UpdateCount: result.Summary.UpdateCount,
			ErrorCount:  result.Summary.ErrorCount,
		},
		Rows: rows,
	}
}

func toGraphQLJob(job db.Job) *model.Job {
	errors := make([]*model.JobError, 0, len(job.Errors))
	for _, jobError := range job.Errors {
		errors = append(errors, &model.JobError{
			ID:        jobError.ID,
			RowNumber: jobError.RowNumber,
			Name:      jobError.Name,
			Email:     jobError.Email,
			Message:   jobError.Message,
		})
	}

	return &model.Job{
		ID:              job.ID,
		Status:          model.JobStatus(job.Status),
		TotalCount:      job.TotalCount,
		ProcessedCount:  job.ProcessedCount,
		SuccessCount:    job.SuccessCount,
		FailureCount:    job.FailureCount,
		SourceObjectKey: job.SourceObjectKey,
		ExternalJobID:   job.ExternalJobID,
		StatusMessage:   job.StatusMessage,
		Errors:          errors,
	}
}
