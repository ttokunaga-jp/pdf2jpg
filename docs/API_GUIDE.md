# API Guide

## Overview

- **Base URL**: `https://{service-name}-{project-number}.{region}.run.app`
- **Authentication**: `X-API-Key` ヘッダ（必須）
- **Supported Content-Type**: `multipart/form-data`
- **最大ファイルサイズ**: 10MB

## Endpoint

### `POST /convert`

| 項目 | 内容 |
| --- | --- |
| Method | `POST` |
| URL | `{BASE_URL}/convert` |
| Header | `X-API-Key: {your_api_key}` |
| Content-Type | `multipart/form-data` |
| Form Field | `file` – 変換対象の PDF（必須） |

#### 正常系リクエスト例

```bash
curl -H "X-API-Key: ${API_KEY}" \
     -F "file=@sample.pdf" \
     https://pdf2jpg-api-738892841373.asia-northeast3.run.app/convert \
     -o result.jpg
```

#### 正常系レスポンス

- **Status**: `200 OK`
- **Headers**:
  ```
  Content-Type: image/jpeg
  Content-Disposition: inline; filename="sample.jpg"
  ```
- **Body**: JPEG バイナリ

## Error Responses

| シナリオ | Status | Content-Type | Body |
| --- | --- | --- | --- |
| 静的キー/一時キー不正 | 401 | `application/json` | `{"error":"unauthorized"}` |
| 一時キー期限切れ/失効 | 403 | `application/json` | `{"error":"key inactive"}` |
| 一時キー使用回数超過 | 429 | `application/json` | `{"error":"usage limit reached"}` |
| Firestore 障害 | 503 | `application/json` | `{"error":"service unavailable"}` (`Retry-After` ヘッダ付与) |
| `file` フィールド未指定 | 400 | `application/json` | `{"error":"file field is required"}` |
| PDF 以外の拡張子 | 400 | `application/json` | `{"error":"file must be a pdf"}` |
| 10MB 超過 | 413 | `application/json` | `{"error":"file too large"}` |
| PDF にページ無し | 400 | `application/json` | `{"error":"pdf has no pages"}` |
| 内部エラー | 500 | `application/json` | `{"error":"failed to convert pdf"}` |

#### エラー例：ファイル未指定

```bash
curl -H "X-API-Key: ${API_KEY}" \
     -F "file=" \
     https://.../convert

# Response
HTTP/1.1 400 Bad Request
Content-Type: application/json

{"error":"file field is required"}
```

## Status Codes

| Code | 意味 |
| --- | --- |
| 200 | 変換成功 |
| 400 | 不正リクエスト（ファイル未指定/形式不正/ページ無しなど） |
| 401 | 認証エラー（API キー不一致） |
| 413 | ファイルサイズ超過（>10MB） |
| 500 | 内部エラー（変換失敗など） |

## Admin API Overview

- すべての管理エンドポイントは `X-Admin-Key` ヘッダ（環境変数 `MASTER_API_KEYS`）で認証されます。
- 失敗時は `404 {"error":"not found"}` を返却し、キー名の推測を防ぎます。
- 副作用のある操作は Cloud Logging に `event=api_key_issue|api_key_revoke` として記録されます。

| Endpoint | 説明 |
| --- | --- |
| `POST /admin/api-keys` | 一時キー作成。`{"label":"trial","usageLimit":10,"ttlMinutes":60}` |
| `GET /admin/api-keys/{key}` | キーのメタデータと `status` (`active`/`expired`/`exhausted`/`revoked`) を返却。|
| `POST /admin/api-keys/{key}/revoke` | `remainingUsage=0` に設定し、即時失効。|
| `POST /admin/api-keys/cleanup` | (任意) 期限切れキーを最大 200 件削除。|

## Notes

- 変換対象は PDF の 1 ページ目のみです。
- 応答は JPEG バイナリのため、`curl` の `-o` などでファイル保存するか、HTTP クライアント側でバイナリ処理してください。
- リクエストごとに `/tmp` 配下の一時ファイルを作成・削除するため、ステートレスに動作します。
- 一時キーを利用する場合は、キー発行時に指定した使用回数・有効期限を超えると 429/403 を返却します。
