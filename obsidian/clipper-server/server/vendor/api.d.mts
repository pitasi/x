// Hand-written types for the exports of vendor/api.mjs we actually use.
// Source: obsidian-clipper 1.7.1 src/api.ts

export interface DocumentParser {
	parseFromString(html: string, mimeType: string): any;
}

export interface Property {
	id?: string;
	name: string;
	value: string;
	type?: string;
}

export interface Template {
	id: string;
	name: string;
	behavior?: string;
	noteNameFormat?: string;
	path?: string;
	noteContentFormat?: string;
	properties?: Property[];
	triggers?: string[];
	vault?: string;
}

export interface ClipOptions {
	html: string;
	url: string;
	template: Template;
	documentParser: DocumentParser;
	propertyTypes?: Record<string, string>;
	parsedDocument?: any;
}

export interface ClipResult {
	noteName: string;
	frontmatter: string;
	content: string;
	fullContent: string;
	properties: Property[];
	variables: Record<string, string>;
}

export function matchTemplate(templates: Template[], url: string, schemaOrgData?: any): Template | undefined;
export function clip(options: ClipOptions): Promise<ClipResult>;
