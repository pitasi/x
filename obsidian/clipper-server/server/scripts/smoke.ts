// Smoke test: boots the real server, clips a live Wikipedia page, asserts.
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const PORT = 8199;
// own profile dir: chromium locks it, and a dev server may hold ./data
const dataDir = mkdtempSync(join(tmpdir(), 'clipper-smoke-'));
// detached group so kill(-pid) reaps the server and its chromium too
const child = spawn('node_modules/.bin/tsx', ['src/main.ts'], {
  env: { ...process.env, PORT: String(PORT), BROWSER_DATA_DIR: dataDir },
  stdio: 'inherit',
  detached: true,
});
const killServer = () => {
  try {
    process.kill(-child.pid!, 'SIGKILL');
  } catch {}
};

const deadline = Date.now() + 30_000;
for (;;) {
  try {
    if ((await fetch(`http://localhost:${PORT}/healthz`)).ok) break;
  } catch {}
  if (Date.now() > deadline) {
    console.error('FAIL server did not start within 30s');
    killServer();
    process.exit(1);
  }
  await new Promise((r) => setTimeout(r, 300));
}

function assert(cond: boolean, msg: string): asserts cond {
  if (!cond) throw new Error(msg);
}

try {
  const res = await fetch(`http://localhost:${PORT}/clip`, {
    method: 'POST',
    body: JSON.stringify({ url: 'https://en.wikipedia.org/wiki/Obsidian' }),
  });
  const body: any = await res.json();
  assert(res.status === 200, `status ${res.status}: ${JSON.stringify(body)}`);
  assert(body.template === 'Wikipedia', `unexpected template: ${body.template}`);
  assert(typeof body.content === 'string' && body.content.startsWith('---'), 'content missing frontmatter');
  assert(typeof body.noteName === 'string' && body.noteName.length > 0, 'empty noteName');
  console.log('PASS', {
    template: body.template,
    noteName: body.noteName,
    path: body.path,
    contentLength: body.content.length,
  });
} catch (e: any) {
  console.error('FAIL', e?.message ?? e);
  process.exitCode = 1;
} finally {
  killServer();
  try {
    rmSync(dataDir, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
  } catch {} // chromium may still be tearing down; the OS reaps tmp anyway
}
process.exit(process.exitCode ?? 0);
