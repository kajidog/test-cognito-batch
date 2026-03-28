package db

import (
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NewDatabase は SQLite データベースを初期化し、全テーブルの AutoMigrate を実行する。
// path で指定したファイルが存在しない場合は自動で作成される。
func NewDatabase(path string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 起動時にスキーマを自動マイグレーション。
	// テーブルが存在しなければ作成し、カラムが不足していれば追加する。
	if err := database.AutoMigrate(&User{}, &Job{}, &JobError{}, &CognitoImportQueue{}); err != nil {
		return nil, err
	}

	return database, nil
}
