# Security Guide

## 1. API キー認証

- 本サービスはリクエストヘッダ `X-API-Key` に設定したトークンで認証します。
- サーバー側では `API_KEYS` 環境変数（カンマ区切り）で常時有効なキーを定義し、`MASTER_API_KEYS` で管理者操作用キーを定義します。
- 一時キーは Firestore に保存され、`remaining_usage`・`expires_at`・`revoked_at` で利用制限を管理します。
- ローカル開発時は `.env` などを利用し、公開リポジトリ内に平文で置かないよう注意してください。

### サンプル

```bash
cp .env.example .env
# 必要に応じて .env の API_KEYS を編集
source .env
curl -H "X-API-Key: ${API_KEYS}" \
     -F "file=@sample.pdf" http://localhost:8080/convert

# 一時キー発行（管理者用）
curl -X POST http://localhost:8080/admin/api-keys \
     -H "X-Admin-Key: ${MASTER_API_KEYS}" \
     -H "Content-Type: application/json" \
     -d '{"label":"trial","usageLimit":3,"ttlMinutes":60}'
```

## 2. Secret Manager 利用手順

Cloud Run では Secret Manager に格納した API キーを環境変数へバインドします。

1. **シークレット登録**
   ```bash
   echo -n "<YOUR_API_KEY>" | gcloud secrets create pdf2jpg-api-key --data-file=- --replication-policy=automatic
   ```
2. **Cloud Run 実行サービスアカウントへの権限付与**
   ```bash
   CLOUD_RUN_SA=738892841373-compute@developer.gserviceaccount.com
   gcloud secrets add-iam-policy-binding pdf2jpg-api-key \
     --member="serviceAccount:${CLOUD_RUN_SA}" \
     --role="roles/secretmanager.secretAccessor"
   ```
3. **デプロイ時にシークレットを参照**
   ```bash
   gcloud run deploy pdf2jpg-api \
     --set-secrets API_KEYS=pdf2jpg-api-key:latest
   ```
4. **GitHub Actions を利用する場合**
   - Workload Identity Federation を構成し、`CLOUD_RUN_API_SECRET_NAME` シークレットに `pdf2jpg-api-key` を設定します。
   - `deploy.yml` から `--set-secrets API_KEYS=${{ env.API_KEY_SECRET }}:latest` が呼び出されます。

## 3. 一時キーと Firestore セキュリティ

- Firestore には `apiKeys` コレクション内に一時キーを保存します。Document ID が API キーそのものになるため、クライアントやログに生のキーを出力しないでください。
- サービスアカウントには `roles/datastore.user` など最小限の Firestore 参照権限のみを付与します。
- Cloud Run 実行時は `ENABLE_FIRESTORE_KEYS=false` を利用することで、緊急時に動的キー検証を停止できます（静的キーのみ許可）。
- エミュレータ利用時も本番用プロジェクト ID や資格情報を混在させないよう `.env` を分けて管理してください。

## 4. ログ管理

- サーバーは標準出力へ JSON 形式の構造化ログを出力します。
- Cloud Run では Cloud Logging に自動連携され、`INFO` / `WARN` / `ERROR` レベルで出力。
- リクエストごとにステータスコード・処理時間・エラーメッセージを記録するため、監査用途にも利用できます。

### 推奨設定

- Cloud Logging でアラート（5xx 割合、平均レイテンシなど）を設定する。
- ログの保持ポリシーは GCP 側で設定し、不要な個人情報をログに含めないよう注意する。

## 5. データ削除ポリシー

- PDF ファイルは一時的に `/tmp` に保存し、変換後に必ず削除します（`internal/util/file_util.go`）。
- 変換済み JPEG はレスポンスとして返却し、サーバー内には保持しません。
- 永続ストレージを利用しないため、ファイルがサーバーに残ることはありません。

## 6. 権限の最小化

- Cloud Run デプロイ用サービスアカウントには以下のロールのみを付与してください。
  - `roles/run.admin`
  - `roles/iam.serviceAccountUser`
  - `roles/artifactregistry.writer`
  - `roles/secretmanager.secretAccessor`
- GitHub Actions の Workload Identity principal にも同等のロールを割り当て、不要な権限を付与しないよう管理することが推奨されます。
