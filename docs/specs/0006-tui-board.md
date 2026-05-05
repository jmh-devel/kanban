# TUI Board Interface

## Objective

Build a high-quality terminal user interface for the kanban board that mirrors the card-driven column
layout of cline/kanban's browser UI — without a browser dependency.

## Why a TUI and not just terminal text

The current `kanban print` output is a flat text dump: a scrollable list grouped by milestone.
That works for a quick glance but fails as an operator surface:

- no card selection or navigation
- no in-place actions (move, dispatch, open)
- no live refresh feedback
- unreadable at 36+ issues

A proper TUI turns the same data into an interactive board that operators can navigate and act on
without leaving the terminal.

## Technology Choice

Use the [Charm](https://charm.sh) stack:

- `github.com/charmbracelet/bubbletea` — Elm-architecture TUI framework, standard for Go CLIs
- `github.com/charmbracelet/lipgloss` — layout, color, borders
- `github.com/charmbracelet/bubbles` — spinner, key bindings, viewport

These are the right primitives. They are actively maintained, used by widely-deployed Go CLIs,
and produce a terminal experience that matches the quality bar the cline/kanban screenshot sets.

No ncurses, no termbox, no tview. Bubbletea only.

## Command Surface

```bash
kanban tui [--path DIR] [--repo owner/repo] [--addr HOST:PORT]
```

`kanban tui` is the new default invocation when running interactively.

`kanban print` and `kanban json` remain for scripting and piped output.

## Layout

```
┌─ jmh-devel/solarops.us ──────────────────────── main · ↑0 ↓0 · 2026-05-05 ─┐
│                                                                               │
│  Backlog (32)          In Progress (2)     Review (1)       Done (3)         │
│  ─────────────         ────────────────    ──────────        ───────          │
│  #318 Kanban           #287 Provision      #308 Quick        #301 Docs...     │
│  Refactor admin...     Ian: CRM + web...   Quote bug...      #295 Login...    │
│  [crm] [frontend]      [user-mgmt]         [bug] [fe]        #290 Cache...    │
│  ▸ priority:high       ▸ priority:high     ▸ priority:high                   │
│                                                                               │
│  #319 Build            #296 BizOps OS      │                                  │
│  lifecycle lane...     Autonomous...                                          │
│  [crm] [backend]       [epic] [backend]                                       │
│  ▸ priority:high       ▸ agent: codex                                         │
│                                                                               │
│ ─────────────────────────────────────────────────────────────────────────── │
│  [j/k] navigate  [h/l] column  [Enter] expand  [d] dispatch  [m] move  [?]  │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Column Model

Four fixed columns, always present even when empty:

| Column      | Meaning                                              |
|-------------|------------------------------------------------------|
| Backlog     | Open issues with no in-progress or review label      |
| In Progress | Issues carrying `kanban:in-progress` label           |
| Review      | Issues carrying `kanban:review` label                |
| Done        | Issues closed in the last 14 days                    |

Column widths divide the terminal evenly. Minimum usable width: 80 columns.
At narrower widths, the board falls back to `kanban print` text mode.

## Card Anatomy

Each card renders:

```
┌────────────────────────────────┐
│ #318 Refactor admin dashboard  │  ← issue number + truncated title
│ [crm] [frontend]               │  ← up to 4 labels (truncated with +N)
│ ▸ priority:high                │  ← priority label rendered distinctly
│ ● codex · implementing...      │  ← agent status line (when active)
└────────────────────────────────┘
```

Selected card is highlighted with a contrasting border color.

Labels are colored by prefix:
- `priority:high` → red
- `priority:medium` → yellow
- `priority:low` → dim
- `epic` → magenta
- `bug` → red
- everything else → blue

## Key Bindings

### Navigation

| Key          | Action                              |
|--------------|-------------------------------------|
| j / ↓        | Move selection down within column   |
| k / ↑        | Move selection up within column     |
| h / ←        | Move focus to left column           |
| l / →        | Move focus to right column          |
| gg / Home    | Jump to top of current column       |
| G / End      | Jump to bottom of current column    |
| Tab          | Cycle columns forward               |
| Shift+Tab    | Cycle columns backward              |

### Actions

| Key   | Action                                        |
|-------|-----------------------------------------------|
| Enter | Expand card (full title, body, labels, links) |
| d     | Dispatch to agent (opens runner picker)       |
| m     | Move card to another lane                     |
| o     | Open issue in browser                         |
| c     | Copy issue number to clipboard                |
| r     | Refresh board from GitHub                     |
| q     | Quit                                          |
| ?     | Show key binding help overlay                 |

### Runner picker (modal, triggered by `d`)

```
Dispatch #318 to agent

  Runner:   [ codex ] [ claude ] [ tsctl ] [ manual ]
  Mode:     [ implement ] [ plan ] [ review ]
  Repo key: solarops.us   (from local config)

  [Enter] confirm   [Esc] cancel
```

## Header Bar

```
jmh-devel/solarops.us  ·  main  ·  ↑0 ↓0  ·  updated 14s ago  ·  [r] refresh
```

Fields:
- repo slug (from detected or explicit origin)
- current branch
- unpushed / unpulled commit counts (from `git status`)
- time since last board refresh

## Status Bar

Single line at the bottom right of the board:

```
[j/k] navigate  [h/l] column  [Enter] expand  [d] dispatch  [m] move  [?] help
```

Replaced with action feedback for 2 seconds after any command:

```
✓ Moved #318 to In Progress
```

## Refresh Model

- Board state loads on startup.
- Auto-refresh every 60 seconds in the background (configurable via `KANBAN_REFRESH_TTL`).
- `r` forces an immediate refresh.
- In-flight refresh shows a spinner in the header bar.
- Stale data is never silently discarded: previous board remains visible during refresh.

## Expand View (Enter)

Pressing Enter on a selected card opens a viewport overlay showing:

- full issue title
- issue body (first 40 lines, scrollable)
- all labels
- milestone
- assignees
- link to GitHub
- available agent actions

Dismiss with `Esc` or `q`.

## Terminal Compatibility

Must render correctly in:

- xterm-256color
- tmux
- screen
- macOS Terminal.app
- Linux terminal emulators (Alacritty, kitty, GNOME Terminal)

Degraded gracefully (no color, box-drawing chars replaced with ASCII) when `TERM` is `dumb`
or `NO_COLOR` is set.

## Acceptance Criteria

- `kanban tui` renders a four-column board with card-driven layout
- Navigation with j/k/h/l works without lag
- Card expand shows full issue body
- Agent dispatch modal appears and builds the correct command
- Lane move applies the correct GitHub label
- Auto-refresh fires every 60 seconds
- Board renders correctly at 80, 120, and 200 column widths
- `NO_COLOR` produces an accessible monochrome layout
