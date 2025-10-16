# PDF to JPEG Conversion API

Go + go-fitz を用いた PDF→JPEG 変換 API サーバーです。Cloud Run 上での運用を前提に、1 ページ目を高速変換・API キー認証・ステートレス設計を満たしています。

## Features

- `POST /convert` でアップロードされた PDF の 1 ページ目を JPEG (品質 85) に変換
- 10MB までの `multipart/form-data` アップロードと X-API-Key トークン認証（静的・Firestore 一時キー双方に対応）
- 管理用エンドポイントで一時 API キーを発行 / 失効 / 状態確認し、使用回数と有効期限を Firestore で制御
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

1. `.env` を用意（必要なら編集）  
   ```bash
   cp .env.example .env
   # 必要に応じて API_KEYS / MASTER_API_KEYS / Firestore 設定を編集
   ```
2. サーバー起動  
   ```bash
   go run ./cmd
   # もしくは
   make install
   ./bin/main
   ```
3. 動作確認（必要なら `source .env` でシェルにも取り込む）  
   ```bash
   source .env
   curl -H "X-API-Key: ${API_KEYS}" \
        -F "file=@sample.pdf" \
        http://localhost:8080/convert \
        -o result.jpg

4. 一時キー発行（Firestore エミュレータ起動中の場合）
   ```bash
   curl -X POST http://localhost:8080/admin/api-keys \
        -H "X-Admin-Key: ${MASTER_API_KEYS}" \
        -H "Content-Type: application/json" \
        -d '{"label":"trial","usageLimit":3,"ttlMinutes":60}'
   ```
   ```

## Testing

```bash
make test   # 単体 + E2E テスト一括
make unit   # internal/* パッケージのみ
make e2e    # test/e2e_test.go のみ実行

# Firestore エミュレータを利用する場合（別ターミナル）
gcloud beta emulators firestore start --host-port=127.0.0.1:8200
# 以降のターミナルで
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8200
export FIRESTORE_PROJECT_ID=demo-project
make test
```

※ Firestore エミュレータを起動していない場合、Firestore 統合テストは自動的にスキップされます。

### Docker Compose (ローカル統合環境)

`docker compose up --build` を実行すると、以下が自動で立ち上がります。

- `firestore-emulator`: Firestore Emulator (`localhost:8200`)
- `app`: API サーバー (`localhost:8080`)
- `test`: Firestore Emulator の起動を待ってから `go test ./...` を実行（完了後に `EXIT 0` で停止）

コード変更後にテストのみ再実行したい場合は、以下が便利です。

```bash
docker compose run --rm test
```

コンテナ内では `FIRESTORE_PROJECT_ID=demo-project` と `FIRESTORE_EMULATOR_HOST=firestore-emulator:8080` が自動設定されます。ホストから直接テストを実行する場合は `.env` などで `FIRESTORE_EMULATOR_HOST=127.0.0.1:8200` を設定してください。

> NOTE: `docker compose up` 実行直後に `test exited with code 0` と表示されますが、テスト完了を示す正常な挙動です。
> 初回起動時は Firestore Emulator 用コンポーネントのインストールに数分かかります（`google-cloud-cli-firestore-emulator` / Temurin JRE 21 を自動導入）。

## Secrets & Configuration

- 必須環境変数と推奨設定方法
  | 変数 | 用途 | 推奨設定方法 |
  | --- | --- | --- |
  | `API_KEYS` | 静的クライアント用 API キー（カンマ区切り） | Secret Manager に保存し Cloud Run から参照 |
  | `MASTER_API_KEYS` | 管理エンドポイント用キー（カンマ区切り） | Secret Manager に保存し Cloud Run から参照 |
  | `ENABLE_FIRESTORE_KEYS` | Firestore を利用したキー検証の有効・無効 | 本番は `true`、ローリングバック時のみ `false` |
  | `FIRESTORE_PROJECT_ID` | Firestore を利用するプロジェクト ID | Cloud Run 環境変数。未指定時は `GOOGLE_CLOUD_PROJECT` を自動利用 |
  | `FIRESTORE_COLLECTION` | Firestore コレクション名 | 既定値 `apiKeys`。変更時のみ設定 |

- GCP 事前準備
  1. Firestore (Native モード) と Cloud Run API を有効化し、データベースを作成します。
     ```bash
     gcloud services enable run.googleapis.com firestore.googleapis.com --project ${PROJECT_ID}
     gcloud firestore databases create --project ${PROJECT_ID} --location asia-northeast1 --type=firestore-native
     ```
  2. Cloud Run 実行サービスアカウントに最小権限を付与します。
     ```bash
     PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')
     SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
     gcloud projects add-iam-policy-binding ${PROJECT_ID} \
       --member="serviceAccount:${SERVICE_ACCOUNT}" \
       --role="roles/datastore.user"
     gcloud projects add-iam-policy-binding ${PROJECT_ID} \
       --member="serviceAccount:${SERVICE_ACCOUNT}" \
       --role="roles/secretmanager.secretAccessor"
     ```
  3. Secret Manager に API キーと管理キーを登録します（実際には十分な長さのランダム値を使用）。
     ```bash
     CLIENT_KEYS="$(openssl rand -hex 32),$(openssl rand -hex 32)"
     ADMIN_KEYS="$(openssl rand -hex 32),$(openssl rand -hex 32)"
     printf '%s' "${CLIENT_KEYS}" | gcloud secrets create pdf2jpg-api-key --replication-policy=automatic --data-file=-
     printf '%s' "${ADMIN_KEYS}" | gcloud secrets create pdf2jpg-master-api-keys --replication-policy=automatic --data-file=-
     ```

- Cloud Run で指定する推奨オプション
  ```bash
  --set-secrets API_KEYS=projects/${PROJECT_ID}/secrets/pdf2jpg-api-key:latest,\
MASTER_API_KEYS=projects/${PROJECT_ID}/secrets/pdf2jpg-master-api-keys:latest \
  --set-env-vars ENABLE_FIRESTORE_KEYS=true,FIRESTORE_PROJECT_ID=${PROJECT_ID},FIRESTORE_COLLECTION=apiKeys
  ```

- GitHub Secrets（`.github/workflows/deploy.yml` 用）
  | Secret | 説明 |
  | --- | --- |
  | `GCP_PROJECT` | デプロイ対象の GCP プロジェクト ID |
  | `CLOUD_RUN_REGION` | 例 `asia-northeast3` |
  | `CLOUD_RUN_SERVICE` | Cloud Run サービス名 |
  | `ARTIFACT_REGISTRY_HOST` | 例 `asia-northeast3-docker.pkg.dev` |
  | `WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation プロバイダ名 |
  | `DEPLOYER_SERVICE_ACCOUNT` | デプロイに使用するサービスアカウント |
  | `CLOUD_RUN_API_SECRET_NAME` | Secret Manager 上の `API_KEYS` シークレット名 |
  | `CLOUD_RUN_MASTER_SECRET_NAME` | Secret Manager 上の `MASTER_API_KEYS` シークレット名 |

  GitHub Actions で `MASTER_API_KEYS` を渡す場合は、デプロイステップの `gcloud run deploy` に `--set-secrets MASTER_API_KEYS=${{ secrets.CLOUD_RUN_MASTER_SECRET_NAME }}:latest` を追記してください。

## Temporary API Key Management

- 管理エンドポイントは `X-Admin-Key` ヘッダ（`.env` の `MASTER_API_KEYS`）で保護されます。
- レート制限: 100 request/min/IP（テストでは調整可能）。

| Method | Path | 説明 |
| --- | --- | --- |
| `POST` | `/admin/api-keys` | 一時キー発行。`usageLimit`(1-1000) と `ttlMinutes`(15-10080) を指定。|
| `GET` | `/admin/api-keys/{key}` | キー状態の確認（`active`/`expired`/`exhausted`/`revoked`）。|
| `POST` | `/admin/api-keys/{key}/revoke` | 残り使用回数を 0 にし、即時失効。 |
| `POST` | `/admin/api-keys/cleanup` | (任意) 期限切れキーを最大 200 件削除。`limit` クエリで調整可。|

レスポンスにはメトリクス (`/debug/vars`) で確認可能な `api_key_issue_total`・`api_key_validation_total`・`temporary_keys_active` が更新されます。

### Secret Rotation & Verification

- 管理キーをローテーションする際は、Secret Manager に新しいバージョンを追加します。
  ```bash
  printf '%s' "$(openssl rand -hex 32),$(openssl rand -hex 32)" | \
    gcloud secrets versions add pdf2jpg-master-api-keys \
      --project ${PROJECT_ID} \
      --data-file=-
  ```
- Cloud Run が最新バージョンを読むように再デプロイします（既存の設定値も合わせて指定）。
  ```bash
  IMAGE=$(gcloud run services describe pdf2jpg-api \
    --project ${PROJECT_ID} \
    --region ${REGION} \
    --format='value(status.latestReadyRevision.spec.containers[0].image)')

  gcloud run deploy pdf2jpg-api \
    --project ${PROJECT_ID} \
    --region ${REGION} \
    --image "${IMAGE}" \
    --set-secrets API_KEYS=pdf2jpg-api-key:latest,MASTER_API_KEYS=pdf2jpg-master-api-keys:latest \
    --set-env-vars ENABLE_FIRESTORE_KEYS=true,FIRESTORE_PROJECT_ID=${PROJECT_ID},FIRESTORE_COLLECTION=apiKeys \
    --allow-unauthenticated
  ```
- 参照しているバージョンは次のコマンドで確認できます。
  ```bash
  REVISION=$(gcloud run services describe pdf2jpg-api \
    --project ${PROJECT_ID} \
    --region ${REGION} \
    --format='value(status.latestReadyRevisionName)')

  gcloud run revisions describe "${REVISION}" \
    --project ${PROJECT_ID} \
    --region ${REGION} \
    --format='value(spec.containers[0].env[?name=="MASTER_API_KEYS"].valueSource.secretKeyRef.version)'
  ```
- 管理 API (`/admin/api-keys` など) に新しいキーでアクセスして 200 が返り、旧キーが 401 になることを必ず確認してください。

## Docker Usage

```bash
docker build -t pdf2jpg:local .

docker run --rm -p 8080:8080 \
  -e API_KEYS=pdf2jpg-api-key-local-20251015 \
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

# Secret Manager に API キー / 管理キーを登録（初回のみ・値は必ず置き換える）
printf '%s' "$(openssl rand -hex 32),$(openssl rand -hex 32)" | \
  gcloud secrets create pdf2jpg-api-key --data-file=- --replication-policy=automatic
printf '%s' "$(openssl rand -hex 32),$(openssl rand -hex 32)" | \
  gcloud secrets create pdf2jpg-master-api-keys --data-file=- --replication-policy=automatic

# Cloud Run へデプロイ
gcloud run deploy pdf2jpg-api \
  --project "${PROJECT_ID}" \
  --region "${REGION}" \
  --image "${IMAGE}" \
  --allow-unauthenticated \
  --set-secrets API_KEYS=pdf2jpg-api-key:latest,MASTER_API_KEYS=pdf2jpg-master-api-keys:latest \
  --set-env-vars ENABLE_FIRESTORE_KEYS=true,FIRESTORE_PROJECT_ID=${PROJECT_ID},FIRESTORE_COLLECTION=apiKeys
```

### GitHub Actions

`.github/workflows/deploy.yml` により `main` ブランチへの push で自動デプロイできます。以下のシークレットを設定してください。

| Secret | Value |
| --- | --- |
| `GCP_PROJECT` | 例 `pdf2jpg-475117` |
| `CLOUD_RUN_REGION` | 例 `asia-northeast3` |
| `CLOUD_RUN_SERVICE` | 例 `pdf2jpg-api` |
| `ARTIFACT_REGISTRY_HOST` | 例 `asia-northeast3-docker.pkg.dev` |
| `WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation のプロバイダ名 |
| `DEPLOYER_SERVICE_ACCOUNT` | デプロイに利用するサービスアカウント |
| `CLOUD_RUN_API_SECRET_NAME` | Secret Manager 上の `API_KEYS` シークレット名 |
| `CLOUD_RUN_MASTER_SECRET_NAME` | Secret Manager 上の `MASTER_API_KEYS` シークレット名 |

## API Documentation

詳細は [docs/API_GUIDE.md](docs/API_GUIDE.md) を参照してください。

## Security Guidelines

API キー管理・Secret Manager・ログポリシーは [docs/SECURITY.md](docs/SECURITY.md) にまとめています。
