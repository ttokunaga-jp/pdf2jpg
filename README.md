# PDF to JPEG Conversion API

Go + go-fitz を用いた PDF→JPEG 変換 API サーバーです。Cloud Run 上での運用を前提に、1 ページ目を高速変換・API キー認証・ステートレス設計を満たしています。

## Features

- `POST /convert` でアップロードされた PDF の 1 ページ目を JPEG (品質 85) に変換
- 10MB までの `multipart/form-data` アップロードと X-API-Key トークン認証
- `/tmp` 配下の一時ファイルを処理後に必ず削除するステートレス設計
- Cloud Run / Docker / GitHub Actions による自動デプロイに対応

## Project Structure

```
.
├── cmd/                 # エントリーポイント
├── internal/
│   ├── auth/            # APIキー認証ミドルウェア
│   ├── handler/         # HTTPハンドラ（/convert）
│   ├── service/         # go-fitz を利用した変換ロジック
│   └── util/            # ファイル操作などの共通処理
├── docs/                # API / セキュリティドキュメント
├── test/                # E2E テスト
├── Dockerfile
└── Makefile
```

## Prerequisites

- Go 1.22 以上
- Docker 24 以上
- gcloud CLI（`gcloud init` 済み想定）
- GNU Make
- API キー（64 桁 16 進数などを Secret Manager に登録）

## Setup

```bash
git clone https://github.com/ttokunaga-jp/pdf2jpg.git
cd pdf2jpg

# 依存取得 & テスト
go mod tidy
make test
```

## Local Development

1. テスト用 API キーを環境変数に設定（カンマ区切りで複数指定可能）  
   ```bash
   export API_KEYS=59fd7eb3a9826c832643fcfb42f45cc4a8e677e6b4a35887f3b3a0ea9d59ab08
   ```
2. サーバー起動  
   ```bash
   go run ./cmd
   # もしくは
   make install
   ./bin/main
   ```
3. 動作確認  
   ```bash
   curl -H "X-API-Key: ${API_KEYS}" \
        -F "file=@sample.pdf" \
        http://localhost:8080/convert \
        -o result.jpg
   ```

## Testing

```bash
make test   # 単体 + E2E テスト一括
make unit   # internal/* パッケージのみ
make e2e    # test/e2e_test.go のみ実行
```

## Docker Usage

```bash
docker build -t pdf2jpg:local .

docker run --rm -p 8080:8080 \
  -e API_KEYS=59fd7eb3a9826c832643fcfb42f45cc4a8e677e6b4a35887f3b3a0ea9d59ab08 \
  pdf2jpg:local
```

## Cloud Run Deployment

### 手動デプロイ

```bash
PROJECT_ID=pdf2jpg-475117
REGION=asia-northeast3
IMAGE=asia-northeast3-docker.pkg.dev/${PROJECT_ID}/pdf2jpg/pdf2jpg:$(git rev-parse HEAD)

# Artifact Registry へビルド & プッシュ
gcloud builds submit --tag "${IMAGE}"

# Secret Manager に API キーを登録（初回のみ）
echo -n "59fd..." | gcloud secrets create pdf2jpg-api-key --data-file=- --replication-policy=automatic

# Cloud Run へデプロイ
gcloud run deploy pdf2jpg-api \
  --project "${PROJECT_ID}" \
  --region "${REGION}" \
  --image "${IMAGE}" \
  --allow-unauthenticated \
  --set-secrets API_KEYS=pdf2jpg-api-key:latest
```

### GitHub Actions

`.github/workflows/deploy.yml` により `main` ブランチへの push で自動デプロイできます。以下のシークレットを設定してください。

| Secret | Value |
| --- | --- |
| `GCP_PROJECT` | 例 `pdf2jpg-475117` |
| `CLOUD_RUN_REGION` | 例 `asia-northeast3` |
| `CLOUD_RUN_SERVICE` | 例 `pdf2jpg-api` |
| `WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation のプロバイダ名 |
| `DEPLOYER_SERVICE_ACCOUNT` | デプロイに利用するサービスアカウント |
| `ARTIFACT_REGISTRY_HOST` | 例 `asia-northeast3-docker.pkg.dev` |
| `CLOUD_RUN_API_SECRET_NAME` | 例 `pdf2jpg-api-key` |

## API Documentation

詳細は [docs/API_GUIDE.md](docs/API_GUIDE.md) を参照してください。

## Security Guidelines

API キー管理・Secret Manager・ログポリシーは [docs/SECURITY.md](docs/SECURITY.md) にまとめています。
