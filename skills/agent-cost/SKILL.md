---
name: agent-cost
description: >-
  Measure what an agent session actually costs in tokens using bin/agents/cost.
  Use before and after changing documentation, skills or context layout, and when
  a session feels unexpectedly expensive.
---

# agent-cost

```bash
bin/agents/cost                        # every project, YAML
bin/agents/cost --repo 2dph            # only sessions whose cwd matches
bin/agents/cost --json | jq .cursor.by_tool
bin/agents/cost --snapshot after-x --repo 2dph   # append a row to docs/CONTEXT-BUDGET.md
```

Reads local session storage from OpenCode and Cursor transcripts. Reports the
always-loaded baseline, cache hit/miss/thrash, and which tools moved the most
bytes.

## How to read it

- **Baseline** is what every single message pays for: `AGENTS.md` plus anything
  eagerly linked from it. Keep it small; it multiplies by message count.
- **Cache thrash** matters more than raw size. Editing a file that sits early in
  the context invalidates the prompt cache for the whole session.
- **by_tool bytes** shows where the real spend is. Usually it is unfiltered
  command output, not documentation.

## Rule

Measure before and after. A claim that something "reduces tokens" without a
before and an after number is an opinion, not a result.
