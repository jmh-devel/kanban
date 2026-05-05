# Kanban Spec Index

## Current Breakoff Point

The repository has a working MVP implementation already:

- Go CLI scaffolded
- repo autodetection from `pwd` and `origin`
- `gh`-backed milestone and issue loading
- terminal board rendering
- local web server with embedded HTML template
- GitHub Actions validation and Linux release workflows
- pre-commit and secret-scanning configuration
- sample systemd unit

What is not done yet:

- repository initialization and publishing to `jmh-devel`
- richer browser UX beyond read-only board display
- GitHub write paths such as creating or editing issues
- tsctl integration
- agent session lifecycle and execution model
- persistent local workspace state beyond runtime-only reload

## Decision Summary

The right architectural split is now explicit:

- use `gh-kanban.sh` as the original seed concept
- use `cline/kanban` as a reference product for future agentic ideas
- do not fork `cline/kanban` for this repo
- keep the implementation Go-first, GitHub-first, and generic

## Spec Pack

- [0001-mvp.md](0001-mvp.md): MVP scope and the adopt-vs-fork decision
- [0002-agent-capability-model.md](0002-agent-capability-model.md): how to agentify the board without turning it into a Cline clone
- [0003-tsctl-integration.md](0003-tsctl-integration.md): how kanban should dispatch and observe `tsctl` work
- [0004-board-state-and-cache.md](0004-board-state-and-cache.md): local state, cache, repo mapping, and UI refresh rules
- [0005-session-handoff.md](0005-session-handoff.md): exact resume point for the next code session

## Recommended Next Session

If the goal is to get back to Jason's SolarOps work quickly, the next coding session should stop at specification and interface work only.

Recommended order:

1. ratify the spec pack in this folder
2. initialize and publish the repo only if you want external visibility now
3. implement phase 1 agentification as interface stubs and server endpoints only
4. leave actual agent execution to a later, isolated session

That keeps this repo from eating focus that belongs back on SolarOps.
