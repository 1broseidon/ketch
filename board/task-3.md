---
id: task-3
title: Guidance in skills/ketch/SKILL.md and MANUAL.md will drift apart
column: todo
position: 1
priority: low
tags:
  - improvement
  - bro-77
createdAt: "2026-10-11T21:01:30.220Z"
description: |-
  `skills/ketch/SKILL.md` and `site/.vitepress/theme/Home.vue` independently cover the same ground — token budgets, exit codes, backend selection, when to reach for which command. Both were edited this cycle when the site was consolidated into a single-page manual; neither references the other, so they will diverge silently and nothing will catch it.

  Overlap check (2026-09-18):

  | Topic | SKILL.md | Home.vue |
  | -- | -- | -- |
  | budget | 10 | 5 |
  | exit code | 3 | 4 |
  | auto | 5 | 11 |
  | pitfall | 0 | 3 |

  **No action needed yet** — this is a note so the next person editing either one knows the other exists. Filed because the duplication was created deliberately (the audiences differ: an agent reading a skill vs. a human reading a manual) and is easy to forget.

  Options if it becomes a problem:

  * Generate both from one source
  * Have SKILL.md link to [ketch.run](<http://ketch.run>) for the reference tables and keep only agent-specific routing advice
  * Accept the duplication and add a note to each pointing at the other

  Worth revisiting the next time either file needs a substantive edit.

  Migrated from Linear BRO-77: https://linear.app/1broseidon/issue/BRO-77/guidance-in-skillsketchskillmd-and-the-site-manual-will-drift-apart (2026-10-11).

  ## Status (2026-10-11 review)

  Stale as written: site/.vitepress/theme/Home.vue is gone; ketch.run builds from MANUAL.md. The drift risk is now between skills/ketch/SKILL.md (+ references/surfaces.md) and MANUAL.md; MANUAL links to the skill but not back. Retitled from "Guidance in skills/ketch/SKILL.md and the site manual will drift apart".
updatedAt: "2026-10-11T21:03:26.426Z"
---

## Description
`skills/ketch/SKILL.md` and `site/.vitepress/theme/Home.vue` independently cover the same ground — token budgets, exit codes, backend selection, when to reach for which command. Both were edited this cycle when the site was consolidated into a single-page manual; neither references the other, so they will diverge silently and nothing will catch it.

Overlap check (2026-09-18):

| Topic | SKILL.md | Home.vue |
| -- | -- | -- |
| budget | 10 | 5 |
| exit code | 3 | 4 |
| auto | 5 | 11 |
| pitfall | 0 | 3 |

**No action needed yet** — this is a note so the next person editing either one knows the other exists. Filed because the duplication was created deliberately (the audiences differ: an agent reading a skill vs. a human reading a manual) and is easy to forget.

Options if it becomes a problem:

* Generate both from one source
* Have SKILL.md link to [ketch.run](<http://ketch.run>) for the reference tables and keep only agent-specific routing advice
* Accept the duplication and add a note to each pointing at the other

Worth revisiting the next time either file needs a substantive edit.

Migrated from Linear BRO-77: https://linear.app/1broseidon/issue/BRO-77/guidance-in-skillsketchskillmd-and-the-site-manual-will-drift-apart (2026-10-11).

## Status (2026-10-11 review)

Stale as written: site/.vitepress/theme/Home.vue is gone; ketch.run builds from MANUAL.md. The drift risk is now between skills/ketch/SKILL.md (+ references/surfaces.md) and MANUAL.md; MANUAL links to the skill but not back. Retitled from "Guidance in skills/ketch/SKILL.md and the site manual will drift apart".
