import { chromium, type BrowserContext } from 'playwright';

// full chromium ("new headless") + a normal UA: the default headless shell
// gets 403s from bot detection (imdb, akamai)
// ponytail: hardcoded UA string, bump it if sites start rejecting it
const UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36';

let ctx: BrowserContext | undefined;

async function context(): Promise<BrowserContext> {
  if (!ctx) {
    ctx = await chromium.launchPersistentContext(process.env.BROWSER_DATA_DIR ?? './data', {
      headless: true,
      channel: 'chromium',
      userAgent: UA,
      args: ['--disable-blink-features=AutomationControlled'],
    });
    ctx.on('close', () => (ctx = undefined));
  }
  return ctx;
}

export async function render(url: string): Promise<string> {
  const page = await (await context()).newPage();
  try {
    const response = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    if (response && response.status() >= 400) {
      throw new Error(`${response.status()} ${response.statusText()}`);
    }
    await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => {});
    return await page.content();
  } finally {
    await page.close();
  }
}
