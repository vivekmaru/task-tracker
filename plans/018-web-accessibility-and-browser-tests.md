# Plan 018: Add web accessibility and browser acceptance tests

> **Executor instructions**: Treat accessibility as functional correctness. Keep browser tooling test-only and use Bun rather than Node/npm for JavaScript test execution.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/web ui-tests .github scripts go.mod bun.lock package.json README.md`

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: Plans 016 and 017
- **Category**: ui, tests, accessibility
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.18`

## Why this matters

Current tests can pass while scope navigation breaks, errors are not announced, controls are too small, or mobile layouts wrap badly. The production UI needs machine-checkable keyboard, responsive, form-feedback, and accessibility acceptance.

## Current state

- The shell has no skip link or route-aware `aria-current`.
- Current controls are roughly 33px tall rather than the 44px touch baseline.
- Six mobile nav links are placed in a five-column grid.
- Error sections are inconsistently associated with fields or live regions.
- Existing tests mostly assert HTML substrings; there is no real-browser suite.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Semantic Go tests | `rtk go test ./internal/web` | pass |
| Browser tests | `rtk bun test` or pinned Bun Playwright command in `ui-tests/` | all acceptance tests pass |
| Accessibility | pinned `@axe-core/playwright` assertions | zero critical/serious violations on covered pages |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 including browser job in CI |

## Scope

**In scope**: skip link, landmarks, active navigation, heading/form/error semantics, focus behavior, touch/mobile sizing, reduced motion, responsive overflow, pinned Bun + Playwright + axe tests, CI browser job.

**Out of scope**: visual rebrand, dark mode requirement, broad screenshot golden tests, browser support beyond the documented matrix, or runtime Node/Bun dependencies.

## Suggested executor toolkit

- Use `ui-ux-pro-max` accessibility, interaction, responsive, forms, and navigation checklists.
- Use the in-app browser during implementation for manual 375px and keyboard verification, but CI tests remain the acceptance authority.

## Git workflow

- Branch: `feat/production-018-web-accessibility`
- Commit: `Add web accessibility acceptance tests`

## Steps

1. Add a visible-on-focus skip link to `main`, route-aware `aria-current`, logical heading order, explicit labels/descriptions, and landmarks.
2. Make validation and operation errors field-associated where possible, announce summaries with appropriate live-region semantics, and move focus to the first invalid field after response.
3. Ensure interactive targets are at least 44px, mobile inputs use at least 16px text, IDs/JSON wrap safely, and mobile nav uses a deliberate six-item wrap/menu rather than five forced columns.
4. Respect `prefers-reduced-motion`; retain clear focus, hover, pressed, disabled, success, warning, and destructive states without color-only meaning.
5. Add pinned test-only Bun/Playwright/axe dependencies in `ui-tests/`. Start an isolated Forge server/database fixture and cover login, workspace/project selection, scoped navigation, ticket proof flow, destructive confirmation, error focus, keyboard-only operation, and 375px overflow.
6. Capture screenshots/traces only on failure. Avoid full-page golden images that fail on irrelevant font rendering.

## Done criteria

- [ ] Keyboard users can reach and operate every covered action.
- [ ] No critical/serious axe violations on login, queue, ticket, attempt, artifact, proposed, and activity pages.
- [ ] 375px and 200% zoom have no unintended horizontal page scroll.
- [ ] Touch targets and focus states meet the documented baseline.
- [ ] Browser tooling is test-only and pinned.

## STOP conditions

- Browser tests require a runtime JavaScript dependency.
- Fixing semantics changes domain behavior or removes progressive enhancement.
- An axe suppression is proposed without a documented false-positive justification.

## Maintenance notes

Every new human route must add semantic and browser coverage. Keep the browser suite narrow around critical journeys and debug artifacts rich on failure.

