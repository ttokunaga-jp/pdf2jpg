# pdf2jpg

## Testing

Use the provided `Makefile` to run tests. The targets automatically create local cache directories required for Go tooling in restricted environments such as WSL sandboxing.

```bash
make test   # run all unit and integration tests
make unit   # run internal package unit tests
make e2e    # run /convert endpoint tests
```

## GitHub Secrets required for deployment

| Secret | Purpose |
| --- | --- |
| `GCP_PROJECT` | Target Google Cloud project ID |
| `CLOUD_RUN_REGION` | Cloud Run region (e.g. `asia-northeast3`) |
| `CLOUD_RUN_SERVICE` | Cloud Run service name |
| `WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation provider resource name |
| `DEPLOYER_SERVICE_ACCOUNT` | Service account email used by the workflow |
| `ARTIFACT_REGISTRY_HOST` | Artifact Registry host (e.g. `asia-northeast3-docker.pkg.dev`) |
| `CLOUD_RUN_API_SECRET_NAME` | Secret Manager ID that stores the API key (e.g. `pdf2jpg-api-key`) |
