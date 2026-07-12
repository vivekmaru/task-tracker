# Plan 016: Refactor the web shell and preserve active scope

> **Executor instructions**: Preserve the Go + templ + htmx architecture and existing neutral, dense, calm visual language. Do not apply the generic hero/marketing style suggested by design databases; Forge is an operator console.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/web go.mod go.sum scripts README.md`

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: MED
- **Depends on**: Plan 001
- **Category**: ui, tech-debt
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.16`

## Why this matters

The web UI is generated from one 2,097-line Go file containing routes, view models, HTML strings, and minified CSS. Primary navigation also discards workspace/project scope, forcing users to re-enter raw UUIDs. Production polish will be unsafe until the shell has typed component and navigation boundaries.

## Current state

- `internal/web/handler.go:1616-1622` uses templ only as a wrapper around formatted HTML strings.
- `internal/web/handler.go:2095` embeds the entire design system as one minified string.
- Shell links at `handler.go:1618` omit scope and active-route state.
- Project links at `handler.go:1605` already establish the correct scoped ticket URL.
- Product intent in `docs/human-operations.md` is proof-first, keyboard-friendly, server-rendered, and anti-Jira.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Generate | pinned templ generation command | no stale generated files |
| Web tests | `rtk go test ./internal/web ./internal/api` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: typed page context, `.templ` shell/navigation/shared components, embedded CSS and pinned htmx assets, route-aware/scope-aware URLs, semantic HTML tests, generation commands.

**Out of scope**: new pages, broad visual redesign, dark mode, ticket-detail content, pagination, browser automation, React/Next, or changing service queries.

## Suggested executor toolkit

- Use `ui-ux-pro-max` for accessibility/navigation checks, but prioritize the documented Forge UX contract over generic style recommendations.
- Use templ documentation matching the pinned repository version.

## Git workflow

- Branch: `feat/production-016-web-shell`
- Commit: `Refactor scoped web shell`

## Steps

1. Define a typed page context containing title, active route, workspace ID, project ID, optional return URL, and Plan 007 CSRF data. Handlers must construct it explicitly.
2. Create `.templ` components for document shell, sidebar/navigation, page heading, buttons, form fields, status/empty/error panels, cards, and metadata lists. Move incrementally and preserve escaping.
3. Move CSS to an embedded readable asset using existing semantic tokens. Preserve current spacing/color direction; do not introduce external fonts or a new styling framework.
4. Vendor the pinned htmx asset into the binary and serve it with content type, immutable cache headers, and versioned URL. Full-page navigation/forms must still work without JavaScript.
5. Generate scope-preserving navigation centrally. Mark the active page and provide a workspace/project chooser path rather than raw UUID re-entry when scope is absent.
6. Replace raw-string tests with parsed landmark, navigation, form-label, escaping, and scoped-URL assertions while keeping focused output tests.

## Done criteria

- [ ] Shared shell and primitives live in `.templ` components.
- [ ] CSS and htmx are embedded local assets.
- [ ] Normal navigation preserves workspace/project scope.
- [ ] Missing scope has a discoverable chooser path.
- [ ] Generated files are reproducible and checked in CI.
- [ ] No React, external font, or runtime CDN dependency was added.

## STOP conditions

- Moving a component changes escaping of user-controlled content.
- Active scope cannot be represented without changing runtime/service request contracts.
- The templ version cannot generate deterministic output on the pinned Go toolchain.

## Maintenance notes

Plans 017 and 018 must build on these primitives rather than adding page-local HTML/CSS strings. Keep design tokens semantic and operator-console oriented.

