# Agent Assignment and Dispatch

## Objective

Define the full flow for assigning a GitHub issue to an agent, dispatching work,
monitoring status on the board, and closing the loop when the agent finishes.

## Principles

- The board is the operator surface; agents are workers, not board owners.
- No agent runtime is embedded in kanban. Agents are launched externally.
- Any runner that produces a deterministic shell command is a valid backend.
- Issue number is the durable unit of work identity. No worktree management in phase 1.

## Lanes and Agent State

Agent assignment lives alongside lane state:

| Agent State     | Lane Effect                                  |
|-----------------|----------------------------------------------|
| Dispatched      | Card moves to In Progress automatically       |
| Agent completed | Card moves to Review automatically (optional)|
| Agent failed    | Card stays In Progress; error shown on card  |

Automatic lane moves on agent events are controlled by local config:

```json
{
  "agent": {
    "auto_move_on_dispatch": true,
    "auto_move_on_complete": false
  }
}
```

`auto_move_on_complete` defaults false because completion should be human-verified
before moving to Review.

## Dispatch Flow

### 1. Operator selects a card and presses `d`

The runner picker modal opens:

```
Dispatch #318 — Refactor admin dashboard into grouped lifecycle Kanban

  Runner      ○ codex  ● claude  ○ tsctl  ○ manual
  Mode        ● implement  ○ plan  ○ review  ○ audit
  Repo key    solarops.us    (from ~/.config/kanban/config.json)
  Repos file  /data/src/tacitsoft/infrastructure/tsctl/repos.yaml

  Preview:
  > tsctl agent dispatch solarops.us \
      --runner claude --issue 318 --mode implement

  [Enter] dispatch   [e] edit command   [Esc] cancel
```

### 2. Operator confirms

On confirm:
1. The constructed command is executed as a subprocess.
2. Card moves to In Progress (if `auto_move_on_dispatch` is true).
3. A dispatch record is written to local state:
   ```json
   {
     "issue": 318,
     "runner": "claude",
     "mode": "implement",
     "dispatched_at": "2026-05-05T17:14:00Z",
     "command": "tsctl agent dispatch solarops.us --runner claude --issue 318 --mode implement",
     "job_id": "abc123"
   }
   ```
4. Card gains an agent status line in the TUI: `● claude · dispatched`.

### 3. Manual runner (no execution)

When runner is `manual`, the picker shows:
- the full command to copy
- a `[c]` key to copy it to clipboard
- no subprocess is launched
- card is NOT moved (operator decides)

This mode is useful when the operator wants to paste into a remote shell or
another tool.

## Runner Registry

Runners are declared in local config at `~/.config/kanban/config.json`:

```json
{
  "runners": {
    "codex": {
      "kind": "local_cli",
      "command": "codex",
      "capabilities": {
        "supports_planning": true,
        "supports_implement": true,
        "supports_review": false,
        "supports_streaming_logs": false,
        "supports_cancel": false
      }
    },
    "claude": {
      "kind": "local_cli",
      "command": "claude",
      "capabilities": {
        "supports_planning": true,
        "supports_implement": true,
        "supports_review": true,
        "supports_streaming_logs": false,
        "supports_cancel": false
      }
    },
    "tsctl": {
      "kind": "tsctl_dispatch",
      "capabilities": {
        "supports_planning": true,
        "supports_implement": true,
        "supports_review": true,
        "supports_streaming_logs": false,
        "supports_cancel": true,
        "requires_repo_key": true
      }
    }
  }
}
```

If no runners are configured, the picker shows only `manual`.

## Repo Key Mapping

`tsctl` requires a repo key that maps GitHub slug to a tsctl repos file entry.
This mapping lives in local config under the repo slug:

```json
{
  "repos": {
    "jmh-devel/solarops.us": {
      "repo_key": "solarops.us",
      "repos_file": "/data/src/tacitsoft/infrastructure/tsctl/repos.yaml",
      "preferred_runner": "claude",
      "preferred_mode": "implement"
    }
  }
}
```

When a card is selected:
- kanban looks up the current repo's slug in this map
- pre-populates `repo_key` and `repos_file` in the picker
- if the slug is not mapped and the runner requires a repo key, the picker shows
  an error and blocks dispatch until the user runs `kanban config set-repo-key`

## Local Dispatch State

After dispatch, records are written to:
`~/.config/kanban/dispatches.json`

```json
[
  {
    "repo": "jmh-devel/solarops.us",
    "issue": 318,
    "runner": "claude",
    "mode": "implement",
    "dispatched_at": "2026-05-05T17:14:00Z",
    "command": "tsctl agent dispatch solarops.us --runner claude --issue 318 --mode implement",
    "status": "dispatched"
  }
]
```

Status values: `dispatched`, `completed`, `failed`, `cancelled`.

This file is used to:
- show the agent status line on each card
- prevent duplicate dispatches (warn if issue already has an active dispatch)
- populate a dispatch history view

## Card Status Line

When an issue has an active dispatch record, the card renders an extra line:

```
● claude · implement · 14m ago
```

Color coding:
- `●` green = dispatched / running
- `●` yellow = review requested
- `●` red = failed
- `○` dim = completed

## Assignment via GitHub Assignees

Assigning a GitHub issue to a human team member is separate from agent dispatch.

From the expand view (Enter on a card), the operator can:
- view current assignees
- type a GitHub username to assign/unassign

This calls `gh issue edit --add-assignee` or `--remove-assignee`.
Assignees appear on the card below the title.

## `kanban config` Command

A new subcommand to manage local config without editing JSON by hand:

```bash
# Set repo key mapping for the current repo
kanban config set-repo-key --repo-key solarops.us \
  --repos-file /data/src/tacitsoft/infrastructure/tsctl/repos.yaml

# Set preferred runner for current repo
kanban config set-runner --runner claude --mode implement

# List runners
kanban config runners

# Show current config
kanban config show
```

## Duplicate Dispatch Guard

Before dispatching, kanban checks `dispatches.json` for an active record for the
same issue in the same repo.

If found:
```
Warning: #318 was already dispatched to claude 14 minutes ago.
Re-dispatch? [y/N]
```

If the operator confirms, a new dispatch record is written and the previous is
marked `superseded`.

## Acceptance Criteria

- Runner picker modal opens from `d` on any card
- Picker pre-populates runner, mode, and repo key from local config
- Manual runner shows copyable command without executing
- Dispatched cards show agent status line on the board
- `auto_move_on_dispatch` moves card to In Progress on confirm
- Duplicate dispatch guard warns and requires confirmation
- `kanban config set-repo-key` writes mapping without hand-editing JSON
- Dispatch records persist across board restarts
