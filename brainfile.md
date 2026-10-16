---
schema: https://brainfile.md/v2/board.json
title: Ketch
agent:
  instructions:
    - Task files are individual .md files in board/
    - Completed tasks are in logs/
    - Preserve all IDs
    - Make minimal changes
columns:
  - id: todo
    title: To do
  - id: in-progress
    title: In progress
  - id: monitor
    title: Monitor
  - id: wont-do
    title: Won't do
---

# Ketch

ketch: search, scrape, crawl, docs and code research for agents. The benchmark corpus lives in 1broseidon/ketch-bench.

> Note: Completing a task moves it to `logs/` via `brainfile complete`.
