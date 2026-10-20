// pi extension for ketch: first-class tools over the five research surfaces
// (search, code, docs, scrape, crawl) plus tag, each a thin shell-out to
// `ketch <cmd> … --json`. The parameter sets and descriptions mirror the MCP
// tools in mcp/ so an agent sees the same surface over either transport;
// config-level settings (API keys, cache TTL, browser) stay operator-configured
// and are never tool params.
//
// The binary is the prerequisite, not a bundle: nothing here runs at load time,
// so a missing `ketch` only surfaces as a [precondition] error on the first call.
import type { ExtensionAPI, ExtensionCommandContext } from "@earendil-works/pi-coding-agent";
import { Type, type Static } from "typebox";
import { renderers } from "./render.ts";

const INSTALL_HINT = "brew install ketch, npm i -g ketch-cli, or go install github.com/1broseidon/ketch@latest";
// Fallback when no `ketch` is on PATH: run the published npm wrapper through npx.
// Pinned so the plugin never executes an unreviewed release; KETCH_PI_NO_NPX=1 opts out.
// Bumped in every release commit; the unit tests and the release workflow
// fail when it differs from the newest CHANGELOG.md version.
export const NPX_PACKAGE = "ketch-cli@0.20.0";
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

// Appended to every tool description, verbatim from mcp/errors.go.
const ERR_TAXONOMY =
	" Errors carry a machine-readable prefix: [validation] fix the input; [not_found] no such resource/match; [upstream] backend or network failure, retrying may help; [precondition] ketch is not installed or its configuration is missing; [cancelled] the call was cancelled or timed out.";

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
	Type.Optional(Type.String({ description: `record each ${what} under this tag, retrievable later with ketch_tag` }));

const SearchParams = Type.Object({
	query: Type.String({ description: "the search query" }),
	backend: Type.Optional(
		Type.String({
			description:
				"search backend (default: the configured backend; `ketch config` lists the available ones). auto is a keyless fallback chain, not a provider",
		}),
	),
	multi: Type.Optional(
		Type.Array(Type.String(), {
			description:
				'federated search: backends to query and rank-fuse (reciprocal rank fusion), e.g. ["brave","ddg"]; use ["all"] for every usable backend; mutually exclusive with backend and random; results gain a backends field showing which engines returned each',
		}),
	),
	random: Type.Optional(
		Type.Array(Type.String(), {
			description:
				'random provider selection with sequential fallback on errors, e.g. ["brave","ddg"]; use ["all"] for every usable backend; mutually exclusive with backend and multi',
		}),
	),
	limit: Type.Optional(Type.Integer({ minimum: 1, description: "max number of results (default: the configured limit)" })),
	searxng_url: Type.Optional(
		Type.String({ description: "override the configured SearXNG instance URL (searxng backend only)" }),
	),
	scrape: Type.Optional(
		Type.Boolean({ description: "also fetch each result URL and fill its content field with extracted markdown" }),
	),
	trim: Type.Optional(
		Type.Boolean({ description: "strip markdown formatting from scraped content, keep text only (with scrape)" }),
	),
	max_chars: Type.Optional(
		Type.Integer({
			minimum: 0,
			description: "truncate each result's scraped content to N characters (with scrape; 0 = disabled)",
		}),
	),
	tag: Type.Optional(
		Type.String({
			description:
				"record each result under this tag; with scrape the entry gets the fetched page's title and description",
		}),
	),
});

const CodeParams = Type.Object({
	query: Type.String({ description: "the code search query" }),
	backend: Type.Optional(
		Type.String({ description: "code search backend: grepapp, sourcegraph, github (default: the configured backend)" }),
	),
	lang: Type.Optional(Type.String({ description: "language filter appended to the query" })),
	repo: Type.Optional(
		Type.String({
			description:
				"search one repository, owner/name (a github.com URL also works); matched exactly on every backend",
		}),
	),
	regexp: Type.Optional(
		Type.Boolean({ description: "interpret query as a regular expression (grepapp, sourcegraph only)" }),
	),
	limit: Type.Optional(Type.Integer({ minimum: 1, description: "max number of results (default: the configured limit)" })),
	tag: tagParam("result"),
});

const DocsParams = Type.Object({
	query: Type.String({ description: "the docs search query, or a library name when resolve is true" }),
	backend: Type.Optional(Type.String({ description: "docs backend: context7 (default: the configured backend)" })),
	library: Type.Optional(
		Type.String({
			description:
				"library ID to fetch docs from directly, skipping the resolve step; requires a backend with library operations",
		}),
	),
	tokens: Type.Optional(
		Type.Integer({ minimum: 1, description: "library token budget when library is set (default 4000)" }),
	),
	limit: Type.Optional(
		Type.Integer({
			minimum: 1,
			description: "max number of results (default: the configured limit; with library set, unbounded unless given)",
		}),
	),
	resolve: Type.Optional(
		Type.Boolean({ description: "resolve a library name to library IDs instead of searching docs" }),
	),
	tag: Type.Optional(
		Type.String({
			description: "record each result under this tag, retrievable later with ketch_tag; not with resolve",
		}),
	),
});

const ScrapeParams = Type.Object({
	url: Type.Optional(Type.String({ description: "the URL to scrape (exactly one of url or urls)" })),
	urls: Type.Optional(
		Type.Array(Type.String(), {
			description:
				"URLs to scrape concurrently; failures come back as per-URL error entries (exactly one of url or urls)",
		}),
	),
	selector: Type.Optional(
		Type.String({
			description: "CSS selector to extract specific elements, skips readability (incompatible with raw)",
		}),
	),
	trim: Type.Optional(
		Type.Boolean({ description: "strip markdown formatting, keep content text only (incompatible with raw)" }),
	),
	max_chars: Type.Optional(
		Type.Integer({
			minimum: 0,
			description: "truncate markdown (or raw HTML) output to N characters per page (0 = disabled)",
		}),
	),
	no_cache: Type.Optional(Type.Boolean({ description: "bypass the page cache" })),
	raw: Type.Optional(Type.Boolean({ description: "return raw HTML instead of extracted markdown" })),
	force_browser: Type.Optional(
		Type.Boolean({
			description:
				"always render via the configured headless browser, skipping JS-shell auto-detection (requires a configured browser)",
		}),
	),
	no_llms_txt: Type.Optional(
		Type.Boolean({ description: "disable automatic /llms.txt detection for bare domain URLs" }),
	),
	concurrency: Type.Optional(
		Type.Integer({ minimum: 1, maximum: 16, description: "max concurrent fetches for multi-URL scrapes (default 5)" }),
	),
	tag: tagParam("fetched page"),
});

const CrawlParams = Type.Object({
	url: Type.String({ description: "seed URL to crawl (BFS, same host only)" }),
	depth: Type.Optional(Type.Integer({ minimum: 1, description: "max BFS depth (default 3)" })),
	sitemap: Type.Optional(Type.Boolean({ description: "treat the seed URL as a sitemap" })),
	max_pages: Type.Optional(
		Type.Integer({
			minimum: 1,
			description: `stop after this many pages (default ${CRAWL_DEFAULT_MAX_PAGES}, capped at ${CRAWL_MAX_PAGES})`,
		}),
	),
	allow: Type.Optional(
		Type.Array(Type.String(), {
			description: "path substring filters; a URL must contain at least one to be crawled",
		}),
	),
	deny: Type.Optional(Type.Array(Type.String(), { description: "regex patterns; matching URLs are skipped" })),
	max_chars: Type.Optional(
		Type.Integer({ minimum: 0, description: "truncate each page's markdown to N characters (0 = disabled)" }),
	),
	no_cache: Type.Optional(Type.Boolean({ description: "bypass the page cache" })),
	tag: tagParam("crawled page"),
});

const TagParams = Type.Object({
	operation: Type.Union(
		[Type.Literal("add"), Type.Literal("show"), Type.Literal("list"), Type.Literal("remove")],
		{ description: "one of: add, show, list, remove" },
	),
	tag: Type.Optional(
		Type.String({ description: "the tag to act on; required for every operation except list" }),
	),
	limit: Type.Optional(
		Type.Integer({ minimum: 0, description: "show only: maximum entries (default 50; 0 = all)" }),
	),
	urls: Type.Optional(
		Type.Array(Type.String(), {
			description:
				"for add, the URLs to tag (a URL with no cached page is still indexed; its title and description fill in once fetched); for remove, the URLs to drop (omit to drop the whole tag)",
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
 * Runs `ketch <args>`; if the binary is not on PATH, falls back to the pinned
 * npm wrapper via `npx -y` (unless KETCH_PI_NO_NPX is set).
 */
async function runKetch(
	pi: ExtensionAPI,
	args: string[],
	signal: AbortSignal | undefined,
	timeout: number,
): Promise<RunResult> {
	const direct = await runCommand(pi, "ketch", args, args, signal, timeout);
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
	flag(args, "--tokens", p.tokens);
	flag(args, "--limit", p.limit);
	toggle(args, "--resolve", p.resolve);
	flag(args, "--tag", p.tag);
	return positional(args, p.query);
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
			"Search the web with the operator-configured ketch backend (brave, ddg, searxng, exa, firecrawl, keenable, tavily, tinyfish, parallel, serpbase, degoog, serply, youcom, or the keyless auto chain) and return results (title, url, description). " +
			"Set scrape=true to also fetch each result and include its content as markdown. " +
			"Set multi to query several backends at once and rank-fuse the results, or random to shuffle providers and fall back sequentially on errors." +
			ERR_TAXONOMY,
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
			"Search code across open-source repositories using grep.app, Sourcegraph, or GitHub (default: the configured backend). Returns file/line-level snippets with repo context. " +
			"Set lang to filter by language, repo to search one repository (exact on every backend), and regexp to treat the query as a regular expression (grepapp, sourcegraph only)." +
			ERR_TAXONOMY,
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
			"Search library documentation using Context7. Supports resolving a library name to a Context7 library ID (resolve=true), and fetching docs directly from a known library ID (library=/org/repo, bounded by tokens)." +
			ERR_TAXONOMY,
		promptSnippet: "ketch_docs for a library's own documentation; resolve the name first, then fetch by library ID",
		parameters: DocsParams,
		async execute(_id, p, signal) {
			if (!p.query?.trim()) return validation("query is required");
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
			"Fetch one or more URLs, extract the main content, and convert it to clean markdown (or raw HTML with raw=true). " +
			"Bare domains are auto-probed for /llms.txt first unless no_llms_txt is set. Set max_chars (and optionally trim) on unknown pages to bound the response size. " +
			"Batch scrapes (urls) report per-URL failures inside a successful result; check every entry. " +
			"After configured URL rewrites, only absolute HTTP(S) URLs are fetched; other schemes such as file:// are rejected." +
			ERR_TAXONOMY,
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
			"BFS-crawl a site from a seed URL (same host only) and return extracted markdown for each page. " +
			`Synchronous and bounded: at most max_pages pages (default ${CRAWL_DEFAULT_MAX_PAGES}, cap ${CRAWL_MAX_PAGES}) within a 3-minute budget; the CLI has no page cap, so the crawl runs to completion or the deadline and the first max_pages pages are returned, with stopped set to "max_pages" or "timeout". ` +
			"Large crawls belong to the CLI's background mode (ketch crawl --background), which is not exposed here. Set max_chars to bound each page. " +
			"After configured URL rewrites, only absolute HTTP(S) URLs are fetched; other schemes such as file:// are rejected." +
			ERR_TAXONOMY,
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
			"Bookmark research sources under project or topic labels and revisit them across sessions. " +
			"Use it to keep a working set for a project: tag the docs and write-ups that proved useful (or pass tag to ketch_search/ketch_scrape/ketch_crawl as you fetch), then later call operation=show to get an index of titles, URLs and descriptions instead of searching the web again. " +
			"Operations: add (bookmark URLs, cached or not), show (newest 50 by default; limit=0 for all), list (all tags), remove (drop a tag, or the given URLs from it). " +
			"Makes no network requests. The index is durable and outlives the cached page bodies: entries whose body has expired come back with cached=false and can be re-fetched with ketch_scrape." +
			ERR_TAXONOMY,
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
