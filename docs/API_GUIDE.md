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
| API キー不正 | 401 | `application/json` | `{"error":"unauthorized"}` |
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

## Notes

- 変換対象は PDF の 1 ページ目のみです。
- 応答は JPEG バイナリのため、`curl` の `-o` などでファイル保存するか、HTTP クライアント側でバイナリ処理してください。
- リクエストごとに `/tmp` 配下の一時ファイルを作成・削除するため、ステートレスに動作します。
