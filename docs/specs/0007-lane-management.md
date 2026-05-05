# Lane Management

## Objective

Define how issues move between lanes on the board, how lane state is persisted
to GitHub, and how the board stays consistent with remote state.

## Problem With Milestone-Only Grouping

The current MVP groups issues by milestone. That is read-only and coarse.

A useful board needs:

- independent lane position per issue, regardless of milestone
- operator-driven moves (Backlog → In Progress → Review → Done)
- state that round-trips to GitHub so it is visible to the whole team
- a model that does not require new GitHub Projects infrastructure

## Lane Model

Four lanes, always present:

| Lane        | GitHub Representation           | Notes                              |
|-------------|----------------------------------|-------------------------------------|
| Backlog     | open, no lane label              | default state for all new issues    |
| In Progress | open + label `kanban:in-progress`| explicitly started                  |
| Review      | open + label `kanban:review`     | work done, awaiting review          |
| Done        | closed, within last 14 days      | closing the issue is the commit act |

## Label Convention

Lane labels use the `kanban:` prefix to namespace them away from functional labels:

- `kanban:in-progress`
- `kanban:review`

These two labels must be created in the target repository before lane moves work.
The `kanban init` command should create them if they are missing.

Rules:

- an issue carries at most one lane label at a time
- adding `kanban:in-progress` removes `kanban:review` if present, and vice versa
- closing an issue implicitly moves it to Done; no label action required
- reopening a closed issue returns it to Backlog (no lane label)

## init Enhancements

`kanban init` should gain a `--setup-labels` flag (also run by default on first
board open) that creates the required labels in the remote repository:

```
kanban init --setup-labels
```

This calls `gh label create` for each missing label with a defined color:

| Label               | Color   |
|---------------------|---------|
| `kanban:in-progress`| `#0075ca` (blue)  |
| `kanban:review`     | `#e4e669` (yellow)|

If a label already exists with the same name, it is left unchanged.

## Move Operations

### Move to In Progress

Triggered by: `m` key → select "In Progress", or direct keybind in TUI.

Steps:
1. Remove `kanban:review` label if present (via `gh issue edit --remove-label`).
2. Add `kanban:in-progress` label (via `gh issue edit --add-label`).
3. Update local board state immediately (optimistic update).
4. Report success or rollback on failure.

### Move to Review

Triggered by: `m` key → select "Review".

Steps:
1. Remove `kanban:in-progress` label if present.
2. Add `kanban:review` label.
3. Optimistic local update.

### Move to Backlog

Triggered by: `m` key → select "Backlog".

Steps:
1. Remove `kanban:in-progress` and `kanban:review` labels.
2. Optimistic local update.

### Move to Done

Triggered by: `m` key → select "Done".

Steps:
1. Remove lane labels.
2. Close the issue (`gh issue close <number>`).
3. Optimistic local update.

## Conflict and Error Handling

- If the label does not exist on the remote, surface a clear error message:
  `Label 'kanban:in-progress' not found. Run: kanban init --setup-labels`
- If the `gh` call fails, roll back the optimistic update and show the error.
- If the issue was already moved by another operator, the next refresh reconciles the board.

## Board Reconciliation

On every refresh the board re-derives lane positions from live GitHub state.
Local optimistic moves are discarded once a successful refresh completes.

This means:

- no persistent local lane override store required
- lane state is always authoritative from GitHub
- refreshing after a conflict always wins

## Milestone Display

Milestones are preserved as a secondary grouping within each lane column.

Within a lane column, cards can optionally be grouped by milestone as sub-headers:

```
In Progress (3)
─────────────────
Sprint 12
  #287 Provision Ian...
  #318 Refactor dashboard...

Sprint 13
  #319 Build lane view...
```

This is toggled by a config key `board.group_by_milestone` (default `false`).
Default is flat card order within lane, sorted by issue number descending.

## Done Window

Done shows closed issues from the last N days (configurable, default 14).

The window is controlled by local config:

```json
{
  "board": {
    "done_window_days": 14
  }
}
```

Issues closed outside the window are not shown on the board.

## Acceptance Criteria

- `kanban init --setup-labels` creates `kanban:in-progress` and `kanban:review` in the repo
- Moving a card to In Progress adds the correct label and removes conflicting labels
- Moving a card to Done closes the issue on GitHub
- Optimistic updates render instantly; errors roll back
- Board reconciliation on refresh always reflects GitHub source of truth
- Done column shows only issues closed within the configured window
