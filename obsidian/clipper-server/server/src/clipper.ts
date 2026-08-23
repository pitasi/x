import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { parseHTML, Node as LinkedomNode } from 'linkedom';
import type { Template, DocumentParser } from '../vendor/api.mjs';

// turndown (bundled inside the vendored api) reads window.DOMParser at module
// init, so the polyfill must run before the dynamic imports below.
const LinkedomParser = function (this: unknown) {} as unknown as { new (): DocumentParser };
// browser DOMParser always synthesizes html/body and puts fragment content in
// body; linkedom doesn't, which breaks filters (remove_html) and turndown
LinkedomParser.prototype.parseFromString = (html: string) => {
  if (!/<(html|body)[\s>]/i.test(html)) {
    return parseHTML(`<!DOCTYPE html><html><head></head><body>${html}</body></html>`).document;
  }
  return parseHTML(html).document;
};
const g = globalThis as any;
if (typeof g.window === 'undefined') g.window = g;
g.DOMParser = LinkedomParser;
g.window.DOMParser = LinkedomParser;
class XMLSerializerShim {
  serializeToString(node: any): string {
    return node?.outerHTML ?? String(node);
  }
}
g.XMLSerializer ??= XMLSerializerShim;
g.window.XMLSerializer ??= XMLSerializerShim;
g.Node ??= LinkedomNode;
g.window.Node ??= LinkedomNode;
if (typeof g.document === 'undefined') {
  g.document = parseHTML('<!DOCTYPE html><html><head></head><body></body></html>').document;
}

const api = await import('../vendor/api.mjs');
const Defuddle = (await import('defuddle')).default as unknown as new (
  doc: unknown,
  options?: { url?: string }
) => { parse(): { schemaOrgData?: unknown } };

const documentParser: DocumentParser = new LinkedomParser();

const settingsPath =
  process.env.CLIPPER_SETTINGS ??
  fileURLToPath(new URL('../../../obsidian-web-clipper-settings.json', import.meta.url));
const settings = JSON.parse(readFileSync(settingsPath, 'utf8'));
export const templates: Template[] = (settings.template_list as string[])
  .map((id) => settings['template_' + id])
  .filter(Boolean);
// Match the browser clipper, which unescapes stored property values before compiling them.
for (const template of templates) {
  for (const property of template.properties ?? []) {
    property.value = property.value.replace(/\\"/g, '"').replace(/\\n/g, '\n');
  }
}
const propertyTypes: Record<string, string> = Object.fromEntries(
  (settings.property_types ?? []).map((p: { name: string; type: string }) => [p.name, p.type])
);

export interface ClipResponse {
  template: string;
  noteName: string;
  path: string;
  content: string;
}

export async function clipHtml(url: string, html: string): Promise<ClipResponse> {
  let parsedDocument: any;
  let template = api.matchTemplate(templates, url);
  if (!template) {
    parsedDocument = documentParser.parseFromString(html, 'text/html');
    const defuddleResult = new Defuddle(parsedDocument, { url }).parse();
    template = api.matchTemplate(templates, url, defuddleResult.schemaOrgData) ?? templates[0];
  }
  const result = await api.clip({ html, url, template, documentParser, propertyTypes, parsedDocument });
  return {
    template: template.name,
    noteName: result.noteName,
    path: template.path ?? '',
    content: result.fullContent,
  };
}
