import { clipHtml } from '../src/clipper.js';

const url = 'https://www.imdb.com/title/tt1748122/';
const html = `<!doctype html><html><head><script type="application/ld+json">${JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'Movie',
  name: 'Moonrise Kingdom',
})}</script></head><body></body></html>`;
const result = await clipHtml(url, html);

if (!result.content.includes('imdbId: "tt1748122"')) {
  throw new Error(`IMDb ID was not extracted:\n${result.content}`);
}
console.log('PASS IMDb ID extraction');
