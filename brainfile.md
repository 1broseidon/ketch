---
schema: https://brainfile.md/v2/board.json
title: My Project
agent:
  instructions:
    - Task files are individual .md files in board/
    - Completed tasks are in logs/
    - Preserve all IDs
    - Make minimal changes
columns:
  - id: todo
    title: To Do
  - id: in-progress
    title: In Progress
---

# My Project

Add your project description here.

> Note: Completing a task moves it to `logs/` via `brainfile complete`.
