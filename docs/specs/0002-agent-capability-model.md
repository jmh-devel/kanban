# Agent Capability Model

## Objective

Define how `kanban` becomes agent-aware without inheriting the full runtime complexity of `cline/kanban`.

## Why This Spec Exists

`cline/kanban` gets an important thing right: the board is more useful when it can launch and monitor work, not just display it.

What we want to borrow is the product idea, not the implementation stack.

## Design Principles

- Keep the board model generic.
- Keep agent execution pluggable.
- Do not hard-code any single agent vendor into the board core.
- Prefer capability flags over one-off agent special cases.
- Preserve a clean read-only mode when no execution backend is configured.

## Capability Model

Every execution backend should declare capabilities instead of leaking implementation details into the UI.

```go
type RunnerKind string

const (
    RunnerKindManual      RunnerKind = "manual"
    RunnerKindLocalCLI    RunnerKind = "local_cli"
    RunnerKindTSCTL       RunnerKind = "tsctl_dispatch"
)

type RunnerCapabilities struct {
    Kind                  RunnerKind
    SupportsPlanning      bool
    SupportsImplement     bool
    SupportsReview        bool
    SupportsStreamingLogs bool
    SupportsCancel        bool
    SupportsIssueContext  bool
    RequiresRepoKey       bool
}
```

## Initial Runner Targets

### Manual

Purpose:

- no execution
- only render launch commands or copyable prompts

Use when:

- user wants a board but no automation

### Local CLI

Purpose:

- run local tools such as `codex`, `claude`, or other shell-driven agents

Use when:

- the operator wants immediate local execution from the same workstation

### tsctl Dispatch

Purpose:

- launch remote or clustered agent work via `tsctl`

Use when:

- tasks should run in isolated sessions or k8s jobs

## Board-Level Agent Fields

These fields should be additive and optional.

```go
type AgentPreferences struct {
    PreferredRunner string `json:"preferred_runner,omitempty"`
    PreferredMode   string `json:"preferred_mode,omitempty"`
    RepoKey         string `json:"repo_key,omitempty"`
    Directive       string `json:"directive,omitempty"`
}
```

These do not belong in GitHub issue bodies. They belong in local kanban state.

## UI Actions

Each card may eventually expose these actions:

- `Plan`
- `Implement`
- `Review`
- `Audit`
- `Delegate`
- `Open Issue`
- `Open Repo`

In phase 1, only `Delegate` needs a backend contract.

## Explicit Borrowed Ideas From `cline/kanban`

- local web server as the primary operator surface
- current repo as the workspace anchor
- capability-driven execution decisions
- board as the center of execution and tracking

## Explicit Non-Goals Compared With `cline/kanban`

- no Cline-specific provider UI
- no worktree manager in the first agentified phase
- no embedded chat session model in the first agentified phase
- no terminal emulator surface in the browser in the first agentified phase

## Acceptance Criteria

- a typed runner capability model exists in code
- board JSON can carry optional agent preference metadata
- UI can render available actions from capabilities
- no specific runner is required for read-only board mode
