// End-to-end checks of the argument encoding against the real CLI parser: the
// extension's exec is wired to a ketch binary built from this checkout, and the
// crawl targets a local HTTP server, so nothing leaves the machine. Uses
// KETCH_BIN when set, otherwise builds with `go build`; skipped when neither is
// available, so `npm test` still runs without a Go toolchain.
import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { after, before, describe, test } from "node:test";
import { fileURLToPath } from "node:url";

import ketch from "./ketch.ts";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const work = mkdtempSync(join(tmpdir(), "ketch-pi-cli-"));

function findBinary(): string | undefined {
	if (process.env.KETCH_BIN) return process.env.KETCH_BIN;
	const out = join(work, "ketch");
	try {
		execFileSync("go", ["version"], { stdio: "ignore" });
	} catch {
		return undefined; // no Go toolchain: the suite skips itself
	}
	// Go is present, so a build failure is a real regression and must fail loudly.
	execFileSync("go", ["build", "-o", out, "."], { cwd: repoRoot, stdio: "inherit", env: { ...process.env, CGO_ENABLED: "0" } });
	return out;
}

const bin = findBinary();

// An isolated home: no operator config, cache or tag index is read or written.
// Inherited KETCH_* settings (cookie file, keys, backends) are dropped first.
const inherited = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("KETCH_")));
const env = {
	...inherited,
	HOME: work,
	KETCH_CONFIG: join(work, "config.json"),
	KETCH_TAGS_PATH: join(work, "tags.db"),
	KETCH_NO_UPDATE_NOTIFIER: "1",
};

interface Tool {
	name: string;
	execute: (
		id: string,
		params: Record<string, unknown>,
		signal: AbortSignal | undefined,
		onUpdate: undefined,
		ctx: unknown,
	) => Promise<{ content: { type: string; text: string }[]; details: unknown; isError?: boolean }>;
}

function realPi() {
	const tools = new Map<string, Tool>();
	const pi = {
		registerTool(t: Tool) {
			tools.set(t.name, t);
		},
		registerCommand() {},
		exec(command: string, args: string[], options?: { signal?: AbortSignal; timeout?: number }) {
			assert.equal(command, "ketch");
			return new Promise((done) => {
				execFile(bin!, args, { env, signal: options?.signal, timeout: options?.timeout, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
					const code = err ? (typeof err.code === "number" ? err.code : 1) : 0;
					done({ stdout, stderr, code, killed: Boolean(err?.killed) });
				});
			});
		},
	};
	ketch(pi as never);
	return (name: string, params: Record<string, unknown>) => tools.get(name)!.execute("cli", params, undefined, undefined, {});
}

// A small same-host site. Every page carries enough prose that it is never
// mistaken for a JS shell, and links to the paths the filters are aimed at.
const paths = ["/", "/keep", "/archive/12", "/archive/12345", "/a,b"];
let hits: string[] = [];
let server: Server;
let origin = "";

function page(path: string): string {
	const links = paths.map((p) => `<li><a href="${p}">${p}</a></li>`).join("");
	const prose = `<p>This is the page at ${path}. It has a paragraph of ordinary text so extraction keeps it.</p>`.repeat(4);
	return `<!doctype html><html><head><title>page ${path}</title></head><body><main><h1>${path}</h1>${prose}<ul>${links}</ul></main></body></html>`;
}

before(async () => {
	server = createServer((req, res) => {
		const path = decodeURIComponent(new URL(req.url ?? "/", "http://x").pathname);
		hits.push(path);
		if (!paths.includes(path)) {
			res.writeHead(404).end();
			return;
		}
		res.writeHead(200, { "content-type": "text/html; charset=utf-8" }).end(page(path));
	});
	await new Promise<void>((ready) => server.listen(0, "127.0.0.1", ready));
	origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

after(() => {
	server?.close();
	rmSync(work, { recursive: true, force: true });
});

async function crawledPaths(run: ReturnType<typeof realPi>, params: Record<string, unknown>): Promise<string[]> {
	const r = await run("ketch_crawl", { url: origin + "/", depth: 2, no_cache: true, ...params });
	assert.equal(r.isError, undefined, r.content[0].text);
	const out = r.details as { pages: { url: string }[] };
	return out.pages.map((p) => decodeURIComponent(new URL(p.url).pathname) || "/").sort();
}

describe("real CLI parsing", { skip: bin ? false : "no ketch binary (set KETCH_BIN or install Go)" }, () => {
	test("a query that looks like a flag is input, not an option", async () => {
		const run = realPi();
		const r = await run("ketch_scrape", { url: "--help" });
		assert.equal(r.isError, true);
		assert.match(r.content[0].text, /^\[validation\] .*"--help"/);
		// A batch entry spelling a flag is validated as a URL and rejected,
		// never applied as --cookie-file.
		hits = [];
		const injected = await run("ketch_scrape", { urls: [origin + "/keep", "--cookie-file=/etc/passwd"] });
		assert.equal(injected.isError, undefined, injected.content[0].text);
		const pages = (injected.details as { result: { url: string }[] }).result;
		assert.deepEqual(pages.map((p) => new URL(p.url).pathname), ["/keep"]);
		assert.match(injected.content[0].text, /warnings:\n.*invalid URL.*"--cookie-file=\/etc\/passwd"/);
		assert.deepEqual(hits, ["/keep"]);
	});

	test("a url naming a local file is a URL, never a file of URLs", async () => {
		const run = realPi();
		const list = join(work, "urls.txt");
		writeFileSync(list, `${origin}/keep\n`);
		hits = [];
		for (const params of [{ url: list }, { urls: [list] }]) {
			const r = await run("ketch_scrape", params);
			assert.equal(r.isError, true, JSON.stringify(params));
			assert.match(r.content[0].text, /^\[validation\] invalid URL/);
		}
		assert.deepEqual(hits, [], "the file's contents were never fetched");
	});

	test("crawl filters with commas and quotes reach the crawler intact", async () => {
		const run = realPi();
		// One regex with a comma: split as CSV it would become `/archive/[0-9]{1`
		// and `4}$`, neither matching, and /archive/12 would be crawled.
		const denied = await crawledPaths(run, { deny: ["/archive/[0-9]{1,4}$"] });
		assert.deepEqual(denied, ["/", "/a,b", "/archive/12345", "/keep"]);

		// A substring filter with a comma: split, `/a` would let /archive/* in.
		const allowed = await crawledPaths(run, { url: origin + "/a,b", allow: ["/a,b"] });
		assert.deepEqual(allowed, ["/a,b"]);

		// A bare quote is a CSV parse error unless the value is quoted.
		const quoted = await crawledPaths(run, { deny: ['never"matches'] });
		assert.ok(quoted.includes("/keep"), quoted.join(" "));
	});
});
