"""Draft the broad open set's cases from ketch-bench's gold cases.

    python3 -I internal/docsqa/broad_cases.py ~/Projects/personal/ketch-bench > candidates.json

Reads docs/cases.json and docs/general/cases-dev.json, keeps the cases of the
sites below (answerable and not version-pinned, plus the general set's
negative controls, as not_found cases), and writes them in docsqa's case
format: every required fact with all its alternatives, every quote of an
alternative with the alternative's source page, budget 1800. The library is
the docs root the site was indexed from; `docsqa -verify` then replaces it
with the root the index lists, resolves each quote to its indexed section
and drops what the index cannot grade. See cases/broad.README.md.
"""
import json
import sys
from urllib.parse import urlsplit

# name: (docs root indexed, kind, generator, words a question names it by)
SITES = {
    'duo': ('https://duo.com/docs/', 'saas-product', 'custom', ['duo']),
    'nginx': ('https://nginx.org/en/docs/', 'infra-ops', 'custom xslt', ['nginx']),
    'gha': ('https://docs.github.com/en/actions', 'cloud-ci', 'custom next.js', ['github']),
    'k8s': ('https://kubernetes.io/docs/', 'infra-ops', 'hugo docsy', ['kubernetes', 'k8s', 'kubectl']),
    'postgres': ('https://www.postgresql.org/docs/current/', 'infra-ops', 'docbook', ['postgres', 'pg_hba']),
    'docker': ('https://docs.docker.com/', 'infra-ops', 'hugo', ['docker']),
    'slack': ('https://docs.slack.dev/', 'saas-product', 'docusaurus', ['slack']),
    'okta': ('https://developer.okta.com/docs/', 'saas-product', 'vuepress', ['okta']),
    'stripe': ('https://docs.stripe.com/', 'saas-product', 'custom', ['stripe']),
    'lambda': ('https://docs.aws.amazon.com/lambda/latest/dg/', 'cloud-ci', 'aws docs', ['lambda']),
    'openid': ('https://openid.net/specs/', 'spec', 'xml2rfc', ['openid', 'oidc']),
    'react': ('https://react.dev/', 'library', 'custom next.js', ['react', 'useeffect', 'setstate']),
    'nextjs': ('https://nextjs.org/docs', 'library', 'custom next.js', ['next']),
    'fastapi': ('https://fastapi.tiangolo.com/', 'library', 'mkdocs-material', ['fastapi']),
    'tokio': ('https://tokio.rs/', 'library', 'custom next.js', ['tokio']),
    'aisdk': ('https://ai-sdk.dev/docs', 'library', 'custom next.js', ['ai sdk']),
    'bun': ('https://bun.uptrace.dev/', 'library', 'vuepress', ['bun']),
    'astro': ('https://docs.astro.build/en/', 'library', 'starlight', ['astro']),
    'streamlit': ('https://docs.streamlit.io/', 'library', 'custom next.js', ['streamlit']),
    'instructor': ('https://python.useinstructor.com/', 'library', 'mkdocs-material', ['instructor']),
    'prefect': ('https://www.prefect.io/docs/v3/', 'library', 'mintlify', ['prefect']),
    'pillow': ('https://pillow.readthedocs.io/en/stable/', 'library', 'sphinx', ['pil', 'pillow']),
    'textual': ('https://textual.textualize.io/', 'library', 'mkdocs-material', ['textual']),
    'psycopg': ('https://www.psycopg.org/psycopg3/docs/', 'library', 'sphinx', ['psycopg']),
    'electron': ('https://www.electronjs.org/docs/latest/', 'library', 'docusaurus', ['electron']),
    'pydanticai': ('https://pydantic.dev/docs/ai/', 'library', 'starlight', ['pydantic']),
    'sveltekit': ('https://svelte.dev/docs/kit', 'library', 'custom sveltekit', ['sveltekit']),
    'hono': ('https://hono.dev/docs', 'library', 'vitepress', ['hono']),
    'testcontainers': ('https://golang.testcontainers.org/', 'library', 'mkdocs-material', ['testcontainers']),
    'temporal': ('https://docs.temporal.io/', 'library', 'docusaurus', ['temporal']),
    'vuerouter': ('https://router.vuejs.org/', 'library', 'vitepress', ['vue router']),
    'scrapy': ('https://docs.scrapy.org/en/latest/', 'library', 'sphinx', ['scrapy']),
    'tanstack': ('https://tanstack.com/query/latest', 'library', 'custom tanstack start', ['tanstack']),
    'testinglib': ('https://testing-library.com/docs/', 'library', 'docusaurus', ['testing library']),
    'seaorm': ('https://www.sea-ql.org/SeaORM/', 'library', 'docusaurus', ['seaorm']),
    'docusaurus': ('https://docusaurus.io/docs', 'library', 'docusaurus', ['docusaurus']),
}

# The general set's negative controls: the term each one's notes checked
# and found nowhere on the site.
NEGATIVE_TERMS = {
    'neg-stripe-decline-daily-limit-exceeded': 'daily_limit_exceeded',
    'neg-nginx-proxy-retry-backoff': 'proxy_retry_backoff',
    'neg-k8s-podmemoryautoresize-gate': 'PodMemoryAutoResize',
    'neg-postgres-query-cache-size': 'query_cache_size',
    'neg-gha-step-retry-on-failure': 'retry-on-failure',
    'neg-cloudflare-wrangler-keep-warm': 'keep_warm',
}


def key(u):
    s = urlsplit(u if '://' in u else 'https://' + u)
    host = s.netloc.lower().removeprefix('www.')
    return host + s.path.rstrip('/')


def site_of(c):
    """The site whose root holds the case's gold sources (or, for a
    negative, its project root)."""
    if c.get('negative'):
        urls = c.get('project_roots', [])
    else:
        urls = [a['source'] for f in c['required_facts'] for a in f['alternatives']]
    for name, (root, *_rest) in SITES.items():
        rk = key(root)
        if any(key(u).startswith(rk) or rk.startswith(key(u)) and c.get('negative') for u in urls):
            return name
    return None


def main(bench):
    out, unnamed = [], 0
    for path in ['docs/cases.json', 'docs/general/cases-dev.json']:
        for c in json.load(open(f'{bench}/{path}'))['cases']:
            if c.get('version') or 'project_roots' not in c:
                continue
            if c.get('negative') and c['id'] not in NEGATIVE_TERMS:
                continue
            name = site_of(c)
            if name is None:
                continue
            root, kind, generator, words = SITES[name]
            q = c['query']
            if not any(w in q.lower() for w in words):
                unnamed += 1
            case = {'id': c['id'], 'question': q, 'library': root, 'facts': [], 'not_found': [], 'budget': 1800, 'kind': kind}
            if c.get('negative'):
                case['not_found'] = [NEGATIVE_TERMS[c['id']]]
            for f in c.get('required_facts', []):
                case['facts'].append({'alternatives': [
                    [{'quote': q, 'source': a['source']} for q in a['quotes']] for a in f['alternatives']]})
            out.append(case)
    json.dump(out, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write('\n')
    print(f'{len(out)} cases; {unnamed} questions do not name their library', file=sys.stderr)


if __name__ == '__main__':
    main(sys.argv[1])
