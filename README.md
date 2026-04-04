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
- AWS Cognito import を使う場合は `.env.example` を元に `.env` を作成し、AWS 側の設定を入れること

起動:

```bash
cp .env.example .env
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

## 環境変数のセットアップ

バックエンドは `docker-compose.yml` からルートの `.env` を読み込みます。

初回セットアップ:

```bash
cp .env.example .env
```

主な切り替えポイント:

- `COGNITO_MODE=mock`: AWS なしでローカルの疑似 import を使う
- `COGNITO_MODE=aws-import`: 実際の AWS Cognito User Import Job API を使う

`mock` で最低限必要なもの:

- 基本的には `.env.example` のままで起動可能
- Garage 連携まで使う場合は `apps/s3/credentials.env` を配置するか、S3 用認証情報を `.env` に設定する

`aws-import` で必須のもの:

- `COGNITO_REGION`
- `COGNITO_USER_POOL_ID`
- `COGNITO_CLOUDWATCH_LOGS_ROLE_ARN`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `S3_BUCKET`
- `S3_REGION`

重要な注意点:

- この実装では `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` を Cognito クライアントと S3 クライアントの両方で使います
- そのため `COGNITO_MODE=aws-import` で AWS 認証情報を入れる場合、通常は `S3_ENDPOINT` を空にして AWS S3 を使ってください
- `S3_ENDPOINT=http://s3:3900` のまま Garage を使う構成だと、Garage 側の認証情報と AWS 側の認証情報が一致しない限り S3 アップロードで失敗します

`aws-import` の設定例:

```env
COGNITO_MODE=aws-import

AWS_ACCESS_KEY_ID=YOUR_AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY=YOUR_AWS_SECRET_ACCESS_KEY

COGNITO_REGION=ap-northeast-1
COGNITO_USER_POOL_ID=ap-northeast-1_xxxxxxxxx
COGNITO_CLOUDWATCH_LOGS_ROLE_ARN=arn:aws:iam::123456789012:role/CognitoCloudWatchLogsRole

S3_ENDPOINT=
S3_REGION=ap-northeast-1
S3_BUCKET=your-batch-bucket
S3_KEY_PREFIX=batch-jobs
S3_CREDENTIALS_FILE=
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

## AWS セットアップ

`COGNITO_MODE=aws-import` を使う場合は、アプリ側の `.env` だけでは足りません。AWS 側で少なくとも以下を用意してください。

- Cognito User Pool
- Cognito import 用の CloudWatch Logs ロール
- 監査・デバッグ用 CSV 保存先の S3 bucket
- アプリが使う IAM user または IAM role

### 1. Cognito User Pool を確認する

README のサンプル CSV から作成される import データでは、少なくとも次の属性を使います。

- `username`
- `email`
- `name`
- `email_verified=true`

このアプリは `GetCSVHeader` を呼んで User Pool 側のスキーマに合う CSV ヘッダーを取得してから CSV を組み立てます。  
そのため、User Pool 側の属性定義が変わると import 用の列順も変わります。

確認項目:

- import 対象の User Pool ID を `.env` の `COGNITO_USER_POOL_ID` に設定する
- import 対象のリージョンを `.env` の `COGNITO_REGION` に設定する
- `email` と `name` を扱えるスキーマになっていることを確認する

### 2. Cognito が引き受ける CloudWatch Logs ロールを作る

`CreateUserImportJob` では `CloudWatchLogsRoleArn` が必須です。  
このロールは「アプリが引き受けるロール」ではなく、「Cognito サービスが引き受けて CloudWatch Logs に書き込むためのロール」です。

信頼ポリシー例:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "cognito-idp.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

権限ポリシー例:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "*"
    }
  ]
}
```

作成したロール ARN を `.env` の `COGNITO_CLOUDWATCH_LOGS_ROLE_ARN` に設定します。

### 3. アプリ実行用 IAM user / role に権限を付ける

このアプリを実行する IAM principal には、少なくとも次の 3 系統の権限が必要です。

- アプリ監査用 CSV を保存する S3 bucket へのアクセス
- Cognito import job を操作する権限
- Cognito に CloudWatch Logs ロールを渡すための `iam:PassRole`

最小構成に近い policy 例:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "BatchAuditS3",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:DeleteObject"
      ],
      "Resource": "arn:aws:s3:::your-batch-bucket/*"
    },
    {
      "Sid": "CognitoImportJob",
      "Effect": "Allow",
      "Action": [
        "cognito-idp:GetCSVHeader",
        "cognito-idp:CreateUserImportJob",
        "cognito-idp:StartUserImportJob",
        "cognito-idp:DescribeUserImportJob",
        "cognito-idp:AdminGetUser"
      ],
      "Resource": "arn:aws:cognito-idp:ap-northeast-1:123456789012:userpool/ap-northeast-1_xxxxxxxxx"
    },
    {
      "Sid": "PassLogsRoleToCognito",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": "arn:aws:iam::123456789012:role/CognitoCloudWatchLogsRole",
      "Condition": {
        "StringEquals": {
          "iam:PassedToService": "cognito-idp.amazonaws.com"
        }
      }
    }
  ]
}
```

運用メモ:

- `iam:PassRole` は `role/*` ではなく、実際に使うロール ARN に絞る方が安全です
- `AdminGetUser` は import 完了後に `sub` を引くために使います
- bucket 単位の操作も必要なら `s3:ListBucket` を `arn:aws:s3:::your-batch-bucket` に追加してください

### 4. `.env` の値を AWS 実リソースに合わせる

例:

```env
COGNITO_MODE=aws-import

AWS_ACCESS_KEY_ID=YOUR_AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY=YOUR_AWS_SECRET_ACCESS_KEY

COGNITO_REGION=ap-northeast-1
COGNITO_USER_POOL_ID=ap-northeast-1_xxxxxxxxx
COGNITO_CLOUDWATCH_LOGS_ROLE_ARN=arn:aws:iam::123456789012:role/CognitoCloudWatchLogsRole

S3_ENDPOINT=
S3_REGION=ap-northeast-1
S3_BUCKET=your-batch-bucket
S3_KEY_PREFIX=batch-jobs
S3_CREDENTIALS_FILE=
```

値の対応関係:

- `COGNITO_USER_POOL_ID`: import 先 User Pool の ID
- `COGNITO_REGION`: その User Pool が存在するリージョン
- `COGNITO_CLOUDWATCH_LOGS_ROLE_ARN`: 手順 2 で作ったロール ARN
- `S3_BUCKET`: アプリが監査用 CSV を保存する bucket

### 5. 起動して動作確認する

```bash
docker compose up --build
```

確認の順番:

1. フロントエンドから CSV をアップロードする
2. バリデーションが通ることを確認する
3. ジョブ開始後、バックエンドで import job が作成されることを確認する
4. 完了後、結果画面に成功件数と失敗件数が反映されることを確認する

バックエンド内部の AWS 処理の流れ:

1. `GetCSVHeader`
2. `CreateUserImportJob`
3. Cognito が返した presigned URL に CSV を `PUT`
4. `StartUserImportJob`
5. `DescribeUserImportJob`
6. `AdminGetUser`

注意:

- Cognito import は「任意の S3 bucket を直接指定する方式」ではありません
- `CreateUserImportJob` のレスポンスに含まれる presigned URL へ CSV をアップロードしてから `StartUserImportJob` を呼びます
- presigned URL への `PUT` では `x-amz-server-side-encryption: aws:kms` を付与する必要があります
- このアプリの `S3_BUCKET` は Cognito 取り込み元ではなく、アプリ側の監査・デバッグ用保存先です

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

## トラブルシューティング

### `AccessDeniedException: ... is not authorized to perform: cognito-idp:GetCSVHeader`

実行 IAM principal に `cognito-idp:GetCSVHeader` が付いていないか、policy の `Resource` が実際の User Pool ARN と一致していません。

確認ポイント:

- `.env` の `COGNITO_USER_POOL_ID` と policy の User Pool ARN が同じか
- 実行ユーザーに policy が本当にアタッチされているか
- 別 User Pool 用の ARN を誤って許可していないか

### `CreateUserImportJob` で失敗する

次を確認してください。

- `COGNITO_CLOUDWATCH_LOGS_ROLE_ARN` が設定されているか
- 実行ユーザーに `iam:PassRole` があるか
- `iam:PassedToService = cognito-idp.amazonaws.com` 条件が正しいか
- 指定ロールの信頼ポリシーで `cognito-idp.amazonaws.com` を許可しているか

### S3 アップロードで失敗する

次のどちらの構成かを確認してください。

- ローカル Garage を使う: `S3_ENDPOINT=http://s3:3900` と Garage 用認証情報を使う
- AWS S3 を使う: `S3_ENDPOINT=` にして、`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` と `S3_BUCKET` / `S3_REGION` を AWS 側に合わせる

`aws-import` モードでは通常、後者の AWS S3 構成を使う方が安全です。

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
