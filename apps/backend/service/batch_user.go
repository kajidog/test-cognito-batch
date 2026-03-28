package service

// BatchUser は CSV の 1 行分のデータを表す。
// バリデーション、S3 アップロード、Cognito import の各フェーズで共通して使われる。
// RowNumber は CSV のヘッダー行を 1 として 2 から始まる行番号で、エラー表示に使う。
type BatchUser struct {
	RowNumber int
	Email     string
	Username  string
	Name      string
}
