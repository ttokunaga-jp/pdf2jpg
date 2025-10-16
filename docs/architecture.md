# PDF to JPEG Conversion API – Architecture Overview

## 1. Directory Structure
```
pdf2jpg/
├── cmd/
│   └── main.go
├── internal/
│   ├── auth/
│   │   ├── api_key.go
│   │   ├── auth_middleware.go
│   │   ├── admin_middleware.go
│   │   ├── firestore_repository.go
│   │   └── key_service.go
│   ├── handler/
│   │   ├── convert_handler.go
│   │   └── admin_keys_handler.go
│   ├── service/
│   │   └── pdf_service.go
│   └── util/
│       └── file_util.go
├── test/
│   └── e2e_test.go
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## 2. Module Responsibilities
- **cmd/main.go**: Cloud Run entry point. Loads `.env`, initialises Firestore client, wires authentication middleware, admin handlers, health checks, and graceful shutdown.
- **internal/handler**: Owns `POST /convert` と管理用 `/admin/api-keys` 系エンドポイント。入力バリデーション、レスポンス整形、HTTP エラーハンドリングを担う。
- **internal/service**: Wraps go-fitz to convert the first page of PDFs to JPEG, manages `/tmp` files, enforces JPEG quality (85), and maps conversion errors to service-level errors.
- **internal/auth**: Provides authentication middlewares, temporary key lifecycle管理 (`KeyService`)、Firestore リポジトリ実装、管理者レートリミット、負荷軽減のためのキャッシュとメトリクス収集を実装。
- **internal/util**: Utility helpers (currently file handling) をまとめ、他層から共有利用。
- **test**: Contains end-to-end tests for the conversion flow, covering static API keys and temporary keys with usage limits.

### Dependency Graph
```
cmd/main.go
 ├─ internal/handler
 │    ├─ internal/auth
 │    └─ internal/service
 └─ internal/util
internal/auth
 ├─ Firestore (cloud.google.com/go/firestore)
 └─ go.opentelemetry.io/otel (trace spans)
```

## 3. OpenAPI Specification
```yaml
openapi: 3.0.3
info:
  title: PDF to JPEG Conversion API
  version: 1.0.0
  description: Converts the first page of an uploaded PDF to JPEG.
servers:
  - url: https://{region}-run.googleapis.com
    description: Cloud Run service endpoint
    variables:
      region:
        default: us-central1
paths:
  /convert:
    post:
      summary: Convert first page of PDF to JPEG
      operationId: convertPdf
      security:
        - ApiKeyAuth: []
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required:
                - file
              properties:
                file:
                  type: string
                  format: binary
                  description: PDF file (first page will be converted)
            encoding:
              file:
                contentType: application/pdf
                headers:
                  Content-Length:
                    schema:
                      type: integer
                      maximum: 10485760
      responses:
        "200":
          description: JPEG image for the first page
          content:
            image/jpeg:
              schema:
                type: string
                format: binary
        "400":
          description: Invalid request (missing file, wrong format)
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "401":
          description: Invalid or missing API key
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "413":
          description: Uploaded file exceeds size limit
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "500":
          description: Internal conversion error
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
  schemas:
    ErrorResponse:
      type: object
      required:
        - error
      properties:
        error:
          type: string
          example: invalid file format
```

## 4. Dockerfile Dependency List
- Base image: `golang:1.25-bullseye` for the builder stage and `debian:bullseye-slim` for the runtime stage.
- apt packages: `build-essential`, `pkg-config`, `clang`, `mupdf`, `mupdf-tools`, `libmupdf-dev`, `libjpeg62-turbo-dev`, `libopenjp2-7`, `libfreetype6-dev`, `libjbig2dec0-dev`, `libharfbuzz-dev`, `ca-certificates`.
- Go modules: run `go mod download` to cache module dependencies, notably `github.com/gen2brain/go-fitz`.
- Runtime shared libraries: `libmupdf`, `libjpeg62-turbo`, `libopenjp2-7`, `libfreetype6`, `libjbig2dec0`, `libharfbuzz`, `libstdc++`, and `libgcc-s`.
- Cloud Run defaults: respect the `PORT` environment variable, run as a non-root user, and ensure `/tmp` remains writable.

## 5. Design Considerations
- Stateless design: each request stores the PDF in `/tmp`, performs conversion, and deletes artifacts to stay compatible with auto-scaling.
- Security: enforce `X-API-Key`, manage keys via environment variables, never log keys, and rely on Cloud Run for TLS termination.
- Input validation: enforce the 10 MB limit via `http.MaxBytesReader`, check MIME type and extension, and return HTTP 400 for malformed uploads.
- Error handling: map service errors to handler-level responses, separate user-facing messages from internal logs, and log with structured severity.
- Performance: single-request conversions with configurable JPEG quality and resolution for future reuse or tuning.
- Observability: attach request IDs, emit Cloud Logging compatible JSON logs, and include HTTP request metadata for monitoring.
- Testing: cover end-to-end scenarios (`test/e2e_convert_test.go`) for authentication, size limits, and invalid files, and create unit tests around the service layer with fixture PDFs.
