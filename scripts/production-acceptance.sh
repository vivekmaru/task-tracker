#!/usr/bin/env bash
set -euo pipefail

: "${FORGE_TEST_DATABASE_URL:?set a disposable forge_test PostgreSQL URL}"
: "${FORGE_RECOVERY_DATABASE_URL:?set a disposable forge_recovery PostgreSQL URL}"

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
started_epoch="$(date +%s)"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

./scripts/verify.sh
go build -trimpath -o "${workdir}/forge" ./cmd/forge
version_output="$("${workdir}/forge" version)"

FORGE_TEST_DATABASE_URL="${FORGE_TEST_DATABASE_URL}" go test -tags=integration ./internal/integration -run 'TestRESTResource|TestRESTExecution|TestMCPStdio|TestConcurrentClaimNext|TestHeartbeatExpiryRace|TestWebhookStaleClaim' -count=1
FORGE_RECOVERY_DATABASE_URL="${FORGE_RECOVERY_DATABASE_URL}" ./scripts/recovery-smoke.sh

if command -v bun >/dev/null 2>&1 && [[ -d ui-tests/node_modules ]]; then
  FORGE_DATABASE_URL="${FORGE_RECOVERY_DATABASE_URL}" FORGE_ADMIN_TOKEN="acceptance-token" FORGE_ARTIFACT_ROOT="${workdir}/artifacts" bun --cwd ui-tests run test
fi

finished_epoch="$(date +%s)"
elapsed=$((finished_epoch-started_epoch))
mkdir -p docs
cat > docs/production-acceptance-report.md <<REPORT
# Production acceptance pilot

- Started (UTC): ${started_at}
- Duration seconds: ${elapsed}
- Release binary: ${version_output}
- Quality gate: pass
- REST and MCP lifecycle: pass
- Claim race, lease fencing, terminal atomicity: pass
- Webhook ownership fencing: pass
- Recovery drill: pass
- Browser accessibility smoke: pass when Bun dependencies are installed

The pilot uses disposable databases and deliberately omits database URLs,
tokens, ticket bodies, artifact contents, and webhook secrets.
REPORT

if (( elapsed > 900 )); then
  echo "acceptance exceeded the 15-minute readiness budget (${elapsed}s)" >&2
  exit 1
fi
echo "production acceptance passed in ${elapsed}s"
