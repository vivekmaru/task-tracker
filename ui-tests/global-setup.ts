import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Seeds a deterministic operator-workflow fixture through the Forge CLI so the
 * browser specs have a workspace/project/ticket/attempt/artifact to exercise.
 * The CLI reads the same FORGE_DATABASE_URL the web server pings, so no HTTP is
 * involved here — we only populate the database and persist the created IDs.
 */

const REPO_ROOT = join(import.meta.dirname, '..');

// A distinctive word so the search spec can match the fixture ticket exactly.
const SEARCH_TOKEN = `browserflow${Date.now()}`;

function forge(args: string[]): string {
  return execFileSync('go', ['run', './cmd/forge', ...args], {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    env: process.env,
  });
}

function forgeJSON(args: string[]): Record<string, unknown> {
  const out = forge(args).trim();
  // The CLI may emit log lines before the JSON payload; take the last line.
  const jsonLine = out.split('\n').filter((line) => line.trim().startsWith('{')).pop();
  if (!jsonLine) {
    throw new Error(`no JSON in output of forge ${args.join(' ')}:\n${out}`);
  }
  return JSON.parse(jsonLine) as Record<string, unknown>;
}

export default function globalSetup(): void {
  const workspace = forgeJSON(['workspaces', 'create', '--json', '--name', 'Browser Workspace']);
  const workspaceId = String(workspace.id);

  const project = forgeJSON([
    'projects', 'create', '--json',
    '--workspace-id', workspaceId,
    '--name', 'Browser Project',
  ]);
  const projectId = String(project.id);

  const ticketTitle = `Browserflow ${SEARCH_TOKEN} ticket`;
  const ticket = forgeJSON([
    'create', '--json',
    '--workspace-id', workspaceId,
    '--project-id', projectId,
    '--title', ticketTitle,
    '--type', 'bug',
    '--description', 'Browser fixture ticket for the operator workflow specs',
    '--acceptance', 'The fixture ticket can be claimed and completed',
    '--verify', 'go test ./...',
  ]);
  const ticketId = String(ticket.id);

  const claim = forgeJSON([
    'codex', 'claim', '--json',
    '--workspace-id', workspaceId,
    '--project-id', projectId,
    '--agent-id', 'codex',
    '--capability', 'codegen',
    '--lease', '30m',
  ]);
  const attemptId = String(claim.attempt_id);

  forge([
    'codex', 'checkpoint', attemptId,
    '--summary', 'Fixture attempt reached checkpoint',
    '--progress', '50',
    '--file', 'README.md',
    '--command', 'go test ./...',
  ]);

  const proofDir = mkdtempSync(join(tmpdir(), 'forge-browser-proof-'));
  const proofPath = join(proofDir, 'proof.txt');
  const proofBody = 'browser proof ok';
  writeFileSync(proofPath, `${proofBody}\n`);

  const complete = forgeJSON([
    'codex', 'complete', attemptId,
    '--summary', 'Fixture attempt completed',
    '--proof', proofPath,
    '--tokens-in', '12',
    '--tokens-out', '7',
    '--cost-usd', '0.001',
    '--duration', '2s',
  ]);
  const artifacts = complete.artifacts as Array<Record<string, unknown>> | undefined;
  const artifactId = artifacts && artifacts[0] ? String(artifacts[0].id) : '';

  const fixture = {
    workspaceId,
    projectId,
    ticketId,
    ticketTitle,
    attemptId,
    artifactId,
    searchToken: SEARCH_TOKEN,
    proofBody,
    adminToken: process.env.FORGE_ADMIN_TOKEN ?? 'browser-test-token',
  };

  writeFileSync(join(import.meta.dirname, '.fixture.json'), JSON.stringify(fixture, null, 2));
}
