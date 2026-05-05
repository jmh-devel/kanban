# kanban

`kanban` is a lightweight GitHub issue board CLI for the repository under the current working directory.

It is intentionally small:

- detect the active git repo from `pwd`
- resolve the GitHub slug from `origin`
- fetch milestones and open issues through `gh`
- render a board in the terminal or through a local web server
- stay generic enough to work in any repo that already uses GitHub Issues

## Why this exists

Your original shell script in [gh-kanban.sh](/data/src/tacitsoft/core/lcm/gh-kanban.sh) proved the core value: a quick board view over GitHub issues grouped by milestone. This project turns that into a reusable CLI with a cleaner UX, JSON output, and a small local UI.

## Why not fork `cline/kanban`

`cline/kanban` is a serious and interesting reference, but it is a different product category.

- It is a full local orchestration platform, not just a GitHub board tool.
- It brings a Node/TypeScript runtime server, a Vite web UI, worktree lifecycle, task sessions, and Cline-specific agent integration.
- It is optimized for agent execution flows first, and GitHub/project board concerns second.

That makes it a good design reference for future agentic features, but a poor base for a small generic public CLI.

The right move here is:

1. build a small Go core around `git` + `gh`
2. keep the board model generic and portable
3. leave clean extension points for future tsctl or agent backends

The detailed comparison lives in [docs/specs/0001-mvp.md](docs/specs/0001-mvp.md).

## Requirements

- `git`
- `gh`
- authenticated GitHub CLI session for the target repository

## Commands

```bash
# Print a milestone-grouped board for the current repo
kanban

# Explicit commands
kanban init
kanban tui
kanban print
kanban json
kanban serve
kanban open
kanban move 123 review
kanban config show
kanban config runners
kanban config set-repo-key --repo-key solarops.us
kanban config set-runner --runner tsctl --mode implement

# Override repo autodetection
kanban print --repo jmh-devel/solarops.us
kanban serve --path /data/src/tacitsoft/core/solarops.us

# Scaffold repo publish (dry-run by default)
kanban init --owner your-org

# Create lane labels when missing
kanban init --setup-labels

# Execute publish
kanban init --owner your-org --apply

# If origin already points at GitHub, owner can be auto-detected
kanban init --apply

# Launch standalone local UI:
# - picks a free localhost port
# - opens your default browser
# - exits when browser window/tab closes
kanban open
```

## Local server

```bash
kanban serve --addr 127.0.0.1:3584
```

Then open `http://127.0.0.1:3584`.

## TUI

`kanban tui` opens an interactive board. Use `j/k` to move between cards,
`h/l` or `Tab` to change lanes, `Enter` to expand a card, `d` to dispatch an
issue to an agent, `m` to move lanes, `o` to open the issue in a browser, and
`r` to refresh.

In narrow terminals, `Enter` opens a plain detail view so the expand action is
still visible even when the full board layout falls back to text rendering.

## Agent dispatch

Both the web UI and TUI dispatch through the same backend path. The dispatch
picker uses configured runners from `kanban config runners` plus `manual`, and
uses per-repo defaults from:

```bash
kanban config set-repo-key --repo-key solarops.us
kanban config set-runner --runner tsctl --mode implement
```

For `tsctl` runners, dispatch executes commands like:

```bash
tsctl agent dispatch solarops.us --runner tsctl --issue 318 --mode implement
```

Manual dispatch records the generated command without executing it. If an issue
already has an active dispatch, the TUI and web UI require a second confirmation
before recording a replacement dispatch.

If Enter in the TUI dispatch modal appears to do nothing:

- ensure you are running the latest installed binary (`make install`)
- configure a repo catalog path when required by your tsctl setup:

```bash
kanban config set-repo-key --repo-key kanban --repos-file /data/src/tacitsoft/infrastructure/tsctl/repos.yaml
```

You can also export `KANBAN_REPOS_FILE` as a fallback.

## Development

```bash
make fmt
make check
make build
```

## Security and quality gates

- `gofmt`
- `go vet`
- `go test`
- `govulncheck` in CI
- `gitleaks` in pre-commit and CI

## Systemd

An example service unit is included at [packaging/systemd/kanban.service](packaging/systemd/kanban.service).

The Debian package also installs [packaging/systemd/kanban.default](packaging/systemd/kanban.default) as `/etc/default/kanban`.

Configure these variables:

- `KANBAN_PATH`: repository path to serve
- `KANBAN_REPO`: optional explicit `owner/repo`
- `KANBAN_ADDR`: bind address (default `127.0.0.1:3584`)

Use systemd drop-ins for per-host overrides:

```bash
sudo systemctl edit kanban
```
