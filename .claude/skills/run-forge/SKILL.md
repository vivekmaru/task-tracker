---
name: run-forge
description: Use when you need to launch, seed, drive, or screenshot Forge locally — the server, web UI, CLI agent lifecycle, or Bubble Tea TUI — including verifying UI or handler changes against a live instance.
---

# Run Forge

Launch Forge against a disposable database and drive every surface. All commands verified from a cold start on 2026-07-18.

## Prerequisites

PostgreSQL running locally (`pg_isready`), Go 1.26+, `jq`, `tmux` (TUI only).

## Launch

Order matters: **the server pings Postgres at boot and exits if the database is missing or unmigrated** (`internal/runtime/runtime.go`). Work in a scratch dir, not the repo root.

```bash
DB=forge_run_$$; createdb "$DB"
export FORGE_DATABASE_URL="postgres://localhost:5432/${DB}?sslmode=disable"
go run ./cmd/forge migrate                       # from the repo root
go build -o /tmp/forge-bin ./cmd/forge
go run ./cmd/forge init --path /tmp/forge-run.json \
  --database-url "$FORGE_DATABASE_URL" \
  --admin-token "local-run-token-0123456789abcdef" \
  --artifact-root /tmp/forge-artifacts
FORGE_HTTP_ADDR=127.0.0.1:3057 /tmp/forge-bin server --config /tmp/forge-run.json &
echo $! > /tmp/forge-run.pid
sleep 1.5
curl -fsS http://127.0.0.1:3057/livez   # → ok
curl -fsS http://127.0.0.1:3057/readyz  # → ready
```

Use a non-default port (default 3017) to avoid colliding with other sessions; `FORGE_HTTP_ADDR` env overrides the config file.

## Seed a realistic fixture (CLI as agent)

Write flags literally — do NOT collect them in a shell variable (`C="--config ..."` breaks under zsh, which does not word-split unquoted variables: `flag provided but not defined: -config /tmp/forge-run.json`).

```bash
FB=/tmp/forge-bin
WS=$($FB workspaces create --config /tmp/forge-run.json --json --name "Run Probe" | jq -r .id)
PROJ=$($FB projects create --config /tmp/forge-run.json --json --workspace-id "$WS" --name "Main" | jq -r .id)
$FB create --config /tmp/forge-run.json --json --workspace-id "$WS" --project-id "$PROJ" \
  --title "Probe ticket" --type bug --description "d" \
  --acceptance "a" --verify "go test ./..." > /dev/null
CLAIM=$($FB codex claim --config /tmp/forge-run.json --workspace-id "$WS" --project-id "$PROJ" \
  --agent-id codex --capability codegen --lease 30m)
AT=$(echo "$CLAIM" | jq -r .attempt_id)   # full ticket detail: .context.ticket
$FB codex checkpoint --config /tmp/forge-run.json --attempt-id "$AT" \
  --summary "half way" --progress 50 --file README.md
printf 'proof\n' > /tmp/proof.txt
$FB codex complete --config /tmp/forge-run.json --attempt-id "$AT" --summary "done" \
  --proof /tmp/proof.txt --tokens-in 10 --tokens-out 5 --cost-usd 0.01 --duration 1m > /dev/null
```

## Drive the web UI

Open `http://127.0.0.1:3057/login`, submit the admin token from the init command; you land on `/workspaces`. Scoped queue: `/tickets?workspace_id=$WS&project_id=$PROJ`. Scripted access: header `X-Forge-Admin-Token: <token>` (works on human routes and `/api/v1`).

## Drive the TUI

Needs a TTY — use tmux:

```bash
tmux new-session -d -s forgetui -x 120 -y 36 \
  "/tmp/forge-bin tui --config /tmp/forge-run.json --workspace-id $WS --project-id $PROJ"
tmux send-keys -t forgetui j        # keys: j/k move, / filter, enter open, c copy, b back, q quit
tmux capture-pane -t forgetui -p
tmux kill-session -t forgetui
```

## Gotchas

| Symptom | Cause / fix |
|---|---|
| Server exits: `database ... does not exist` | createdb + `forge migrate` before boot |
| Attempt command ignores `--reason`/`--category` | Use flags-first with `--attempt-id X`, not positional-then-flags |
| `SQLSTATE 23514` on block/fail | `--category` must be one of: task_failed, blocked, needs_human, environment_failed, permission_required, dependency_missing, unclear_requirements |
| `jq: Cannot index array` on `list` | Output is wrapped: `.tickets[]` |
| GET `/` 404s (older builds) | Use `/login` directly |

## Cleanup

```bash
kill "$(cat /tmp/forge-run.pid)" 2>/dev/null; tmux kill-session -t forgetui 2>/dev/null
dropdb "$DB"; rm -f /tmp/forge-run.json /tmp/forge-run.pid /tmp/forge-bin /tmp/proof.txt
```

(`kill %1` does not work across separate agent shell invocations — each tool call is a fresh shell, so job control is empty; use the PID file.)
