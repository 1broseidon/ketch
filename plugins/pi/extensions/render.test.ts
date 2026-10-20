import assert from "node:assert/strict";
import { test } from "node:test";
import ketch from "./ketch.ts";
import { callSummary, fullDetails, preview, renderers, resultSummary, safeText, type Surface } from "./render.ts";

const result = (data: unknown, warnings?: string[]) => ({ content: [{ type: "text", text: JSON.stringify(data) }], details: { result: data, warnings } });
const summary = (surface: Surface, data: unknown) => resultSummary(surface, result(data)).join("\n");

test("search previews show title, domain, snippet, count; no context dump", () => {
	const rows = Array.from({ length: 12 }, (_, i) => ({ title: `Result ${i}`, url: "https://example.org/a", description: "Useful snippet", content: "FULL_BODY".repeat(1000) }));
	const r = result(rows);
	const before = JSON.stringify(r);
	const text = resultSummary("search", r).join("\n");
	assert.match(text, /12 results/); assert.match(text, /Result 0 · example.org/);
	assert.match(text, /Useful snippet/); assert.match(text, /\+8 more/);
	assert.doesNotMatch(text, /FULL_BODY|Result 4|"content"/);
	assert.ok(text.length < 1500);
	assert.match(fullDetails(r), /FULL_BODY/);
	assert.equal(JSON.stringify(r), before);
});

test("scrapes: singleton, batch, raw HTML and per-page failures", () => {
	assert.match(summary("scrape", { title: "Page", url: "https://example.org/a", markdown: "Readable preview" }), /Page · example.org\n   https:\/\/example.org\/a\n   Readable preview/);
	assert.match(summary("scrape", [{ url: "https://example.org", error: "unavailable" }]), /Error: unavailable\n1 page errors/);
	assert.match(summary("scrape", { url: "https://example.org", raw_html: "<html>raw preview</html>" }), /raw preview/);
});

test("crawl reports errors, page bounds and timeouts from direct details", () => {
	for (const stopped of ["max_pages", "timeout"]) {
		const data = { pages: [{ title: "Home", url: "https://example.org", markdown: "hello" }], stopped, errors: [{ url: "https://example.org/broken", error: "404" }], warnings: ["warning"] };
		const text = resultSummary("crawl", { content: [], details: data }).join("\n");
		assert.match(text, /1 page/); assert.match(text, /1 errors/); assert.match(text, /404/); assert.match(text, /1 warnings/);
		assert.match(text, stopped === "max_pages" ? /page limit reached/ : /Stopped: timeout/);
	}
});

test("tag list, show, add and remove have actionable summaries", () => {
	assert.match(summary("tag", { tags: [{ name: "research", entries: 5, cached: 2 }] }), /research — 5 entries · 2 cached/);
	assert.match(summary("tag", { tag: "research", entries: 10, pages: [{ title: "Source", url: "https://example.org", description: "Notes" }], cache_status: "unavailable" }), /1 result of 10 total · research/);
	assert.match(summary("tag", { tag: "research", tagged: ["url"], not_cached: ["url"] }), /Tagged 1 URLs · research\n1 not cached/);
	assert.match(summary("tag", { tag: "research", removed: 0, missing: ["url"] }), /Removed 0 entries · research\n1 URLs not found/);
	assert.match(summary("tag", { tags: [] }), /0 tags/);
});

test("code and docs include repository/line, snippet and library identity", () => {
	assert.match(summary("code", [{ repo: "owner/repo", path: "main.go", line: 42, snippet: "func main()", url: "https://github.com/owner/repo" }]), /owner\/repo\/main.go:42 · github.com\n   func main\(\)/);
	assert.match(summary("docs", { results: [{ title: "Library", id: "/org/library", description: "Documentation" }] }), /Library\n   \/org\/library\n   Documentation/);
});

test("warnings and federated backend errors are visible without raw JSON", () => {
	const r = result({ results: [], errors: { brave: "rate limited" } }, ["bookmark write failed"]);
	const text = resultSummary("search", r).join("\n");
	assert.match(text, /brave: rate limited/); assert.match(text, /bookmark write failed/);
});

test("unknown, null, malformed and empty responses never crash or dump JSON", () => {
	for (const data of [null, {}, 123, "text", { unexpected: "SECRET_CONTEXT" }, [null, 42, {}, { title: [] }]]) {
		for (const surface of ["search", "code", "docs", "scrape", "crawl", "tag"] as Surface[]) {
			assert.doesNotMatch(summary(surface, data), /SECRET_CONTEXT|\[object Object\]/);
		}
	}
	assert.match(resultSummary("search", { content: [{ type: "text", text: "not JSON" }] }).join("\n"), /unstructured/);
	assert.match(summary("search", []), /0 results/);
});

test("terminal controls stripped, previews bounded, arguments resilient while streaming", () => {
	const hostile = "ok\x1b[31mRED\x1b[0m\x1b]52;c;clipboard\x07\x1bPpayload\x1b\\\r\x00\u202eend";
	assert.equal(safeText(hostile), "okREDend");
	assert.equal(preview("a\n\tb"), "a b");
	assert.equal(preview("a".repeat(1000), 10), "aaaaaaaaa…");
	assert.equal(callSummary("search", undefined), "");
	assert.ok(callSummary("search", { query: hostile.repeat(500), multi: ["a", "b"] }).length <= 260);
	assert.match(callSummary("crawl", { url: "https://example.org", max_pages: 3 }), /max_pages=3/);
	assert.match(callSummary("tag", { operation: "show", tag: "research" }), /show research/);
	assert.doesNotMatch(fullDetails({ content: [{ type: "text", text: hostile }] }), /\x1b|\x07|\u202e/);
});

test("actual Text components render collapsed, expanded, partial and host errors at narrow widths", () => {
	const theme = { fg: (_color: string, s: string) => s, bold: (s: string) => s };
	const renderer = renderers("search");
	const r = result([{ title: "你好 🌐", url: "https://example.org", description: "short", content: "EXPANDED_ONLY" }]);
	for (const width of [12, 40, 100]) {
		const collapsed = renderer.renderResult!(r as never, { expanded: false, isPartial: false }, theme as never, {} as never).render(width).join("\n");
		assert.doesNotMatch(collapsed, /EXPANDED_ONLY/);
		const expanded = renderer.renderResult!(r as never, { expanded: true, isPartial: false }, theme as never, {} as never).render(width).join("");
		assert.match(expanded, /EXPANDED_ONLY/);
	}
	assert.match(renderer.renderResult!(r as never, { expanded: false, isPartial: true }, theme as never, {} as never).render(80).join(""), /Working/);
	const error = { content: [{ type: "text", text: "[validation] query required" }] };
	assert.match(renderer.renderResult!(error as never, { expanded: false, isPartial: false }, theme as never, { isError: true } as never).render(80).join("\n"), /Failed\s+\[validation\]/);
	assert.match(resultSummary("search", { ...error, isError: true }).join("\n"), /Failed/);
});

test("all six registered tools have call/result renderers without changing execute payloads", async () => {
	const tools: any[] = [];
	const raw = JSON.stringify([{ title: "Full", content: "model keeps this" }]);
	ketch({ registerTool: (t: unknown) => tools.push(t), registerCommand() {}, exec: async () => ({ code: 0, stdout: raw, stderr: "", killed: false }) } as never);
	assert.equal(tools.length, 6);
	for (const tool of tools) { assert.equal(typeof tool.renderCall, "function"); assert.equal(typeof tool.renderResult, "function"); }
	const r = await tools[0].execute("test", { query: "test" });
	assert.equal(r.content[0].text, raw);
	assert.deepEqual(r.details.result, JSON.parse(raw));
});
