# pdf2jpg

## Testing

Use the provided `Makefile` to run tests. The targets automatically create local cache directories required for Go tooling in restricted environments such as WSL sandboxing.

```bash
make test   # run all unit and integration tests
make unit   # run internal package unit tests
make e2e    # run /convert endpoint tests
```
