# bump

Repo-local scheduling, without a platform.

Bump is being bootstrapped as a Go CLI for scheduled commands and agent sessions.
It will run locally or inside a container, keep configuration beside the work,
and expose the same operations to humans and agents.

The initial implementation covers CLI and configuration only; scheduling, job
execution, hooks, MCP, and a TUI are not implemented yet.

Tooling follows the sibling hum and project repositories: mise, Task, Lefthook,
Backlog.md, staged secret checks, and Conventional Commits.

See docs/ for the design and `backlog task list --plain` for implementation work.
