<script setup>
import { useData } from 'vitepress'
const { theme } = useData()

const features = [
  {
    title: 'Search + Scrape',
    description: 'Web search via Brave, DuckDuckGo, or SearXNG. Scrape any URL to clean markdown. Combine both in one call with --scrape.',
  },
  {
    title: 'Agent-Native Output',
    description: 'YAML frontmatter + markdown body — the same format as cymbal. Agents scan metadata first, read content when needed. --json available as escape hatch.',
  },
  {
    title: 'Operator Configures, Agent Consumes',
    description: 'Set your backend and API keys once in ~/.config/ketch/config.json. Agents call ketch search without knowing the infrastructure.',
  },
  {
    title: 'Page Cache',
    description: 'Scraped pages cached locally with TTL-based expiry. Second fetch of the same URL returns in ~5ms instead of ~200ms. Cache follows platform conventions.',
  },
  {
    title: 'Zero Dependencies',
    description: 'Single Go binary, CGO_ENABLED=0, cross-compiles everywhere. No daemon, no database, no runtime dependencies. Install and run.',
  },
  {
    title: 'Multiple Backends',
    description: 'Brave Search API (free tier), DuckDuckGo HTML scraping, or self-hosted SearXNG. Switch backends with one config change.',
  },
]
</script>

<template>
  <div class="home">
    <!-- Nav -->
    <nav class="home-nav">
      <div class="nav-left">
        <a href="https://chain.sh" class="nav-back">CHAIN.SH</a>
        <span class="nav-sep">/</span>
        <span class="nav-title">KETCH</span>
      </div>
      <a href="https://github.com/1broseidon/ketch" target="_blank" rel="noopener" class="nav-icon">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
          <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/>
        </svg>
      </a>
    </nav>

    <!-- Hero -->
    <main class="hero fade-up" style="animation-delay: 100ms">
      <h1 class="headline">KETCH</h1>
      <div class="tagline-row">
        <div class="accent-line"></div>
        <p class="tagline">Web search and scrape for agents</p>
        <span v-if="theme.version" class="version-tag">{{ theme.version }}</span>
      </div>
      <p class="subtitle">Fast, stateless CLI for web search and page scraping. Search the web, fetch pages, extract clean markdown — all from one binary. Designed to be called by AI agents or directly from your terminal.</p>

      <!-- Install -->
      <div class="install-block">
        <span class="install-dollar">$</span>
        <code>brew install 1broseidon/tap/ketch</code>
      </div>

      <!-- Actions -->
      <div class="actions">
        <a href="/ketch/guide/getting-started" class="action-primary">Get Started</a>
        <a href="https://github.com/1broseidon/ketch" target="_blank" rel="noopener" class="action-secondary">View Source</a>
      </div>
    </main>

    <!-- Features -->
    <section class="features fade-up" style="animation-delay: 200ms">
      <div class="section-header">
        <h2 class="section-title">Features</h2>
        <div class="section-divider"></div>
        <span class="section-count">{{ String(features.length).padStart(2, '0') }} Modules</span>
      </div>
      <div class="feature-grid">
        <div v-for="(feature, i) in features" :key="feature.title" class="feature-card">
          <span class="feature-index">{{ String(i + 1).padStart(3, '0') }}</span>
          <h3 class="feature-title">{{ feature.title }}</h3>
          <p class="feature-desc">{{ feature.description }}</p>
        </div>
      </div>
    </section>

    <!-- Quick Start -->
    <section class="quickstart fade-up" style="animation-delay: 300ms">
      <div class="section-header">
        <h2 class="section-title">Quick Start</h2>
        <div class="section-divider"></div>
      </div>
      <div class="code-block">
        <pre><code><span class="code-comment"># Search the web</span>
ketch search "golang error handling"

<span class="code-comment"># Search and fetch full content from each result</span>
ketch search "golang error handling" --scrape

<span class="code-comment"># Scrape a URL to clean markdown</span>
ketch scrape https://go.dev/doc/effective_go

<span class="code-comment"># Batch scrape (concurrent)</span>
ketch scrape https://example.com https://go.dev</code></pre>
      </div>
    </section>

    <!-- Footer -->
    <footer class="home-footer fade-up" style="animation-delay: 400ms">
      <div class="footer-line">
        <span>&copy; {{ new Date().getFullYear() }} CHAIN.SH / KETCH</span>
        <a href="https://chain.sh" class="footer-back">&larr; CHAIN.SH</a>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.home {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  padding: 2rem;
  max-width: 1200px;
  margin: 0 auto;
  width: 100%;
}

@media (min-width: 768px) {
  .home { padding: 2rem 3rem; }
}

/* Nav */
.home-nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1.5rem 2rem;
  background: rgba(13, 17, 23, 0.8);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--vp-c-divider);
}

@media (min-width: 768px) {
  .home-nav { padding: 1.5rem 3rem; }
}

.nav-left {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.15em;
}

.nav-back {
  color: var(--vp-c-text-3);
  text-decoration: none;
  transition: color 0.2s;
}

.nav-back:hover { color: var(--vp-c-text-1); }

.nav-sep { color: var(--vp-c-text-3); }

.nav-title { color: var(--vp-c-text-1); }

.nav-icon {
  color: var(--vp-c-text-3);
  transition: color 0.2s;
}

.nav-icon:hover { color: var(--vp-c-text-1); }

/* Hero */
.hero {
  flex-grow: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-start;
  margin-top: 8rem;
  margin-bottom: 5rem;
}

.headline {
  font-size: clamp(4rem, 18vw, 12rem);
  line-height: 0.9;
  font-weight: 700;
  color: var(--vp-c-text-1);
  text-transform: uppercase;
  position: relative;
  display: inline-block;
}

.headline::after {
  content: 'KETCH';
  position: absolute;
  left: 4px;
  top: 4px;
  z-index: -1;
  -webkit-text-stroke: 1px var(--vp-c-divider);
  color: transparent;
}

.tagline-row {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  margin-top: 2rem;
}

.accent-line {
  height: 1px;
  width: 6rem;
  background: var(--vp-c-brand-1);
}

.tagline {
  text-transform: uppercase;
  letter-spacing: 0.3em;
  color: var(--vp-c-text-3);
  font-size: 0.875rem;
  font-weight: 500;
}

.version-tag {
  font-family: var(--vp-font-family-mono);
  font-size: 0.6875rem;
  color: var(--vp-c-text-3);
  letter-spacing: 0.05em;
  margin-left: auto;
}

.subtitle {
  color: var(--vp-c-text-3);
  font-size: 0.8125rem;
  line-height: 1.7;
  margin-top: 1.5rem;
  max-width: 36rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.install-block {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 2rem;
  padding: 0.75rem 1.25rem;
  border: 1px solid var(--vp-c-divider);
  font-family: var(--vp-font-family-mono);
  font-size: 0.8125rem;
}

.install-dollar {
  color: var(--vp-c-text-3);
  user-select: none;
}

.install-block code {
  color: var(--vp-c-text-1);
}

.actions {
  display: flex;
  gap: 1rem;
  margin-top: 2rem;
}

.action-primary,
.action-secondary {
  padding: 0.75rem 1.5rem;
  font-size: 0.6875rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  text-decoration: none;
  transition: all 0.2s;
}

.action-primary {
  background: var(--vp-c-divider);
  border: 1px solid #30363d;
  color: var(--vp-c-text-1);
}

.action-primary:hover {
  background: #30363d;
  border-color: var(--vp-c-brand-1);
}

.action-secondary {
  border: 1px solid var(--vp-c-divider);
  color: var(--vp-c-text-2);
  background: transparent;
}

.action-secondary:hover {
  border-color: #30363d;
  color: var(--vp-c-text-1);
}

/* Section header */
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 3rem;
}

.section-title {
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.4em;
  color: var(--vp-c-brand-1);
  flex-shrink: 0;
}

.section-divider {
  height: 1px;
  flex-grow: 1;
  margin: 0 2rem;
  background: var(--vp-c-divider);
}

.section-count {
  font-size: 0.6875rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--vp-c-text-3);
  flex-shrink: 0;
}

/* Feature cards */
.feature-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 2rem;
}

@media (min-width: 768px) {
  .feature-grid { grid-template-columns: 1fr 1fr; }
}

.feature-card {
  border: 1px solid var(--vp-c-divider);
  padding: 1.5rem;
  transition: background 0.2s;
}

.feature-card:hover {
  background: rgba(139, 148, 158, 0.03);
}

.feature-index {
  font-size: 0.6875rem;
  font-weight: 700;
  color: var(--vp-c-brand-1);
  display: block;
  margin-bottom: 1rem;
}

.feature-title {
  font-size: 1rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--vp-c-text-1);
  margin-bottom: 0.5rem;
}

.feature-desc {
  font-size: 0.6875rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  line-height: 1.7;
  color: var(--vp-c-text-3);
}

/* Features section */
.features {
  margin-bottom: 4rem;
}

/* Quick Start */
.quickstart {
  margin-bottom: 4rem;
}

.code-block {
  border: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-alt);
  padding: 1.5rem;
  overflow-x: auto;
}

.code-block pre {
  margin: 0;
}

.code-block code {
  font-family: var(--vp-font-family-mono);
  font-size: 0.8125rem;
  line-height: 1.8;
  color: var(--vp-c-text-1);
}

.code-comment {
  color: var(--vp-c-text-3);
}

/* Footer */
.home-footer {
  margin-top: auto;
}

.footer-line {
  padding-top: 1.5rem;
  border-top: 1px solid var(--vp-c-divider);
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.6875rem;
  text-transform: uppercase;
  letter-spacing: 0.2em;
  color: var(--vp-c-text-3);
}

.footer-back {
  color: var(--vp-c-text-3);
  text-decoration: none;
  transition: color 0.2s;
}

.footer-back:hover {
  color: var(--vp-c-text-1);
}
</style>
