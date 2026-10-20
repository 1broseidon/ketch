// UI-only projections. Never mutate tool content/details: the model keeps the
// complete CLI response. Text (rather than Markdown) keeps remote content inert.
import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";

export type Surface = "search" | "code" | "docs" | "scrape" | "crawl" | "tag";
type RecordValue = Record<string, unknown>;
const object = (v: unknown): RecordValue => v !== null && typeof v === "object" && !Array.isArray(v) ? v as RecordValue : {};
const array = (v: unknown): unknown[] => Array.isArray(v) ? v : [];
const scalar = (v: unknown): string => typeof v === "string" || typeof v === "number" ? String(v) : "";

/** Strip ANSI/OSC/DCS and C0/C1 controls, plus invisible bidi overrides. */
export function safeText(value: unknown): string {
	return scalar(value)
		.replace(/(?:\x1b\]|\x9d)[\s\S]*?(?:\x07|\x1b\\|\x9c|$)/g, "")
		.replace(/\x1b[P^_][\s\S]*?(?:\x1b\\|$)/g, "")
		.replace(/(?:\x1b\[|\x9b)[0-?]*[ -/]*[@-~]/g, "")
		.replace(/\x1b[ -/]*[@-~]/g, "")
		.replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f\u202a-\u202e\u2066-\u2069]/g, "");
}
export function preview(value: unknown, limit = 180): string {
	const text = safeText(value).replace(/\s+/g, " ").trim();
	return text.length > limit ? text.slice(0, limit - 1) + "…" : text;
}
function domain(value: unknown): string {
	try { return new URL(scalar(value)).hostname; } catch { return ""; }
}
function first(...values: unknown[]): string {
	return values.map(scalar).find(Boolean) ?? "";
}

export function callSummary(surface: Surface, args: unknown): string {
	const p = object(args);
	const target = surface === "tag" ? [p.operation, p.tag].map(scalar).filter(Boolean).join(" ")
		: first(p.query, p.url, array(p.urls).length ? `${array(p.urls).length} URLs` : "");
	const options = ["backend", "repo", "lang", "library", "limit", "depth", "max_pages", "max_chars", "tag"]
		.filter(key => p[key] !== undefined && !(surface === "tag" && key === "tag"))
		.map(key => `${key}=${preview(p[key], 50)}`);
	for (const key of ["multi", "random"]) if (Array.isArray(p[key])) options.push(`${key}=${array(p[key]).map(scalar).join(",")}`);
	for (const key of ["scrape", "resolve", "raw", "sitemap"]) if (p[key] === true) options.push(key);
	return preview([target, options.join(" · ")].filter(Boolean).join("  ·  "), 260);
}

interface DisplayResult { content?: unknown; details?: unknown; isError?: boolean }
function contentText(result: DisplayResult): string {
	return array(result.content).map(object).filter(c => c.type === "text").map(c => scalar(c.text)).join("\n");
}
function dataOf(result: DisplayResult): unknown {
	const d = object(result.details);
	if (d.result !== undefined) return d.result;
	if (result.details !== undefined && !("result" in d) && !("stderr" in d)) return result.details;
	try { return JSON.parse(contentText(result)); } catch { return undefined; }
}
function rowsOf(data: unknown): unknown[] | undefined {
	if (Array.isArray(data)) return data;
	const d = object(data);
	for (const key of ["results", "pages", "tags", "libraries", "items"]) if (Array.isArray(d[key])) return d[key] as unknown[];
	if (["url", "title", "snippet", "markdown", "raw_html", "id"].some(k => k in d)) return [d];
	return undefined;
}
function rowLines(value: unknown, index: number, surface: Surface): string[] {
	const r = object(value);
	if (!Object.keys(r).length) return [`${index + 1}. ${preview(value) || "Unknown entry"}`];
	if (surface === "tag" && "name" in r) return [`${preview(r.name, 80)} — ${scalar(r.entries) || "0"} entries · ${scalar(r.cached) || "0"} cached${r.cache_status === "unavailable" ? " (cache unavailable)" : ""}`];
	const title = first(r.title, r.name, r.repo && `${scalar(r.repo)}/${scalar(r.path)}${r.line ? `:${scalar(r.line)}` : ""}`, r.id, r.library, r.url, "Untitled");
	const host = domain(r.url);
	const lines = [`${index + 1}. ${preview(title, 110)}${host && title !== r.url ? ` · ${preview(host, 65)}` : ""}`];
	if (surface === "docs" && (r.id || r.library)) lines.push(`   ${preview(first(r.id, r.library), 140)}`);
	if (r.url && (surface === "scrape" || surface === "crawl" || surface === "tag")) lines.push(`   ${preview(r.url, 160)}`);
	const snippet = first(r.error, r.description, r.snippet, r.markdown, r.body, r.content, r.raw_html)
		.replace(/^[\s\S]*?hide this help[\s\S]*?(?:\bAyu\b|\bCoal\b)\s*[*·-]*\s*/i, "");
	if (snippet) lines.push(`   ${r.error ? "Error: " : ""}${preview(snippet, 150)}`);
	return lines;
}

/** Compact summary is bounded independently of the size of the model response. */
export function resultSummary(surface: Surface, result: DisplayResult, isError = false): string[] {
	if (isError || result.isError) return ["Failed", preview(contentText(result), 360) || "No error message returned"];
	const data = dataOf(result);
	const d = object(data);
	const rows = rowsOf(data);
	const lines: string[] = [];
	if (surface === "tag" && Array.isArray(d.tagged)) {
		lines.push(`Tagged ${d.tagged.length} URLs · ${preview(d.tag, 80)}`);
		if (array(d.not_cached).length) lines.push(`${array(d.not_cached).length} not cached (bookmarks saved)`);
	} else if (surface === "tag" && typeof d.removed === "number") {
		lines.push(`Removed ${d.removed} entries · ${preview(d.tag, 80)}`);
		if (array(d.missing).length) lines.push(`${array(d.missing).length} URLs not found`);
	} else if (rows) {
		const noun = surface === "scrape" || surface === "crawl" ? "pages" : surface === "tag" && Array.isArray(d.tags) ? "tags" : "results";
		const label = rows.length === 1 ? noun.replace(/s$/, "") : noun;
		lines.push(`${rows.length} ${label}${typeof d.entries === "number" ? ` of ${d.entries} total` : ""}${d.tag ? ` · ${preview(d.tag, 60)}` : ""}`);
		for (const [i, row] of rows.slice(0, 4).entries()) lines.push(...rowLines(row, i, surface));
		if (rows.length > 4) lines.push(`   +${rows.length - 4} more`);
		const failed = rows.filter(r => Boolean(object(r).error)).length;
		if (failed) lines.push(`${failed} page errors`);
	} else {
		// Unknown structured shapes must not turn into a default JSON/context dump.
		lines.push(data === undefined ? "Response received (unstructured)" : "Response received");
	}
	if (d.stopped) lines.push(`Stopped: ${d.stopped === "max_pages" ? "page limit reached" : preview(d.stopped, 80)}`);
	const errors = Array.isArray(d.errors) ? d.errors : Object.entries(object(d.errors)).map(([url, error]) => ({ url, error }));
	if (errors.length) {
		lines.push(`${errors.length} errors`);
		for (const error of errors.slice(0, 2)) { const e = object(error); lines.push(`   ${preview(first(e.url, e.backend), 70)}: ${preview(first(e.error, error), 140)}`); }
	}
	const warnings = [...array(object(result.details).warnings), ...(data === result.details ? [] : array(d.warnings))];
	if (warnings.length) {
		lines.push(`${warnings.length} warnings`);
		for (const warning of warnings.slice(0, 2)) lines.push(`   ${preview(warning, 180)}`);
	}
	if (d.cache_status === "unavailable") lines.push("Cache status unavailable");
	return lines;
}

export function fullDetails(result: DisplayResult): string {
	// Preserve the original text (including warnings); details may have extra
	// metadata not present in content. Expansion is intentionally not truncated.
	const text = contentText(result);
	let details = "";
	try {
		details = result.details === undefined ? "" : JSON.stringify(result.details, null, 2);
		const d = object(result.details);
		const data = d.result !== undefined ? d.result : result.details;
		const compact = JSON.stringify(data);
		const warnings = array(d.warnings).map(scalar);
		const expected = compact + (warnings.length ? `\n\nwarnings:\n${warnings.join("\n")}` : "");
		// Common CLI/crawl path: show the full structured response just once.
		if (details && text === expected) return safeText(details);
	} catch { details = "[Details could not be serialized]"; }
	return safeText([text, details && `Details:\n${details}`].filter(Boolean).join("\n\n"));
}

export function renderers(surface: Surface): ToolRenderers {
	return {
		renderCall(args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold(`ketch ${surface}`)) + " " + theme.fg("accent", callSummary(surface, args)), 0, 0);
		},
		renderResult(result, { expanded, isPartial }, theme, context) {
			if (isPartial) return new Text(theme.fg("muted", "Working…"), 0, 0);
			const lines = resultSummary(surface, result, context?.isError);
			const heading = theme.fg(context?.isError || (result as DisplayResult).isError ? "error" : "toolTitle", theme.bold(lines[0]));
			const body = lines.slice(1).map(line => /^\d+\. /.test(line) ? theme.fg("text", line) : theme.fg("muted", line)).join("\n");
			const extra = expanded ? fullDetails(result) : theme.fg("dim", "Expand tool output for full details");
			return new Text([heading, body, extra].filter(Boolean).join("\n"), 0, 0);
		},
	};
}
