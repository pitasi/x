// Clip once and print the result. Usage:
//   npm run try -- <url>                     fetch the url
//   npm run try -- <url> --html <file>       use saved html instead of fetching
import { readFileSync } from 'node:fs';
import { clipHtml } from '../src/clipper.js';

const args = process.argv.slice(2);
const htmlIdx = args.indexOf('--html');
const htmlPath = htmlIdx >= 0 ? args[htmlIdx + 1] : undefined;
const url = args.find((a) => !a.startsWith('--') && a !== htmlPath);
if (!url) {
  console.error('usage: try.ts <url> [--html file]');
  process.exit(1);
}

const html = htmlPath ? readFileSync(htmlPath, 'utf8') : await (await fetch(url)).text();
const res = await clipHtml(url, html);
console.error(JSON.stringify({ template: res.template, noteName: res.noteName, path: res.path }, null, 2));
process.stdout.write(res.content + '\n');
