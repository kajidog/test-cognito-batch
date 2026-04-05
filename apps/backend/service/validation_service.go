package service

import (
	"cognito-batch-backend/db"
	"cognito-batch-backend/internal/repository"
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

// バリデーション用の正規表現パターン
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)    // 簡易的なメールアドレス形式チェック
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@+-]{3,64}$`)  // Cognito の username 制約に準拠

// ValidationStatus は各行のバリデーション結果を表す。
type ValidationStatus string

const (
	ValidationStatusNew    ValidationStatus = "NEW"    // DB に存在しない → Cognito import 対象
	ValidationStatusUpdate ValidationStatus = "UPDATE" // DB に既存 → ローカル更新のみ
	ValidationStatusError  ValidationStatus = "ERROR"  // バリデーションエラーあり → スキップ
)

type ValidationFieldError struct {
	Field   string // エラーのあるフィールド名 ("email", "username", "name")
	Message string // ユーザー向けエラーメッセージ (日本語)
}

type ValidationRow struct {
	RowNumber int                    // CSV の行番号 (2始まり)
	Status    ValidationStatus
	Errors    []ValidationFieldError // エラーがなければ空
}

type ValidationSummary struct {
	NewCount    int // 新規ユーザー数
	UpdateCount int // 更新対象ユーザー数
	ErrorCount  int // エラー行数
}

type ValidationResult struct {
	Summary ValidationSummary
	Rows    []ValidationRow
}

type ValidationService struct {
	userRepo repository.UserRepository
}

func NewValidationService(userRepo repository.UserRepository) *ValidationService {
	return &ValidationService{userRepo: userRepo}
}

// ValidateUsers は CSV から読み込んだユーザー一覧を一括バリデーションする。
//
// 処理の流れ:
//   1. CSV 内での重複チェック用にカウントマップを構築 (name, username)
//   2. DB から既存ユーザーを username で一括取得 (UPDATE/NEW の判定に使う)
//   3. 各行に対してフィールドバリデーション + 重複チェック + 存在チェックを実行
func (s *ValidationService) ValidateUsers(inputs []db.User) (*ValidationResult, error) {
	result := &ValidationResult{
		Rows: make([]ValidationRow, 0, len(inputs)),
	}

	// --- ステップ 1: CSV 内の重複検出用カウントマップ ---
	nameCounts := make(map[string]int, len(inputs))
	usernameCounts := make(map[string]int, len(inputs))
	usernames := make([]string, 0, len(inputs))
	seenUsernames := make(map[string]struct{}, len(inputs))

	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name != "" {
			nameCounts[name]++
		}

		username := strings.TrimSpace(input.Username)
		if username == "" {
			continue
		}

		usernameCounts[username]++
		if _, exists := seenUsernames[username]; exists {
			continue
		}

		seenUsernames[username] = struct{}{}
		usernames = append(usernames, username)
	}

	// --- ステップ 2: DB から既存ユーザーを一括取得 ---
	existingUsersByUsername := make(map[string]db.User, len(usernames))
	if len(usernames) > 0 {
		existingUsers, err := s.userRepo.FindByUsernames(context.Background(), usernames)
		if err != nil {
			return nil, err
		}

		for _, user := range existingUsers {
			existingUsersByUsername[user.Username] = user
		}
	}

	// --- ステップ 3: 各行のバリデーション ---
	for index, input := range inputs {
		email := strings.TrimSpace(input.Email)
		username := strings.TrimSpace(input.Username)
		name := strings.TrimSpace(input.Name)

		row := ValidationRow{
			RowNumber: index + 2,
			Status:    ValidationStatusNew,
			Errors:    make([]ValidationFieldError, 0, 4),
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

		if username == "" {
			row.Errors = append(row.Errors, ValidationFieldError{
				Field:   "username",
				Message: "username は必須です",
			})
		} else {
			if !usernamePattern.MatchString(username) {
				row.Errors = append(row.Errors, ValidationFieldError{
					Field:   "username",
					Message: "username は英数字と . _ @ + - のみ、3文字以上64文字以下で入力してください",
				})
			}

			if usernameCounts[username] > 1 {
				row.Errors = append(row.Errors, ValidationFieldError{
					Field:   "username",
					Message: "CSV内で username が重複しています",
				})
			}
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

		if _, exists := existingUsersByUsername[username]; exists {
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
