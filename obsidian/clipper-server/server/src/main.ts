import { createServer } from 'node:http';
import { clipHtml } from './clipper.js';
import { render } from './render.js';

const PORT = Number(process.env.PORT ?? 8090);
const TOKEN = process.env.CLIPPER_TOKEN;

// ponytail: single global queue, parallel tabs if it ever feels slow
let queue: Promise<unknown> = Promise.resolve();
function enqueue<T>(fn: () => Promise<T>): Promise<T> {
  const job = queue.then(fn);
  queue = job.then(
    () => {},
    () => {}
  );
  return job;
}

async function handleClip(url: string): Promise<{ status: number; body: unknown }> {
  let html: string;
  try {
    html = await render(url);
  } catch (e: any) {
    return { status: 502, body: { error: `page load failed: ${e?.message ?? e}` } };
  }
  try {
    return { status: 200, body: await clipHtml(url, html) };
  } catch (e: any) {
    return { status: 500, body: { error: `clip failed: ${e?.message ?? e}` } };
  }
}

createServer(async (req, res) => {
  const json = (status: number, body: unknown) => {
    res.writeHead(status, { 'content-type': 'application/json' });
    res.end(JSON.stringify(body));
  };

  if (req.method === 'GET' && req.url === '/healthz') {
    res.end('ok');
    return;
  }
  if (req.method !== 'POST' || req.url !== '/clip') return json(404, { error: 'not found' });
  if (TOKEN && req.headers.authorization !== `Bearer ${TOKEN}`) {
    return json(401, { error: 'unauthorized' });
  }

  let body = '';
  for await (const chunk of req) body += chunk;
  let url: unknown;
  try {
    url = JSON.parse(body).url;
  } catch {
    return json(400, { error: 'invalid json body' });
  }
  if (typeof url !== 'string' || !/^https?:\/\//.test(url)) {
    return json(400, { error: 'missing or invalid url' });
  }

  console.log(`clip ${url}`);
  const result = await enqueue(() => handleClip(url));
  json(result.status, result.body);
}).listen(PORT, () => console.log(`clipper-server listening on :${PORT}`));
