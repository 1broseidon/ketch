// pi extension for ketch: first-class tools over the five research surfaces
// (search, code, docs, scrape, crawl) plus tag, each a thin shell-out to
// `ketch <cmd> … --json`. The parameter sets and descriptions mirror the MCP
// tools in mcp/ so an agent sees the same surface over either transport;
// config-level settings (API keys, cache TTL, browser) stay operator-configured
// and are never tool params.
//
// The binary is the prerequisite, not a bundle: nothing here runs at load time,
// so a missing `ketch` only surfaces as a [precondition] error on the first call.
import { existsSync } from "node:fs";
import { join } from "node:path";
import type { ExtensionAPI, ExtensionCommandContext } from "@earendil-works/pi-coding-agent";
import { Type, type Static } from "typebox";
import { renderers } from "./render.ts";

const INSTALL_HINT = "brew install ketch, npm i -g ketch-cli, or go install github.com/1broseidon/ketch@latest";
// Fallback when no `ketch` is on PATH: run the published npm wrapper through npx.
// Pinned so the plugin never executes an unreviewed release; KETCH_PI_NO_NPX=1 opts out.
// Bumped in every release commit; the unit tests and the release workflow
// fail when it differs from the newest CHANGELOG.md version.
export const NPX_PACKAGE = "ketch-cli@0.22.0";
const NPX_FIRST_RUN_EXTRA_MS = 120_000;
const KETCH_MISSING = `[precondition] ketch not on PATH: ${INSTALL_HINT}`;

// Exit codes → the same stable prefixes the MCP server puts on tool errors
// (cmd/exit.go, mcp/errors.go). Anything else non-zero is [upstream].
const EXIT_PREFIX: Record<number, string> = {
	2: "validation",
	3: "not_found",
	4: "upstream",
	5: "precondition",
	6: "cancelled",
};

// Per-tool wall-clock budgets (ms). Crawl matches the MCP server's 3-minute
// cap; the fetching tools allow for a headless-browser render.
const TIMEOUT = {
	search: 120_000,
	code: 60_000,
	docs: 60_000,
	scrape: 120_000,
	crawl: 180_000,
	tag: 30_000,
	doctor: 90_000,
} as const;

// Crawl bounds, mirroring mcp/crawl.go. The CLI has no page cap, so the tool
// runs the crawl within its time budget and keeps the first max_pages pages.
const CRAWL_DEFAULT_MAX_PAGES = 30;
const CRAWL_MAX_PAGES = 100;

// Bound what we echo back from stderr so a chatty failure can't flood the model.
const STDERR_LIMIT = 2000;

// ---------------------------------------------------------------------------
// Schemas (TypeBox). Descriptions are the MCP jsonschema tags, lightly edited
// where the CLI differs.

const tagParam = (what: string) =>
	Type.Optional(Type.String({ description: `bookmark ${what} under this tag` }));

const SearchParams = Type.Object({
	query: Type.String({ description: "the search query" }),
	backend: Type.Optional(
		Type.String({ description: "search backend (default: configured; auto is a keyless fallback chain)" }),
	),
	multi: Type.Optional(
		Type.Array(Type.String(), {
			description: 'backends to query and rank-fuse, e.g. ["brave","ddg"] or ["all"]; exclusive with backend/random',
		}),
	),
	random: Type.Optional(
		Type.Array(Type.String(), {
			description: 'backends to shuffle with fallback, e.g. ["brave","ddg"] or ["all"]; exclusive with backend/multi',
		}),
	),
	limit: Type.Optional(Type.Integer({ minimum: 1, description: "max results (default: configured)" })),
	searxng_url: Type.Optional(
		Type.String({ description: "override SearXNG instance URL (searxng only)" }),
	),
	scrape: Type.Optional(Type.Boolean({ description: "fetch each result URL as markdown" })),
	trim: Type.Optional(Type.Boolean({ description: "strip markdown formatting from scraped content (with scrape)" })),
	max_chars: Type.Optional(
		Type.Integer({
			minimum: 0,
			description: "truncate scraped content to N chars per result (with scrape; 0 = off)",
		}),
	),
	tag: tagParam("each result"),
});

const CodeParams = Type.Object({
	query: Type.String({ description: "the code search query" }),
	backend: Type.Optional(
		Type.String({ description: "code backend: grepapp, sourcegraph, github (default: configured)" }),
	),
	lang: Type.Optional(Type.String({ description: "language filter" })),
	repo: Type.Optional(
		Type.String({ description: "search one repo as owner/name or github.com URL (matched exactly)" }),
	),
	regexp: Type.Optional(Type.Boolean({ description: "treat query as regex (grepapp, sourcegraph only)" })),
	limit: Type.Optional(Type.Integer({ minimum: 1, description: "max results (default: configured)" })),
	tag: tagParam("results"),
});

const DocsParams = Type.Object({
	query: Type.Optional(
		Type.String({
			description: "docs question, or with resolve a package name or docs URL (never a question); omitted with section",
		}),
	),
	backend: Type.Optional(
		Type.String({ description: "docs backend: search (default, no key) or context7" }),
	),
	library: Type.Optional(
		Type.String({
			description:
				"target library. search: docs URL, name@version (e.g. django@5.2), npm:/pypi:/crates:/go:/github: spec, or owner/repo; sub-URL scopes an indexed root. context7: ID from resolve (e.g. /vercel/next.js)",
		}),
	),
	section: Type.Optional(
		Type.String({
			description: "read one section in full: url#anchor or page URL from the local index or page cache (no query)",
		}),
	),
	tokens: Type.Optional(
		Type.Integer({ minimum: 1, description: "token budget (default 4000; leave unset for search)" }),
	),
	limit: Type.Optional(
		Type.Integer({
			minimum: 1,
			description: "max results (search: unset uses token budget; context7: configured limit)",
		}),
	),
	resolve: Type.Optional(
		Type.Boolean({
			description:
				"resolve a package name or docs URL instead of answering (never a question); empty query lists indexed libraries",
		}),
	),
	tag: Type.Optional(Type.String({ description: "bookmark results under this tag (not with resolve)" })),
	decisions: Type.Optional(
		Type.String({
			description:
				"decision provider when no library is named: a provider by name, auto or off (default: configured docs_decisions)",
		}),
	),
});

const ScrapeParams = Type.Object({
	url: Type.Optional(Type.String({ description: "single URL to fetch (exclusive with urls)" })),
	urls: Type.Optional(
		Type.Array(Type.String(), {
			description: "URLs to fetch concurrently; per-URL errors return in the result (exclusive with url)",
		}),
	),
	selector: Type.Optional(Type.String({ description: "CSS selector to extract, skips readability (not with raw)" })),
	trim: Type.Optional(Type.Boolean({ description: "strip markdown formatting, text only (not with raw)" })),
	max_chars: Type.Optional(
		Type.Integer({ minimum: 0, description: "truncate output to N chars per page (0 = off)" }),
	),
	no_cache: Type.Optional(Type.Boolean({ description: "bypass page cache" })),
	raw: Type.Optional(Type.Boolean({ description: "return raw HTML instead of markdown" })),
	force_browser: Type.Optional(Type.Boolean({ description: "always render with configured headless browser" })),
	no_llms_txt: Type.Optional(Type.Boolean({ description: "skip /llms.txt probe on bare domains" })),
	concurrency: Type.Optional(
		Type.Integer({ minimum: 1, maximum: 16, description: "max concurrent fetches for urls (default 5, max 16)" }),
	),
	tag: tagParam("fetched pages"),
});

const CrawlParams = Type.Object({
	url: Type.String({ description: "seed URL (same host only)" }),
	depth: Type.Optional(Type.Integer({ minimum: 1, description: "max BFS depth (default 3)" })),
	sitemap: Type.Optional(Type.Boolean({ description: "treat seed URL as a sitemap" })),
	max_pages: Type.Optional(
		Type.Integer({
			minimum: 1,
			description: `page cap (default ${CRAWL_DEFAULT_MAX_PAGES}, max ${CRAWL_MAX_PAGES})`,
		}),
	),
	allow: Type.Optional(
		Type.Array(Type.String(), { description: "path substring filters (URL must match at least one)" }),
	),
	deny: Type.Optional(Type.Array(Type.String(), { description: "regex patterns to skip" })),
	max_chars: Type.Optional(
		Type.Integer({ minimum: 0, description: "truncate each page to N chars (0 = off)" }),
	),
	no_cache: Type.Optional(Type.Boolean({ description: "bypass page cache" })),
	tag: tagParam("crawled pages"),
});

const TagParams = Type.Object({
	operation: Type.Union(
		[Type.Literal("add"), Type.Literal("show"), Type.Literal("list"), Type.Literal("remove")],
		{ description: "add, show, list, or remove" },
	),
	tag: Type.Optional(Type.String({ description: "tag name (required except for list)" })),
	limit: Type.Optional(Type.Integer({ minimum: 0, description: "max entries for show (default 50; 0 = all)" })),
	urls: Type.Optional(
		Type.Array(Type.String(), {
			description: "URLs to add or remove (omit on remove to drop the whole tag)",
		}),
	),
});

type SearchInput = Static<typeof SearchParams>;
type CodeInput = Static<typeof CodeParams>;
type DocsInput = Static<typeof DocsParams>;
type ScrapeInput = Static<typeof ScrapeParams>;
type CrawlInput = Static<typeof CrawlParams>;
type TagInput = Static<typeof TagParams>;

// ---------------------------------------------------------------------------
// Shell-out plumbing.

interface ExecResult {
	stdout: string;
	stderr: string;
	code: number;
	killed: boolean;
}

interface RunOk {
	ok: true;
	stdout: string;
	stderr: string;
}

interface RunFailed {
	ok: false;
	error: string;
	/** true when the process was killed (timeout or abort) and stdout may be partial */
	killed: boolean;
	stdout: string;
	stderr: string;
}

type RunResult = RunOk | RunFailed;

interface ToolResult {
	content: { type: "text"; text: string }[];
	details: unknown;
	isError?: boolean;
}

/**
 * Appends `--flag=value` when value is set. The `=` form binds the value to the
 * flag even when it starts with `-`, so a selector or tag can't become a flag.
 */
function flag(args: string[], name: string, value: string | number | undefined): void {
	if (value === undefined || value === "") return;
	args.push(`${name}=${value}`);
}

/** Appends a boolean `--flag` when true. */
function toggle(args: string[], name: string, value: boolean | undefined): void {
	if (value) args.push(name);
}

/**
 * Appends one `--flag=value` per entry for a Cobra StringSlice flag. pflag reads
 * each value as a CSV record, so a bare comma would split one pattern in two
 * (`[0-9]{1,4}`) and a bare quote is a parse error; quoting every value as a
 * single CSV field passes it through verbatim. The `=` form keeps a value that
 * starts with `-` from being read as a flag.
 */
function each(args: string[], name: string, values: string[] | undefined): void {
	for (const v of values ?? []) {
		if (v === "") continue;
		args.push(`${name}=${csvField(v)}`);
	}
}

/** Quotes s as one RFC 4180 field, the inverse of encoding/csv's reader. */
export function csvField(s: string): string {
	return `"${s.replaceAll('"', '""')}"`;
}

/**
 * Ends the option list: everything after `--` is positional to Cobra, so a
 * query or URL that starts with `-` (`--help`, `--cookie-file=…`) is searched or
 * fetched as text and never parsed as a flag, and a crawl seed or tag name that
 * spells a subcommand (`status`, `list`) is never dispatched as one.
 */
function positional(args: string[], ...values: string[]): string[] {
	args.push("--", ...values);
	return args;
}

function tail(s: string, limit: number): string {
	const t = s.trim();
	return t.length <= limit ? t : "…" + t.slice(t.length - limit);
}

function fail(message: string, details?: unknown): ToolResult {
	return { content: [{ type: "text", text: message }], details, isError: true };
}

function validation(message: string): ToolResult {
	return fail(`[validation] ${message}`);
}

function parseJSON(text: string): unknown {
	try {
		return JSON.parse(text);
	} catch {
		return undefined;
	}
}

/** stderr lines from a successful call are warnings (e.g. a bookmark write that failed). */
function warningsOf(stderr: string): string[] {
	return stderr
		.split("\n")
		.map((l) => l.trim())
		.filter(Boolean);
}

/**
 * The executable every tool runs, resolved once per call.
 *
 * Windows: an npm-installed `ketch` on PATH is a `.cmd` shim, and Bun refuses to
 * hand an argument containing a cmd.exe metacharacter (a quote, `&`, `^`) to a
 * .bat/.cmd — which is exactly what a multi-URL scrape (`["u1","u2"]`) or a
 * `--tag` value holding a quote produces. So resolve the native executable the
 * shim would have exec'd and call it directly; the path carries no version, so
 * an npm upgrade needs no reconfiguration. Unknown layouts keep the plain name.
 *
 * KETCH_PI_BIN names a specific binary and skips the resolution entirely.
 */
export function resolveKetchCommand(
	platform: string = process.platform,
	arch: string = process.arch,
	pathEnv: string = process.env.PATH ?? "",
	exists: (p: string) => boolean = existsSync,
	override: string | null = process.env.KETCH_PI_BIN ?? null,
): string {
	if (override) return override;
	if (platform !== "win32") return "ketch";
	const pkg = `@ketch-cli/win32-${arch === "arm64" ? "arm64" : "x64"}`;
	for (const dir of pathEnv.split(";")) {
		if (!dir || !exists(join(dir, "ketch.cmd"))) continue;
		for (const candidate of [
			join(dir, "node_modules", "ketch-cli", "node_modules", pkg, "bin", "ketch.exe"),
			join(dir, "node_modules", pkg, "bin", "ketch.exe"),
		]) {
			if (exists(candidate)) return candidate;
		}
	}
	return "ketch";
}

/**
 * Runs `ketch <args>`; if the binary is not on PATH, falls back to the pinned
 * npm wrapper via `npx -y` (unless KETCH_PI_NO_NPX is set).
 */
async function runKetch(
	pi: ExtensionAPI,
	args: string[],
	signal: AbortSignal | undefined,
	timeout: number,
): Promise<RunResult> {
	const direct = await runCommand(pi, resolveKetchCommand(), args, args, signal, timeout);
	if (direct.error !== KETCH_MISSING || process.env.KETCH_PI_NO_NPX) return direct;
	const viaNpx = await runCommand(pi, "npx", ["-y", NPX_PACKAGE, ...args], args, signal, timeout + NPX_FIRST_RUN_EXTRA_MS);
	// npm's own chatter (update notices) is not a ketch warning.
	const stderr = viaNpx.stderr.split("\n").filter((l) => !/^npm (notice|warn)\b/.test(l.trim())).join("\n");
	return { ...viaNpx, stderr };
}

/** Runs one command and classifies the outcome under the error taxonomy. */
async function runCommand(
	pi: ExtensionAPI,
	command: string,
	execArgs: string[],
	args: string[],
	signal: AbortSignal | undefined,
	timeout: number,
): Promise<RunResult> {
	let r: ExecResult;
	try {
		r = await pi.exec(command, execArgs, { signal, timeout });
	} catch (err) {
		const e = err as NodeJS.ErrnoException;
		if (e?.code === "ENOENT") {
			return { ok: false, error: KETCH_MISSING, killed: false, stdout: "", stderr: "" };
		}
		return {
			ok: false,
			error: `[upstream] ketch could not be started: ${e?.message ?? String(err)}`,
			killed: false,
			stdout: "",
			stderr: "",
		};
	}
	const stderr = r.stderr ?? "";
	const stdout = r.stdout ?? "";
	if (r.killed) {
		const why = signal?.aborted ? "the call was cancelled" : `the call exceeded its ${Math.round(timeout / 1000)}s budget`;
		return { ok: false, error: `[cancelled] ketch ${args[0]}: ${why}`, killed: true, stdout, stderr };
	}
	if (r.code === 0) {
		return { ok: true, stdout, stderr };
	}
	// pi.exec swallows the spawn error, so a missing binary comes back as a bare
	// non-zero exit with nothing on either stream — ketch itself always says
	// something on stderr when it fails.
	if (stdout === "" && stderr === "") {
		return { ok: false, error: KETCH_MISSING, killed: false, stdout, stderr };
	}
	if (/ENOENT/.test(stderr)) {
		return { ok: false, error: KETCH_MISSING, killed: false, stdout, stderr };
	}
	const kind = EXIT_PREFIX[r.code] ?? "upstream";
	// Cobra prefixes the returned error with "Error: "; the taxonomy prefix
	// replaces it, as on the MCP server.
	const detail = tail(stderr.replace(/^\s*Error: /, ""), STDERR_LIMIT) || `ketch exited ${r.code}`;
	return { ok: false, error: `[${kind}] ${detail}`, killed: false, stdout, stderr };
}

/**
 * Runs a command whose stdout is the tool content. `details` carries the
 * parsed JSON when stdout is JSON, plus any stderr warnings.
 */
async function passthrough(
	pi: ExtensionAPI,
	args: string[],
	signal: AbortSignal | undefined,
	timeout: number,
): Promise<ToolResult> {
	const r = await runKetch(pi, args, signal, timeout);
	if (!r.ok) {
		return fail(r.error, { stderr: tail(r.stderr, STDERR_LIMIT) || undefined });
	}
	const warnings = warningsOf(r.stderr);
	let text = r.stdout.trim();
	if (warnings.length > 0) {
		text += `\n\nwarnings:\n${warnings.join("\n")}`;
	}
	const parsed = parseJSON(r.stdout);
	return {
		content: [{ type: "text", text }],
		details: { result: parsed, warnings: warnings.length > 0 ? warnings : undefined },
	};
}

// ---------------------------------------------------------------------------
// Argument construction, one function per tool so tests can pin each down.

// Each builder puts the options first and the user's positional inputs last,
// after `--` (see positional).

export function searchArgs(p: SearchInput): string[] {
	const args = ["search", "--json"];
	flag(args, "--backend", p.backend);
	if (p.multi && p.multi.length > 0) args.push(`--multi=${p.multi.join(",")}`);
	if (p.random && p.random.length > 0) args.push(`--random=${p.random.join(",")}`);
	flag(args, "--limit", p.limit);
	flag(args, "--searxng-url", p.searxng_url);
	toggle(args, "--scrape", p.scrape);
	toggle(args, "--trim", p.trim);
	flag(args, "--max-chars", p.max_chars);
	flag(args, "--tag", p.tag);
	return positional(args, p.query);
}

export function codeArgs(p: CodeInput): string[] {
	const args = ["code", "--json"];
	flag(args, "--backend", p.backend);
	flag(args, "--lang", p.lang);
	flag(args, "--repo", p.repo);
	toggle(args, "--regex", p.regexp);
	flag(args, "--limit", p.limit);
	flag(args, "--tag", p.tag);
	return positional(args, p.query);
}

export function docsArgs(p: DocsInput): string[] {
	const args = ["docs", "--json"];
	flag(args, "--backend", p.backend);
	flag(args, "--library", p.library);
	flag(args, "--section", p.section);
	flag(args, "--tokens", p.tokens);
	flag(args, "--limit", p.limit);
	toggle(args, "--resolve", p.resolve);
	flag(args, "--tag", p.tag);
	flag(args, "--decisions", p.decisions);
	return p.section ? args : positional(args, p.query ?? "");
}

/**
 * The URLs always travel as one JSON-array argument, never as bare positionals:
 * the CLI reads a lone positional that names an existing path as a file of URLs
 * (`./urls.txt`), whereas each entry of a JSON array is taken as a URL and
 * validated as one.
 */
export function scrapeArgs(p: ScrapeInput, urls: string[]): string[] {
	const args = ["scrape", "--json"];
	flag(args, "--select", p.selector);
	toggle(args, "--raw", p.raw);
	toggle(args, "--force-browser", p.force_browser);
	toggle(args, "--no-llms-txt", p.no_llms_txt);
	toggle(args, "--trim", p.trim);
	flag(args, "--max-chars", p.max_chars);
	toggle(args, "--no-cache", p.no_cache);
	flag(args, "--concurrency", p.concurrency);
	flag(args, "--tag", p.tag);
	return positional(args, JSON.stringify(urls));
}

export function crawlArgs(p: CrawlInput): string[] {
	const args = ["crawl", "--json"];
	flag(args, "--depth", p.depth);
	toggle(args, "--sitemap", p.sitemap);
	each(args, "--allow", p.allow);
	each(args, "--deny", p.deny);
	toggle(args, "--no-cache", p.no_cache);
	flag(args, "--tag", p.tag);
	return positional(args, p.url);
}

export function tagArgs(p: TagInput): string[] {
	switch (p.operation) {
		case "list":
			return ["tag", "list", "--json"];
		case "show": {
			const args = ["tag", "show", "--json"];
			if (p.limit !== undefined) args.push(`--limit=${p.limit}`);
			return positional(args, p.tag ?? "");
		}
		case "add":
			return positional(["tag", "add", "--json"], p.tag ?? "", ...(p.urls ?? []));
		case "remove":
			return positional(["tag", "remove", "--json"], p.tag ?? "", ...(p.urls ?? []));
	}
}

// ---------------------------------------------------------------------------
// Crawl: the CLI streams one JSON object per page; reshape to the MCP tool's
// {pages, errors, stopped} output and apply the page/char bounds client-side.

interface CrawlPage {
	url: string;
	title: string;
	markdown: string;
}

interface CrawlOutput {
	pages: CrawlPage[];
	errors?: { url: string; error: string }[];
	stopped?: "max_pages" | "timeout";
	warnings?: string[];
}

export function assembleCrawl(stdout: string, stderr: string, maxPages: number, maxChars: number, killed: boolean): CrawlOutput {
	const pages: CrawlPage[] = [];
	let seen = 0;
	for (const line of stdout.split("\n")) {
		if (!line.trim()) continue;
		const obj = parseJSON(line) as { url?: string; title?: string; body?: string } | undefined;
		if (!obj || typeof obj.url !== "string") continue; // a line cut off by the kill
		seen++;
		if (pages.length >= maxPages) continue;
		let markdown = obj.body ?? "";
		if (maxChars > 0 && markdown.length > maxChars) markdown = markdown.slice(0, maxChars) + "\n[truncated]";
		pages.push({ url: obj.url, title: obj.title ?? "", markdown });
	}
	const errors: { url: string; error: string }[] = [];
	const warnings: string[] = [];
	for (const raw of stderr.split("\n")) {
		const line = raw.trim();
		const m = /^warn: (\S+): (.*)$/.exec(line);
		if (m) {
			errors.push({ url: m[1], error: m[2] });
		} else if (/^\{/.test(line)) {
			warnings.push(line); // structured --json warnings (bookmark writes)
		}
		// the trailing stats block (seed/pages/errors/duration) is not an error
	}
	const out: CrawlOutput = { pages };
	if (errors.length > 0) out.errors = errors;
	if (warnings.length > 0) out.warnings = warnings;
	if (seen > maxPages) out.stopped = "max_pages";
	else if (killed) out.stopped = "timeout";
	return out;
}

// ---------------------------------------------------------------------------

export default function ketch(pi: ExtensionAPI): void {
	pi.registerTool({
		name: "ketch_search",
		...renderers("search"),
		label: "ketch search",
		description:
			"Search the web and return results (title, url, description). Omit backend to use the configured default. Set scrape=true to fetch each result as markdown, multi to rank-fuse several backends, or random to shuffle with fallback.",
		promptSnippet: "ketch_search for the open web; omit backend to use the operator's default",
		parameters: SearchParams,
		async execute(_id, p, signal) {
			if (!p.query?.trim()) return validation("query is required");
			if (p.multi?.length && p.random?.length) return validation("multi and random are mutually exclusive");
			if (p.backend && (p.multi?.length || p.random?.length)) {
				return validation("backend is mutually exclusive with multi and random");
			}
			return passthrough(pi, searchArgs(p), signal, TIMEOUT.search);
		},
	});

	pi.registerTool({
		name: "ketch_code",
		...renderers("code"),
		label: "ketch code",
		description:
			"Search code across open-source repositories (grep.app, Sourcegraph, or GitHub; default: configured). Returns file/line snippets.",
		promptSnippet: "ketch_code for how real OSS projects call an API",
		parameters: CodeParams,
		async execute(_id, p, signal) {
			if (!p.query?.trim()) return validation("query is required");
			return passthrough(pi, codeArgs(p), signal, TIMEOUT.code);
		},
	});

	pi.registerTool({
		name: "ketch_docs",
		...renderers("docs"),
		label: "ketch docs",
		description:
			"Answer a documentation question directly: pass the question as query (no resolve, leave tokens unset). " +
			"The default search backend checks the local index first, else searches the web and reads the top matching pages (check read for which site answered; not_on_pages checks only the pages read). " +
			"Pass library for a specific library, name@version, or docs URL, resolve=true for a package name or docs URL (never a question), or backend=context7 for Context7 snippets. " +
			"Two rules: text that is truncated, behind …, or only in outline is unread — read it with ketch_docs(section=\"<url#anchor>\") (no query) before stating what it says, and read an other_results hit with ketch_docs(library=\"<url>\", query=\"<your question>\"). " +
			"Whatever your answer states that no result prints is your own knowledge: label it so, never cite the docs for it.",
		promptSnippet: "ketch_docs for documentation; ask the question, then read a cut or outlined section with section before stating what it says",
		parameters: DocsParams,
		async execute(_id, p, signal) {
			if (p.section && (p.resolve || p.query?.trim())) return validation("section takes no query and no resolve");
			// search's resolve with no query lists every indexed library
			if (!p.section && !(p.resolve && p.backend !== "context7") && !p.query?.trim()) return validation("query is required");
			if (p.resolve && p.library) return validation("resolve and library are mutually exclusive");
			if (p.resolve && p.tag) return validation("tag is not available with resolve");
			return passthrough(pi, docsArgs(p), signal, TIMEOUT.docs);
		},
	});

	pi.registerTool({
		name: "ketch_scrape",
		...renderers("scrape"),
		label: "ketch scrape",
		description:
			"Fetch one or more HTTP(S) URLs and extract main content as markdown (or raw HTML with raw=true). Bare domains auto-probe /llms.txt unless no_llms_txt is set. Set max_chars on unknown pages; batch urls report per-URL errors in the result.",
		promptSnippet: "ketch_scrape when you already have the URL; always set max_chars on pages you have not seen",
		parameters: ScrapeParams,
		async execute(_id, p, signal) {
			const single = p.url?.trim();
			const batch = (p.urls ?? []).map((u) => u.trim()).filter(Boolean);
			if (!single && batch.length === 0) return validation("exactly one of url or urls is required");
			if (single && batch.length > 0) return validation("url and urls are mutually exclusive");
			if (p.raw && (p.trim || p.selector)) return validation("raw is incompatible with trim and selector");
			const urls = single ? [single] : batch;
			return passthrough(pi, scrapeArgs(p, urls), signal, TIMEOUT.scrape);
		},
	});

	pi.registerTool({
		name: "ketch_crawl",
		...renderers("crawl"),
		label: "ketch crawl",
		description:
			`BFS-crawl an HTTP(S) site from a seed URL (same host only) and return extracted markdown per page. Bounded to max_pages (default ${CRAWL_DEFAULT_MAX_PAGES}, max ${CRAWL_MAX_PAGES}) and a 3-minute budget; partial results set stopped. Set max_chars to bound each page.`,
		promptSnippet: "ketch_crawl only when one page is not enough; keep depth and max_pages small",
		parameters: CrawlParams,
		async execute(_id, p, signal) {
			if (!p.url?.trim()) return validation("url is required");
			const maxPages = Math.min(p.max_pages && p.max_pages > 0 ? p.max_pages : CRAWL_DEFAULT_MAX_PAGES, CRAWL_MAX_PAGES);
			const r = await runKetch(pi, crawlArgs(p), signal, TIMEOUT.crawl);
			if (!r.ok) {
				// The deadline firing with pages already streamed is a bounded
				// result, not a failure — return what was collected, like the MCP
				// tool does. A client abort or an empty kill is still an error.
				const partial = r.killed && !signal?.aborted && r.stdout.trim() !== "";
				if (!partial) return fail(r.error, { stderr: tail(r.stderr, STDERR_LIMIT) || undefined });
			}
			const out = assembleCrawl(r.stdout, r.stderr, maxPages, p.max_chars ?? 0, !r.ok);
			return { content: [{ type: "text", text: JSON.stringify(out) }], details: out };
		},
	});

	pi.registerTool({
		name: "ketch_tag",
		...renderers("tag"),
		label: "ketch tag",
		description:
			"Bookmark research URLs under project or topic tags and revisit them across sessions (local only, no network). Operations: add, show (newest 50; limit=0 for all), list, remove. Expired entries return cached=false and can be re-fetched with ketch_scrape.",
		promptSnippet: "ketch_tag show <tag> before searching again for sources you already found",
		parameters: TagParams,
		async execute(_id, p, signal) {
			const op = p.operation;
			if (!op) return validation("operation is required (add, show, list or remove)");
			if (op !== "list" && !p.tag?.trim()) return validation(`tag is required for operation=${op}`);
			if (p.limit !== undefined && op !== "show") return validation("limit is only valid for show");
			if (op === "add" && !(p.urls && p.urls.length > 0)) return validation("urls is required for operation=add");
			return passthrough(pi, tagArgs(p), signal, TIMEOUT.tag);
		},
	});

	// Operator convenience: one keystroke to the same health check the CLI
	// offers. Not a tool — doctor is an operator action, same as over MCP.
	pi.registerCommand("ketch-doctor", {
		description: "Run `ketch doctor`: live health check of every ketch backend, the browser, the cache and the tag index",
		handler: async (_args: string, ctx: ExtensionCommandContext) => {
			const r = await runKetch(pi, ["doctor"], undefined, TIMEOUT.doctor);
			if (r.ok) {
				ctx.ui.notify(r.stdout.trim() || "ketch doctor: all configured surfaces healthy", "info");
				return;
			}
			// Exit 5 is doctor's own verdict (a configured surface is broken) and
			// carries the report on stdout; anything else is the failure itself.
			const report = r.stdout.trim();
			ctx.ui.notify(report ? `${report}\n\n${r.error}` : r.error, report ? "warning" : "error");
		},
	});
}
