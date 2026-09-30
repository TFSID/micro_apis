# Project guidance

- This project is a Go HTTP API built with Huma v2 and Chi, deployed to AWS Lambda using the provided.al2023 custom runtime and API Gateway HTTP API payload v2.
- Keep API operation definitions in `internal/api`; expose the same `http.Handler` locally and through the Lambda adapter.
- Run `go test ./...` before submitting changes. Use `go run ./cmd/api` for local development.
- Lambda deployment artifacts must be built for Linux ARM64 as the executable `bootstrap` at the project root.
