package service

import (
	"cognito-batch-backend/db"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type ValidationStatus string

const (
	ValidationStatusNew    ValidationStatus = "NEW"
	ValidationStatusUpdate ValidationStatus = "UPDATE"
	ValidationStatusError  ValidationStatus = "ERROR"
)

type ValidationFieldError struct {
	Field   string
	Message string
}

type ValidationRow struct {
	RowNumber int
	Status    ValidationStatus
	Errors    []ValidationFieldError
}

type ValidationSummary struct {
	NewCount    int
	UpdateCount int
	ErrorCount  int
}

type ValidationResult struct {
	Summary ValidationSummary
	Rows    []ValidationRow
}

type ValidationService struct {
	db *gorm.DB
}

func NewValidationService(database *gorm.DB) *ValidationService {
	return &ValidationService{db: database}
}

func (s *ValidationService) ValidateUsers(inputs []db.User) (*ValidationResult, error) {
	result := &ValidationResult{
		Rows: make([]ValidationRow, 0, len(inputs)),
	}

	nameCounts := make(map[string]int, len(inputs))
	names := make([]string, 0, len(inputs))
	seenNames := make(map[string]struct{}, len(inputs))

	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			continue
		}

		nameCounts[name]++
		if _, exists := seenNames[name]; exists {
			continue
		}

		seenNames[name] = struct{}{}
		names = append(names, name)
	}

	existingUsersByName := make(map[string]db.User, len(names))
	if len(names) > 0 {
		var existingUsers []db.User
		if err := s.db.Where("name IN ?", names).Find(&existingUsers).Error; err != nil {
			return nil, err
		}

		for _, user := range existingUsers {
			existingUsersByName[user.Name] = user
		}
	}

	for index, input := range inputs {
		email := strings.TrimSpace(input.Email)
		name := strings.TrimSpace(input.Name)

		row := ValidationRow{
			RowNumber: index + 2,
			Status:    ValidationStatusNew,
			Errors:    make([]ValidationFieldError, 0, 3),
		}

		if email == "" {
			row.Errors = append(row.Errors, ValidationFieldError{
				Field:   "email",
				Message: "メールアドレスは必須です",
			})
		} else if !emailPattern.MatchString(email) {
			row.Errors = append(row.Errors, ValidationFieldError{
				Field:   "email",
				Message: "メールアドレスの形式が不正です",
			})
		}

		if name == "" {
			row.Errors = append(row.Errors, ValidationFieldError{
				Field:   "name",
				Message: "名前は必須です",
			})
		} else {
			nameLength := utf8.RuneCountInString(name)
			if nameLength < 2 || nameLength > 10 {
				row.Errors = append(row.Errors, ValidationFieldError{
					Field:   "name",
					Message: "名前は2文字以上10文字以下で入力してください",
				})
			}

			if nameCounts[name] > 1 {
				row.Errors = append(row.Errors, ValidationFieldError{
					Field:   "name",
					Message: "CSV内で名前が重複しています",
				})
			}
		}

		if len(row.Errors) > 0 {
			row.Status = ValidationStatusError
			result.Summary.ErrorCount++
			result.Rows = append(result.Rows, row)
			continue
		}

		if _, exists := existingUsersByName[name]; exists {
			row.Status = ValidationStatusUpdate
			result.Summary.UpdateCount++
		} else {
			row.Status = ValidationStatusNew
			result.Summary.NewCount++
		}

		result.Rows = append(result.Rows, row)
	}

	return result, nil
}
