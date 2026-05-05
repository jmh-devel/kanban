# tsctl Integration

## Objective

Define the cleanest way for `kanban` to launch and observe `tsctl` work without collapsing both tools into one codebase.

## Rationale

`tsctl` already owns multi-repo dispatch, repo catalogs, issue-oriented execution, and k8s-backed agent jobs.

`kanban` should become a front-end and workflow accelerator for that system, not a replacement for it.

## Integration Boundary

`kanban` owns:

- board UX
- current-repo discovery
- repo slug and local-path context
- per-card action affordances
- local operator preferences

`tsctl` owns:

- repo key registry
- agent runner inventory
- dispatch semantics
- job execution
- remote logs and lifecycle

## Required Local Mapping

`tsctl` dispatches by `repo_key`, but `kanban` naturally starts from a local path and GitHub slug.

So `kanban` needs a local mapping layer:

```json
{
  "repos": {
    "jmh-devel/solarops.us": {
      "repo_key": "solarops.us",
      "repos_file": "/data/src/tacitsoft/infrastructure/tsctl/repos.yaml"
    }
  }
}
```

This should live in local kanban config, not in GitHub.

## Dispatch Contract

Preferred command shape:

```bash
tsctl agent dispatch <repo_key> \
  --repos-file <path> \
  --runner <runner> \
  --issue <issue_number> \
  --mode <read-only|implement>
```

If the card has no GitHub issue number, dispatch is blocked until a backing issue exists.

## Phase 1 UX

From a card, the operator can:

- choose runner
- choose mode
- confirm repo key mapping
- dispatch the issue to `tsctl`

Return payload should capture:

- executed command
- repo key
- issue number
- runner
- mode
- job identifier if available

## API Shape

Suggested kanban server endpoint:

```http
POST /api/agents/delegate
```

Suggested request body:

```json
{
  "repo_slug": "jmh-devel/solarops.us",
  "issue_number": 287,
  "runner": "codex",
  "mode": "implement",
  "repo_key": "solarops.us",
  "repos_file": "/data/src/tacitsoft/infrastructure/tsctl/repos.yaml"
}
```

## Logging and Status

Phase 1 only needs synchronous dispatch result capture.

Phase 2 may add:

- polling or streaming job status from `tsctl`
- surfacing recent dispatches in the web UI
- quick links into `tsctl` logs or agent runs

## Error Rules

Block dispatch when:

- current repo has no GitHub issue backing the card
- `repo_key` is unknown
- `tsctl` is not installed
- `repos_file` is missing

Do not silently guess the repo key when multiple matches exist.

## Acceptance Criteria

- repo slug to repo key mapping is supported by local config
- a delegate request can be turned into a deterministic `tsctl` command
- dispatch failures are explicit and actionable
- `kanban` does not take ownership of agent execution logic itself
