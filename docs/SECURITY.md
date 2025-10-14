# Security Guide

## 1. API キー認証

- 本サービスはリクエストヘッダ `X-API-Key` に設定したトークンで認証します。
- サーバー側では `API_KEYS` 環境変数（カンマ区切り）で有効なキーを定義します。
- ローカル開発時は `.env` などを利用し、公開リポジトリ内に平文で置かないよう注意してください。

### サンプル

```bash
export API_KEYS=59fd7eb3a9826c832643fcfb42f45cc4a8e677e6b4a35887f3b3a0ea9d59ab08
curl -H "X-API-Key: ${API_KEYS}" \
     -F "file=@sample.pdf" http://localhost:8080/convert
```

## 2. Secret Manager 利用手順

Cloud Run では Secret Manager に格納した API キーを環境変数へバインドします。

1. **シークレット登録**
   ```bash
   echo -n "59fd..." | gcloud secrets create pdf2jpg-api-key --data-file=- --replication-policy=automatic
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

## 3. ログ管理

- サーバーは標準出力へ JSON 形式の構造化ログを出力します。
- Cloud Run では Cloud Logging に自動連携され、`INFO` / `WARN` / `ERROR` レベルで出力。
- リクエストごとにステータスコード・処理時間・エラーメッセージを記録するため、監査用途にも利用できます。

### 推奨設定

- Cloud Logging でアラート（5xx 割合、平均レイテンシなど）を設定する。
- ログの保持ポリシーは GCP 側で設定し、不要な個人情報をログに含めないよう注意する。

## 4. データ削除ポリシー

- PDF ファイルは一時的に `/tmp` に保存し、変換後に必ず削除します（`internal/util/file_util.go`）。
- 変換済み JPEG はレスポンスとして返却し、サーバー内には保持しません。
- 永続ストレージを利用しないため、ファイルがサーバーに残ることはありません。

## 5. 権限の最小化

- Cloud Run デプロイ用サービスアカウントには以下のロールのみを付与してください。
  - `roles/run.admin`
  - `roles/iam.serviceAccountUser`
  - `roles/artifactregistry.writer`
  - `roles/secretmanager.secretAccessor`
- GitHub Actions の Workload Identity principal にも同等のロールを割り当て、不要な権限を付与しないよう管理することが推奨されます。
