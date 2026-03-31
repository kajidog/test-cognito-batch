# test-cognito-batch

Cognito アカウントを CSV から一括登録するサンプルアプリです。  
フロントエンドで CSV を読み込み、バックエンドでバリデーションしたうえで、一括登録ジョブを開始し、進捗ポーリングと結果表示まで確認できます。

## 構成

- Frontend: React, Vite, Apollo Client
- Backend: Go, gqlgen, GORM, SQLite
- Storage: S3 互換ストレージとして Garage
- Local Cognito: mock / AWS Cognito import job 切り替え
- Runtime: Docker Compose

## できること

- CSV をアップロードしてプレビュー表示
- バックエンドでバリデーション
- 新規 / 更新 / エラーの色分け表示
- 一括登録ジョブの開始
- 作成中画面での進捗ポーリング
- 完了画面での成功件数 / 失敗件数 / エラー行表示

画面フロー:

`/` → `/jobs/:jobId` → `/jobs/:jobId/complete`

## ディレクトリ構成

```text
test-cognito-batch/
├── docker-compose.yml
└── apps/
    ├── backend/   # Go + GraphQL + SQLite
    ├── frontend/  # React + Vite
    └── s3/        # Garage 設定
```

## 起動方法

前提:

- Docker / Docker Compose が使えること

起動:

```bash
docker compose up --build
```

起動後の URL:

- Frontend: http://localhost:5173
- Backend GraphQL: http://localhost:8080/graphql
- GraphQL Playground: http://localhost:8080/
- Garage S3 API: http://localhost:3900
- Garage Web: http://localhost:3902
- Garage Admin API: http://localhost:3903

## 開発用コマンド

フロントエンド build:

```bash
cd apps/frontend
npm run build
```

バックエンド build:

```bash
cd apps/backend
go build ./...
```

GraphQL コード生成:

```bash
cd apps/backend
go run github.com/99designs/gqlgen generate
```

## CSV 仕様

ヘッダー付き CSV を想定しています。以下の別名ヘッダーを受け付けます。

- `email`, `mail`, `メール`, `メールアドレス`
- `username`, `user_name`, `user id`, `ユーザー名`
- `name`, `名前`
- `cognitoId`, `cognito_id`, `cognito id`

主なバリデーション:

- メール形式
- username 必須
- username 形式チェック
- 名前 2-10 文字
- username / 名前の CSV 内重複チェック
- CSV 内重複チェック
- 既存 username との突合による `NEW` / `UPDATE` / `ERROR` 判定

## S3 / Garage について

バックエンドは以下の環境変数を見て S3 クライアントを初期化します。

- `S3_ENDPOINT`
- `S3_REGION`
- `S3_BUCKET`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `S3_CREDENTIALS_FILE`
- `JOB_STEP_DELAY_MS`
- `COGNITO_MODE`
- `COGNITO_IMPORT_POLL_INTERVAL_MS`

`docker-compose.yml` では backend に `S3_CREDENTIALS_FILE=/s3-config/credentials.env` を渡しています。  
一方で、このファイルは Compose 起動だけでは自動生成されません。Garage 連携まで有効にしたい場合は、`apps/s3/credentials.env` を用意するか、`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` を明示的に設定してください。

認証情報がない場合:

- バックエンドは起動できます
- CSV 検証までは動きます
- 一括登録ジョブの S3 アップロードで失敗します

## Cognito import mode

`COGNITO_MODE` で動作を切り替えます。

- `mock`: ローカルの疑似 import job を実行
- `aws-import`: Cognito の `CreateUserImportJob` / `StartUserImportJob` / `DescribeUserImportJob` を使う

`aws-import` に切り替える場合は以下も必要です。

- `COGNITO_REGION`
- `COGNITO_USER_POOL_ID`
- `COGNITO_CLOUDWATCH_LOGS_ROLE_ARN`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

動作概要:

- アプリ側の S3 には確認用 CSV を保存します
- Cognito import には job 作成後に返る presigned URL へ backend が CSV を upload します
- backend worker が DB queue を監視し、Cognito job 完了後に `username` で再検索して `sub` を `cognito_id` に保存します

## 使用する AWS API リファレンス

本アプリケーションが利用する AWS API の一覧です。

### Amazon S3

| API | 用途 | ドキュメント |
|-----|------|-------------|
| PutObject | 監査・デバッグ用 CSV のアップロード (Garage S3 互換ストレージ) | [PutObject - Amazon S3 API Reference](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html) |

### Amazon Cognito User Pools

| API | 用途 | ドキュメント |
|-----|------|-------------|
| GetCSVHeader | User Pool スキーマに対応する CSV ヘッダー (列順) の取得 | [GetCSVHeader - Amazon Cognito User Pools API Reference](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_GetCSVHeader.html) |
| CreateUserImportJob | import ジョブの作成と presigned URL の取得 | [CreateUserImportJob - Amazon Cognito User Pools API Reference](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_CreateUserImportJob.html) |
| StartUserImportJob | CSV アップロード後の import 処理開始 | [StartUserImportJob - Amazon Cognito User Pools API Reference](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_StartUserImportJob.html) |
| DescribeUserImportJob | import ジョブの状態・進捗のポーリング | [DescribeUserImportJob - Amazon Cognito User Pools API Reference](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_DescribeUserImportJob.html) |
| AdminGetUser | import 完了後に username から sub (CognitoID) を取得 | [AdminGetUser - Amazon Cognito User Pools API Reference](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_AdminGetUser.html) |

### AWS SDK

バックエンドは [AWS SDK for Go v2](https://aws.github.io/aws-sdk-go-v2/docs/) を使用しています。

- S3 クライアント: [`github.com/aws/aws-sdk-go-v2/service/s3`](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3)
- Cognito クライアント: [`github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider`](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider)

## ジョブ進捗の遅延設定

モックの一括登録処理は `JOB_STEP_DELAY_MS` で 1件ごとの待機時間を調整できます。

- 例: `JOB_STEP_DELAY_MS=1500`
- `0` を指定すると待機なし
- 未指定時は `1500ms`
