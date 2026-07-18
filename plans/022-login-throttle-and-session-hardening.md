# Plan 022: Throttle human login failures and document session revocation

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 9f8d948..HEAD -- internal/web/handler.go internal/web/handler_test.go README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: security
- **Beads**: agent-task-tracker-sij
- **Planned at**: commit `9f8d948`, 2026-07-18

## Why this matters

The human web login compares the admin token in constant time, but accepts unlimited attempts: an attacker who can reach `/login` can submit guesses as fast as the server responds. The 32-byte generated token makes online guessing impractical, but operator-chosen tokens may be weaker, and a failure throttle is cheap defense-in-depth for an internet-reachable deployment. Additionally, "logout" only clears the cookie client-side: session values are stateless HMACs keyed by the admin token, so a stolen cookie stays valid until its expiry, and the only real revocation is rotating the admin token — which is nowhere documented. This plan adds a deterministic failure throttle and documents the revocation contract. It deliberately does NOT introduce server-side session state (v0.1 is trusted, single-tenant, self-hosted — recorded in `plans/README.md` "Decisions intentionally preserved").

## Current state

- `internal/web/handler.go:197-227` — `handleLogin` POST branch:
  ```go
  if !constantTimeTokenEqual(r.FormValue("admin_token"), h.auth.AdminToken) {
      renderComponent(r.Context(), w, http.StatusUnauthorized, loginPage(next, "Invalid admin token."))
      return
  }
  ```
  No counter, no delay, no lockout.
- `internal/web/handler.go:311-333` — `sessionValue` builds `expiresUnix.hex(HMAC-SHA256(admin_token, "forge-human-session-v1|expiresUnix"))`; `validSessionValue` re-derives and compares constant-time. Stateless: nothing to revoke server-side.
- `internal/web/handler.go` `AuthOptions` (around line 75-110) already has injectable `Now func() time.Time` (used via `a.now()`) — reuse this for deterministic throttle tests; do not add a second clock.
- `internal/web/handler.go:172-184` — `handleLogout` clears the cookie only.
- Conventions: handlers are methods on `Handler`; tests live in `internal/web/handler_test.go` using `net/http/httptest` (match its table/subtest style). No third-party middleware — keep the throttle dependency-free.
- `README.md:79` documents `forge init` token generation; there is no revocation/rotation documentation.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Unit tests | `go test ./internal/web` | all pass |
| Full suite | `go test ./...` | all pass |
| Race | `go test -race ./internal/web` | all pass (throttle is concurrent state) |
| Vet | `go vet ./...` | exit 0 |

## Scope

**In scope** (the only files you should modify):
- `internal/web/handler.go`
- `internal/web/handler_test.go`
- `README.md` (session revocation paragraph only)

**Out of scope** (do NOT touch):
- `internal/api/` — the `/api/v1` bearer boundary is separate and rate limiting there is deliberately deferred (trusted single tenant).
- Session format/`sessionValue` — do not add server-side session storage or change the cookie contract; browser tests and docs depend on it.
- `docs/` files other than README.

## Git workflow

- Branch: `advisor/022-login-throttle`
- Commit style: short imperative sentence (match `git log --oneline`).
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add a global failure throttle to `handleLogin`

Add a small mutex-guarded fixed-window counter on the `Handler`'s auth state (e.g. a `loginThrottle` struct with `windowStart time.Time`, `failures int`): allow at most 10 failed POST attempts per 1-minute window process-wide; while over the limit, respond `429 Too Many Requests` with the login page message "Too many failed login attempts. Try again shortly." Successful logins are never throttled unless the window is already exhausted. Key globally, not per-IP: the server commonly sits behind a reverse proxy and trusting `X-Forwarded-For` unauthenticated would be spoofable; a global limit is safe for a single-operator product. Use `h.auth.now()` for time so tests can inject the clock. Ensure the throttle check happens BEFORE token comparison only for over-limit rejection; count only failed comparisons.

**Verify**: `go test ./internal/web` → existing tests still pass.

### Step 2: Test the throttle

In `internal/web/handler_test.go`, following the existing httptest patterns, add subtests: (a) 10 failures then an 11th attempt → 429 even with the correct token; (b) window expiry via injected `Now` restores access; (c) successful login with correct token under the limit is unaffected; (d) concurrent failed attempts do not race (`go test -race`).

**Verify**: `go test -race ./internal/web` → all pass including 4+ new subtests.

### Step 3: Document session revocation

In `README.md`, after the paragraph about `forge init` token generation (around line 79), add 2-4 sentences: sessions are stateless HMACs derived from `admin_token`; logout clears the browser cookie but does not invalidate outstanding cookies; to revoke all sessions (e.g. a leaked cookie or shared machine), rotate `admin_token` in the config file or `FORGE_ADMIN_TOKEN` and restart `forge server`; all API callers must switch to the new token at the same time.

**Verify**: `grep -n "rotate" README.md` → shows the new paragraph.

## Test plan

New tests in `internal/web/handler_test.go` as listed in Step 2, modeled on the existing login tests in that file (find them with `grep -n "handleLogin\|/login" internal/web/handler_test.go`). Full gate: `go test ./...` and `go vet ./...` clean.

## Done criteria

- [ ] `go test -race ./...` exits 0; new throttle subtests exist and pass
- [ ] 11th failed login in one window returns 429 (asserted by test)
- [ ] `README.md` documents token rotation as the session revocation path
- [ ] No files outside the in-scope list modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- `handleLogin` at `internal/web/handler.go:197` no longer matches the excerpt (drift).
- The fix appears to require touching `internal/api/` or the session format.
- Existing handler tests fail for reasons unrelated to the throttle after two fix attempts.

## Maintenance notes

- If Forge later becomes multi-operator or adds real users, replace the global window with per-identity throttling and server-side sessions — this plan explicitly does not build that.
- Reviewer should scrutinize: the throttle must not introduce a timing side channel that distinguishes "wrong token" from "throttled" before the limit is reached, and must not lock out the operator permanently (window must expire).
