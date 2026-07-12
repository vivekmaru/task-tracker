#!/usr/bin/env bash
set -euo pipefail

readonly SQLC_VERSION="v1.31.1"
readonly GOVULNCHECK_VERSION="v1.6.0"

unformatted="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
if [[ -n "${unformatted}" ]]; then
  printf 'Go files require formatting:\n%s\n' "${unformatted}" >&2
  exit 1
fi

go vet ./...
go mod tidy -diff

if ! git diff --quiet -- internal/db; then
  printf 'Generated database code has local changes; regenerate from a clean tree.\n' >&2
  exit 1
fi
go run "github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}" generate
git diff --exit-code -- internal/db

go test ./...
go test -race ./...
go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./...
go build ./cmd/forge
