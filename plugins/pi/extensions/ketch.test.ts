// Unit tests for the pi extension with a stubbed pi.exec: no pi install and no
// ketch binary needed. Run with `npm test` from the repo root (Node >= 22.18
// strips the types natively).
import assert from "node:assert/strict";
import { describe, test } from "node:test";

import ketch, { assembleCrawl, crawlArgs, csvField, scrapeArgs, searchArgs, tagArgs } from "./ketch.ts";

interface ExecCall {
	command: string;
	args: string[];
	options: { signal?: AbortSignal; timeout?: number } | undefined;
}

interface ExecResult {
	stdout: string;
	stderr: string;
	code: number;
	killed: boolean;
}

type Responder = ExecResult | ((call: ExecCall) => ExecResult | Promise<ExecResult>);

interface Tool {
	name: string;
	label: string;
	description: string;
	promptSnippet?: string;
	parameters: { properties: Record<string, unknown>; required?: string[] };
	execute: (
		id: string,
		params: Record<string, unknown>,
		signal: AbortSignal | undefined,
		onUpdate: undefined,
		ctx: unknown,
	) => Promise<{ content: { type: string; text: string }[]; details: unknown; isError?: boolean }>;
}

interface Command {
	description: string;
	handler: (args: string, ctx: { ui: { notify: (m: string, t?: string) => void } }) => Promise<void>;
}

const ok = (stdout: string, stderr = ""): ExecResult => ({ stdout, stderr, code: 0, killed: false });
const exit = (code: number, stderr: string, stdout = ""): ExecResult => ({ stdout, stderr, code, killed: false });

/** A fake ExtensionAPI capturing registrations and exec calls. */
function fakePi(respond: Responder = ok("[]")) {
	const tools = new Map<string, Tool>();
	const commands = new Map<string, Command>();
	const calls: ExecCall[] = [];
	const pi = {
		registerTool(t: Tool) {
			tools.set(t.name, t);
		},
		registerCommand(name: string, c: Command) {
			commands.set(name, c);
		},
		async exec(command: string, args: string[], options?: ExecCall["options"]): Promise<ExecResult> {
			const call = { command, args, options };
			calls.push(call);
			return typeof respond === "function" ? respond(call) : respond;
		},
	};
	// The extension only touches registerTool/registerCommand/exec; the rest of
	// ExtensionAPI is irrelevant here.
	ketch(pi as never);
	return { pi, tools, commands, calls };
}

async function run(tools: Map<string, Tool>, name: string, params: Record<string, unknown>, signal?: AbortSignal) {
	const tool = tools.get(name);
	assert.ok(tool, `tool ${name} registered`);
	return tool.execute("call-1", params, signal, undefined, {});
}

describe("registration", () => {
	test("registers the six research tools and the doctor command without running anything", () => {
		const { tools, commands, calls } = fakePi();
		assert.deepEqual(
			[...tools.keys()].sort(),
			["ketch_code", "ketch_crawl", "ketch_docs", "ketch_scrape", "ketch_search", "ketch_tag"],
		);
		assert.ok(commands.has("ketch-doctor"));
		assert.equal(calls.length, 0, "no exec at load time");
		for (const t of tools.values()) {
			assert.ok(t.label && t.description && t.promptSnippet, `${t.name} has label/description/promptSnippet`);
			assert.match(t.description, /\[validation\].*\[not_found\].*\[upstream\].*\[precondition\].*\[cancelled\]/);
			assert.equal(typeof t.parameters.properties, "object");
		}
	});

	test("config-level settings are not tool params", () => {
		const { tools } = fakePi();
		for (const t of tools.values()) {
			for (const key of Object.keys(t.parameters.properties)) {
				assert.doesNotMatch(key, /api_key|token$|cache_ttl|^browser$|cookie|user_agent/, `${t.name}.${key}`);
			}
		}
	});
});

describe("argument construction", () => {
	test("search: --json always, flags only when set", async () => {
		const { tools, calls } = fakePi();
		await run(tools, "ketch_search", { query: "go iter.Seq" });
		assert.equal(calls[0].command, "ketch");
		assert.deepEqual(calls[0].args, ["search", "--json", "--", "go iter.Seq"]);

		await run(tools, "ketch_search", {
			query: "q",
			backend: "brave",
			limit: 3,
			searxng_url: "http://sx:8081",
			scrape: true,
			trim: true,
			max_chars: 4000,
			tag: "proj",
		});
		assert.deepEqual(calls[1].args, [
			"search", "--json",
			"--backend=brave",
			"--limit=3",
			"--searxng-url=http://sx:8081",
			"--scrape", "--trim",
			"--max-chars=4000",
			"--tag=proj",
			"--", "q",
		]);
	});

	test("search: multi uses the = form the CLI requires", () => {
		assert.deepEqual(searchArgs({ query: "q", multi: ["brave", "ddg"] }), ["search", "--json", "--multi=brave,ddg", "--", "q"]);
		assert.deepEqual(searchArgs({ query: "q", multi: ["all"] }), ["search", "--json", "--multi=all", "--", "q"]);
		assert.deepEqual(searchArgs({ query: "q", random: ["all"] }), ["search", "--json", "--random=all", "--", "q"]);
	});

	test("search: false booleans and empty strings add nothing", () => {
		assert.deepEqual(searchArgs({ query: "q", scrape: false, trim: false, backend: "", tag: "" }), ["search", "--json", "--", "q"]);
	});

	test("inputs that look like flags stay positional, after --", () => {
		assert.deepEqual(searchArgs({ query: "--help" }), ["search", "--json", "--", "--help"]);
		assert.deepEqual(crawlArgs({ url: "status" }), ["crawl", "--json", "--", "status"]);
		assert.deepEqual(tagArgs({ operation: "add", tag: "-p", urls: ["--cookie-file=x"] }), ["tag", "add", "--json", "--", "-p", "--cookie-file=x"]);
		// A value bound with = can't be read as a flag of its own.
		assert.deepEqual(searchArgs({ query: "q", tag: "--json" }), ["search", "--json", "--tag=--json", "--", "q"]);
	});

	test("search: mutually exclusive selectors are rejected locally", async () => {
		const { tools, calls } = fakePi();
		const r = await run(tools, "ketch_search", { query: "q", backend: "brave", multi: ["all"] });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[validation\]/);
		assert.equal(calls.length, 0);
	});

	test("code: regexp maps to the CLI's --regex", async () => {
		const { tools, calls } = fakePi();
		await run(tools, "ketch_code", { query: "NewFromConfig", lang: "go", repo: "1broseidon/ketch", regexp: true, backend: "sourcegraph", limit: 2, tag: "t" });
		assert.deepEqual(calls[0].args, [
			"code", "--json",
			"--backend=sourcegraph",
			"--lang=go",
			"--repo=1broseidon/ketch",
			"--regex",
			"--limit=2",
			"--tag=t",
			"--", "NewFromConfig",
		]);
		await run(tools, "ketch_code", { query: "x" });
		assert.deepEqual(calls[1].args, ["code", "--json", "--", "x"]);
	});

	test("docs: library/tokens and resolve", async () => {
		const { tools, calls } = fakePi();
		await run(tools, "ketch_docs", { query: "routing", library: "/vercel/next.js", tokens: 2000 });
		assert.deepEqual(calls[0].args, ["docs", "--json", "--library=/vercel/next.js", "--tokens=2000", "--", "routing"]);
		await run(tools, "ketch_docs", { query: "next.js", resolve: true });
		assert.deepEqual(calls[1].args, ["docs", "--json", "--resolve", "--", "next.js"]);

		const r = await run(tools, "ketch_docs", { query: "next.js", resolve: true, tag: "t" });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[validation\]/);
		assert.equal(calls.length, 2);
	});

	test("scrape: single url, then every option", async () => {
		const { tools, calls } = fakePi();
		await run(tools, "ketch_scrape", { url: "https://example.com" });
		assert.deepEqual(calls[0].args, ["scrape", "--json", "--", '["https://example.com"]']);

		await run(tools, "ketch_scrape", {
			url: "https://example.com/a",
			selector: "main",
			force_browser: true,
			no_llms_txt: true,
			trim: true,
			max_chars: 6000,
			no_cache: true,
			concurrency: 2,
			tag: "docs",
		});
		assert.deepEqual(calls[1].args, [
			"scrape", "--json",
			"--select=main",
			"--force-browser", "--no-llms-txt", "--trim",
			"--max-chars=6000",
			"--no-cache",
			"--concurrency=2",
			"--tag=docs",
			"--", '["https://example.com/a"]',
		]);
	});

	test("scrape: urls travel as one JSON-array argument, never as file paths", async () => {
		const { tools, calls } = fakePi();
		await run(tools, "ketch_scrape", { urls: ["https://a.test/1", "https://a.test/2", " https://a.test/3 "] });
		assert.deepEqual(calls[0].args, ["scrape", "--json", "--", '["https://a.test/1","https://a.test/2","https://a.test/3"]']);
		// A lone path would be read by the CLI as a file of URLs; inside the
		// array it is one URL and fails URL validation.
		await run(tools, "ketch_scrape", { url: "./urls.txt" });
		assert.deepEqual(calls[1].args, ["scrape", "--json", "--", '["./urls.txt"]']);
		await run(tools, "ketch_scrape", { urls: ["./urls.txt"] });
		assert.deepEqual(calls[2].args, calls[1].args);
		assert.deepEqual(scrapeArgs({ raw: true }, ["u"]), ["scrape", "--json", "--raw", "--", '["u"]']);
	});

	test("scrape: exactly one of url or urls", async () => {
		const { tools, calls } = fakePi();
		for (const params of [{}, { urls: [] }, { url: "https://a.test", urls: ["https://b.test"] }, { url: "u", raw: true, trim: true }]) {
			const r = await run(tools, "ketch_scrape", params);
			assert.equal(r.isError, true, JSON.stringify(params));
			assert.match(r.content[0].text, /^\[validation\]/);
		}
		assert.equal(calls.length, 0);
	});

	test("crawl: repeated --allow/--deny, never --background", () => {
		assert.deepEqual(crawlArgs({ url: "https://d.test" }), ["crawl", "--json", "--", "https://d.test"]);
		const args = crawlArgs({
			url: "https://d.test",
			depth: 2,
			sitemap: true,
			allow: ["/docs", "/guide"],
			deny: ["\\.pdf$"],
			no_cache: true,
			tag: "d",
			max_pages: 5,
			max_chars: 100,
		});
		assert.deepEqual(args, [
			"crawl", "--json",
			"--depth=2",
			"--sitemap",
			'--allow="/docs"', '--allow="/guide"',
			'--deny="\\.pdf$"',
			"--no-cache",
			"--tag=d",
			"--", "https://d.test",
		]);
		assert.ok(!args.includes("--background"));
		assert.ok(!args.some((a) => a.includes("max")), "max_pages/max_chars are applied client-side");
	});

	test("crawl: filters are CSV-quoted so commas and quotes survive the StringSlice parse", () => {
		const args = crawlArgs({ url: "https://d.test", allow: ["/a,b", ""], deny: ["/archive/[0-9]{1,4}/", 'say "hi"', "-x"] });
		assert.deepEqual(args.slice(2, -2), [
			'--allow="/a,b"',
			'--deny="/archive/[0-9]{1,4}/"',
			'--deny="say ""hi"""',
			'--deny="-x"',
		]);
		assert.equal(csvField('a"b,c'), '"a""b,c"');
	});

	test("tag: one CLI verb per operation", () => {
		assert.deepEqual(tagArgs({ operation: "list" }), ["tag", "list", "--json"]);
		assert.deepEqual(tagArgs({ operation: "show", tag: "p" }), ["tag", "show", "--json", "--", "p"]);
		assert.deepEqual(tagArgs({ operation: "show", tag: "p", limit: 0 }), ["tag", "show", "--json", "--limit=0", "--", "p"]);
		assert.deepEqual(tagArgs({ operation: "add", tag: "p", urls: ["https://a", "https://b"] }), ["tag", "add", "--json", "--", "p", "https://a", "https://b"]);
		assert.deepEqual(tagArgs({ operation: "remove", tag: "p" }), ["tag", "remove", "--json", "--", "p"]);
		assert.deepEqual(tagArgs({ operation: "remove", tag: "p", urls: ["https://a"] }), ["tag", "remove", "--json", "--", "p", "https://a"]);
	});

	test("tag: local validation mirrors the MCP tool", async () => {
		const { tools, calls } = fakePi();
		for (const params of [
			{ operation: "show" },
			{ operation: "add", tag: "p" },
			{ operation: "list", limit: 5 },
		]) {
			const r = await run(tools, "ketch_tag", params);
			assert.equal(r.isError, true, JSON.stringify(params));
			assert.match(r.content[0].text, /^\[validation\]/);
		}
		assert.equal(calls.length, 0);
	});
});

describe("results", () => {
	test("stdout is the content, parsed JSON lands in details", async () => {
		const body = '[{"title":"T","url":"https://t","description":"d"}]\n';
		const { tools } = fakePi(ok(body));
		const r = await run(tools, "ketch_search", { query: "q" });
		assert.equal(r.isError, undefined);
		assert.equal(r.content[0].text, body.trim());
		assert.deepEqual((r.details as { result: unknown }).result, JSON.parse(body));
	});

	test("stderr on success is surfaced as warnings, not an error", async () => {
		const warn = '{"warning":{"code":"tag_write_failed","tag":"p","url":"https://t","message":"locked"}}';
		const { tools } = fakePi(ok("[]\n", warn + "\n"));
		const r = await run(tools, "ketch_search", { query: "q", tag: "p" });
		assert.equal(r.isError, undefined);
		assert.match(r.content[0].text, /warnings:\n\{"warning"/);
		assert.deepEqual((r.details as { warnings: string[] }).warnings, [warn]);
	});

	test("signal and a per-tool timeout are forwarded to pi.exec", async () => {
		const { tools, calls } = fakePi();
		const ac = new AbortController();
		await run(tools, "ketch_code", { query: "q" }, ac.signal);
		assert.equal(calls[0].options?.signal, ac.signal);
		assert.equal(typeof calls[0].options?.timeout, "number");
		assert.ok((calls[0].options?.timeout ?? 0) > 0);
	});
});

describe("error taxonomy", () => {
	const cases: [number, string][] = [
		[2, "[validation]"],
		[3, "[not_found]"],
		[4, "[upstream]"],
		[5, "[precondition]"],
		[6, "[cancelled]"],
		[1, "[upstream]"],
		[42, "[upstream]"],
	];
	for (const [code, prefix] of cases) {
		test(`exit ${code} → ${prefix} with stderr`, async () => {
			const { tools } = fakePi(exit(code, `Error: something about exit ${code}\n`));
			const r = await run(tools, "ketch_docs", { query: "q" });
			assert.equal(r.isError, true);
			assert.equal(r.content[0].text, `${prefix} something about exit ${code}`, "cobra's Error: prefix is replaced");
		});
	}

	test("binary missing (pi.exec swallows ENOENT: code 1, silent) → [precondition] install hint", async () => {
		const { tools } = fakePi(exit(1, ""));
		const r = await run(tools, "ketch_search", { query: "q" });
		assert.equal(r.isError, true);
		assert.equal(r.content[0].text, "[precondition] ketch not on PATH: brew install ketch, npm i -g ketch-cli, or go install github.com/1broseidon/ketch@latest");
	});

	test("binary missing (pi.exec rejects with ENOENT) → [precondition]", async () => {
		const { tools } = fakePi(() => {
			const err = new Error("spawn ketch ENOENT") as NodeJS.ErrnoException;
			err.code = "ENOENT";
			throw err;
		});
		const r = await run(tools, "ketch_scrape", { url: "https://a.test" });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[precondition\] ketch not on PATH/);
	});

	test("ketch missing → falls back to the pinned npx wrapper with the same args", async () => {
		const { tools, calls } = fakePi((c) => (c.command === "ketch" ? exit(1, "") : ok('{"results":[]}')));
		const r = await run(tools, "ketch_search", { query: "q" });
		assert.equal(r.isError, undefined);
		assert.equal(calls[0].command, "ketch");
		assert.equal(calls[1].command, "npx");
		assert.deepEqual(calls[1].args.slice(0, 2), ["-y", "ketch-cli@0.20.0"]);
		assert.deepEqual(calls[1].args.slice(2), calls[0].args);
		assert.ok((calls[1].options?.timeout ?? 0) > (calls[0].options?.timeout ?? 0), "first-run download allowance");
	});

	test("npx fallback drops npm's update notices from warnings", async () => {
		const { tools } = fakePi((c) => (c.command === "ketch" ? exit(1, "") : ok('{"tags":[]}', "npm notice New major version\nnpm notice To update run\n")));
		const r = await run(tools, "ketch_tag", { operation: "list" });
		assert.equal(r.isError, undefined);
		assert.equal((r.details as any).warnings, undefined);
	});

	test("ketch present → npx is never used", async () => {
		const { tools, calls } = fakePi(ok("[]"));
		await run(tools, "ketch_search", { query: "q" });
		assert.deepEqual(calls.map((c) => c.command), ["ketch"]);
	});

	test("KETCH_PI_NO_NPX opts out of the fallback", async () => {
		process.env.KETCH_PI_NO_NPX = "1";
		try {
			const { tools, calls } = fakePi(exit(1, ""));
			const r = await run(tools, "ketch_search", { query: "q" });
			assert.match(r.content[0].text, /^\[precondition\] ketch not on PATH/);
			assert.deepEqual(calls.map((c) => c.command), ["ketch"]);
		} finally {
			delete process.env.KETCH_PI_NO_NPX;
		}
	});

	test("npx run errors keep the taxonomy (exit 2 → validation)", async () => {
		const { tools } = fakePi((c) => (c.command === "ketch" ? exit(1, "") : exit(2, "Error: bad input")));
		const r = await run(tools, "ketch_search", { query: "q" });
		assert.equal(r.content[0].text, "[validation] bad input");
	});

	test("killed by abort → [cancelled]", async () => {
		const ac = new AbortController();
		const { tools } = fakePi(() => {
			ac.abort();
			return { stdout: "", stderr: "", code: 130, killed: true };
		});
		const r = await run(tools, "ketch_search", { query: "q" }, ac.signal);
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[cancelled\] ketch search: the call was cancelled/);
	});

	test("killed by timeout → [cancelled] naming the budget", async () => {
		const { tools } = fakePi({ stdout: "", stderr: "", code: 143, killed: true });
		const r = await run(tools, "ketch_scrape", { url: "https://slow.test" });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[cancelled\] ketch scrape: the call exceeded its \d+s budget/);
	});
});

describe("crawl assembly", () => {
	const line = (i: number, body = `body ${i}`) =>
		JSON.stringify({ url: `https://d.test/${i}`, title: `P${i}`, words: 2, status: "new", source: "http", body });

	test("reshapes NDJSON to pages, caps max_pages, truncates max_chars", () => {
		const stdout = [line(1), line(2), line(3, "x".repeat(50)), ""].join("\n");
		const out = assembleCrawl(stdout, "", 2, 10, false);
		assert.equal(out.pages.length, 2);
		assert.equal(out.stopped, "max_pages");
		assert.deepEqual(out.pages[0], { url: "https://d.test/1", title: "P1", markdown: "body 1" });

		const all = assembleCrawl(stdout, "", 10, 10, false);
		assert.equal(all.pages.length, 3);
		assert.equal(all.stopped, undefined);
		assert.equal(all.pages[2].markdown, "x".repeat(10) + "\n[truncated]");
	});

	test("per-URL warnings become errors; the stats block does not", () => {
		const stderr = "warn: https://d.test/bad: 503 Service Unavailable\n\n---\nseed: https://d.test\npages: 1\nerrors: 1\n---\n";
		const out = assembleCrawl(line(1), stderr, 30, 0, false);
		assert.deepEqual(out.errors, [{ url: "https://d.test/bad", error: "503 Service Unavailable" }]);
		assert.equal(out.warnings, undefined);
	});

	test("a deadline kill with streamed pages is a bounded result, a cut line is dropped", async () => {
		const stdout = line(1) + "\n" + line(2).slice(0, 20);
		const { tools } = fakePi({ stdout, stderr: "", code: 143, killed: true });
		const r = await run(tools, "ketch_crawl", { url: "https://d.test", max_pages: 500 });
		assert.equal(r.isError, undefined);
		const out = r.details as ReturnType<typeof assembleCrawl>;
		assert.equal(out.pages.length, 1);
		assert.equal(out.stopped, "timeout");
	});

	test("a deadline kill with nothing streamed, or a client abort, is [cancelled]", async () => {
		const empty = fakePi({ stdout: "", stderr: "", code: 143, killed: true });
		const r1 = await run(empty.tools, "ketch_crawl", { url: "https://d.test" });
		assert.equal(r1.isError, true);
		assert.match(r1.content[0].text, /^\[cancelled\]/);

		const ac = new AbortController();
		const aborted = fakePi(() => {
			ac.abort();
			return { stdout: line(1), stderr: "", code: 130, killed: true };
		});
		const r2 = await run(aborted.tools, "ketch_crawl", { url: "https://d.test" }, ac.signal);
		assert.equal(r2.isError, true);
		assert.match(r2.content[0].text, /^\[cancelled\]/);
	});

	test("crawl errors keep the taxonomy", async () => {
		const { tools } = fakePi(exit(2, "Error: invalid URL scheme\n"));
		const r = await run(tools, "ketch_crawl", { url: "file:///etc/passwd" });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[validation\] invalid URL scheme/);
	});
});

describe("/ketch-doctor", () => {
	test("healthy → info with the report; exit 5 → warning with report and error", async () => {
		const notes: [string, string | undefined][] = [];
		const ctx = { ui: { notify: (m: string, t?: string) => notes.push([m, t]) } };

		const healthy = fakePi(ok("search: ok\n"));
		await healthy.commands.get("ketch-doctor")!.handler("", ctx);
		assert.deepEqual(healthy.calls[0].args, ["doctor"]);
		assert.deepEqual(notes[0], ["search: ok", "info"]);

		const broken = fakePi(exit(5, "doctor: 1 configured surface broken\n", "search: brave no_key\n"));
		await broken.commands.get("ketch-doctor")!.handler("", ctx);
		assert.equal(notes[1][1], "warning");
		assert.match(notes[1][0], /^search: brave no_key\n\n\[precondition\] doctor: 1 configured surface broken$/);

		const missing = fakePi(exit(1, ""));
		await missing.commands.get("ketch-doctor")!.handler("", ctx);
		assert.deepEqual(notes[2], ["[precondition] ketch not on PATH: brew install ketch, npm i -g ketch-cli, or go install github.com/1broseidon/ketch@latest", "error"]);
	});
});
