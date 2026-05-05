# Board State and Cache

## Objective

Define the smallest local state model that makes `kanban` fast and agent-ready without turning it into a heavy local database.

## Current State

The current MVP is effectively stateless:

- detect repo
- query `gh`
- render board

That is correct for the first milestone.

## Why Additional State Is Needed

Agentic features need a few local-only values that should not live in GitHub:

- repo slug to `tsctl` repo key mapping
- preferred runner per repository
- preferred dispatch mode per repository
- optional refresh cache metadata
- last successful board snapshot timestamp

## State Location

Preferred path:

- user scope: `~/.config/kanban/config.json`

Optional future path:

- repo scope: `.kanban/config.json`

Decision:

- use user scope first
- add repo scope only when there is a strong need for shared team config

## Config Shape

```json
{
  "repos": {
    "jmh-devel/solarops.us": {
      "repo_key": "solarops.us",
      "repos_file": "/data/src/tacitsoft/infrastructure/tsctl/repos.yaml",
      "preferred_runner": "codex",
      "preferred_mode": "implement"
    }
  },
  "cache": {
    "ttl_seconds": 60
  }
}
```

## Cache Rules

Cache goals:

- avoid hammering `gh` during browser refreshes
- keep data fresh enough for an issue board

Default behavior:

- in-memory cache only
- 60 second TTL
- explicit refresh button bypasses cache

Non-goal for now:

- durable on-disk issue snapshot storage

## Write Ownership

`kanban` may write only:

- its own config file
- its own cache metadata if needed later

It should not mutate git repo files during phase 1 agentification.

## Acceptance Criteria

- local config schema exists
- repo key mapping is modeled there
- refresh semantics are deterministic
- no heavy database or repo mutation is introduced
