import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Operator-workflow browser coverage for the proof-first path:
 * login -> workspaces -> scoped queue -> ticket detail -> attempt/artifact
 * proof -> search. The fixture is seeded by global-setup.ts. It is read
 * lazily (not via a static import) because Playwright evaluates test-file
 * imports before globalSetup writes .fixture.json.
 */

type Fixture = {
  workspaceId: string;
  projectId: string;
  ticketId: string;
  ticketTitle: string;
  attemptId: string;
  artifactId: string;
  searchToken: string;
  proofBody: string;
  adminToken: string;
};

function loadFixture(): Fixture {
  const raw = readFileSync(join(import.meta.dirname, '.fixture.json'), 'utf8');
  return JSON.parse(raw) as Fixture;
}

async function login(page: Page, adminToken: string, next?: string): Promise<void> {
  await page.goto(next ? `/login?next=${encodeURIComponent(next)}` : '/login');
  await page.getByLabel('Admin token').fill(adminToken);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

function noSeriousAxeViolations(results: { violations: Array<{ impact?: string | null }> }): void {
  expect(
    results.violations.filter((violation) => ['critical', 'serious'].includes(violation.impact ?? '')),
  ).toEqual([]);
}

test('login round-trip lands on workspaces and rejects a wrong token', async ({ page }) => {
  const fixture = loadFixture();

  // Wrong token stays on /login with the invalid-token message.
  await login(page, 'definitely-wrong-token');
  await expect(page.getByText('Invalid admin token.')).toBeVisible();
  await expect(page).toHaveURL(/\/login/);

  // Correct token lands on /workspaces with the fixture workspace visible.
  await login(page, fixture.adminToken);
  await expect(page).toHaveURL(/\/workspaces$/);
  await expect(page.locator(`a[href="/workspaces/${fixture.workspaceId}"]`)).toBeVisible();
});

test('queue lists the fixture ticket and opens its detail', async ({ page }) => {
  const fixture = loadFixture();
  await login(page, fixture.adminToken);

  await page.goto(`/tickets?workspace_id=${fixture.workspaceId}&project_id=${fixture.projectId}`);
  const ticketLink = page.locator(`a[href="/tickets/${fixture.ticketId}"]`);
  await expect(ticketLink).toBeVisible();
  await expect(ticketLink).toContainText(fixture.ticketTitle);

  await ticketLink.click();
  await expect(page).toHaveURL(new RegExp(`/tickets/${fixture.ticketId}$`));
  await expect(page.getByRole('heading', { name: fixture.ticketTitle })).toBeVisible();
  // Acceptance criteria and the attempt link are part of the proof-first detail.
  await expect(page.getByText('The fixture ticket can be claimed and completed')).toBeVisible();
  await expect(page.locator(`a[href="/attempts/${fixture.attemptId}"]`).first()).toBeVisible();
});

test('attempt and artifact detail expose the proof', async ({ page }) => {
  const fixture = loadFixture();
  await login(page, fixture.adminToken);

  await page.goto(`/attempts/${fixture.attemptId}`);
  await expect(page.getByRole('heading', { name: 'Attempt Detail' })).toBeVisible();
  const artifactLink = page.locator(`a[href="/artifacts/${fixture.artifactId}"]`).first();
  await expect(artifactLink).toBeVisible();

  await page.goto(`/artifacts/${fixture.artifactId}`);
  await expect(page.getByRole('heading', { name: 'Artifact Detail' })).toBeVisible();

  // The content route serves the raw proof body.
  const content = await page.request.get(`/artifacts/${fixture.artifactId}/content`);
  expect(content.ok()).toBeTruthy();
  expect(await content.text()).toContain(fixture.proofBody);
});

test('search finds the fixture ticket by its token', async ({ page }) => {
  const fixture = loadFixture();
  await login(page, fixture.adminToken);

  await page.goto(
    `/search?workspace_id=${fixture.workspaceId}&project_id=${fixture.projectId}&q=${fixture.searchToken}`,
  );
  await expect(page.locator(`a[href="/tickets/${fixture.ticketId}"]`)).toBeVisible();
});

test('ticket action forms lay out without buttons overflowing their form', async ({ page }) => {
  const fixture = loadFixture();
  await login(page, fixture.adminToken);

  for (const width of [1200, 900]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/tickets/${fixture.ticketId}`);

    const forms = page.locator('.ticket-actions form');
    const formCount = await forms.count();
    // A completed ticket exposes Reopen / Request review / Archive.
    expect(formCount).toBeGreaterThanOrEqual(2);

    for (let i = 0; i < formCount; i++) {
      const form = forms.nth(i);
      const button = form.getByRole('button');
      const formBox = await form.boundingBox();
      const buttonBox = await button.boundingBox();
      expect(formBox, `form ${i} has a box`).not.toBeNull();
      expect(buttonBox, `button ${i} has a box`).not.toBeNull();
      if (!formBox || !buttonBox) continue;
      // The button must stay within its own form's column (a few px tolerance)
      // rather than overflowing into a sibling form as it did at 89px columns.
      const tolerance = 2;
      expect(buttonBox.x).toBeGreaterThanOrEqual(formBox.x - tolerance);
      expect(buttonBox.x + buttonBox.width).toBeLessThanOrEqual(formBox.x + formBox.width + tolerance);
    }
  }

  // Full labels render for the completed-ticket actions.
  await expect(page.getByRole('button', { name: 'Reopen' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Request review' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Archive' })).toBeVisible();
});

test('core operator pages have no serious axe violations', async ({ page }) => {
  const fixture = loadFixture();
  await login(page, fixture.adminToken);

  await page.goto('/workspaces');
  noSeriousAxeViolations(await new AxeBuilder({ page }).analyze());

  await page.goto(`/tickets?workspace_id=${fixture.workspaceId}&project_id=${fixture.projectId}`);
  noSeriousAxeViolations(await new AxeBuilder({ page }).analyze());

  await page.goto(`/tickets/${fixture.ticketId}`);
  noSeriousAxeViolations(await new AxeBuilder({ page }).analyze());
});
