# Development

## Setup

Install mise, then run `mise trust && mise exec -- task init`. This installs the
pinned toolchain, downloads Go modules, and installs Lefthook hooks. No scheduler
or application service starts. No Python or Node runtime is required.

Tooling follows ../hum (Go CLI) and ../project (Task, mise, worktree preparation,
staged checks). Web app/container-stack machinery is deliberately not copied.
Go 1.27.2 and urfave/cli v3 match hum. The module path matches the Bump origin.
MIT licensing follows hum.

## Commands

| Command | Purpose |
| --- | --- |
| `task` | List development commands |
| `task cli:run -- --help` | Run from source |
| `task cli:build` | Build bin/bump |
| `task test` | Unit and CLI tests |
| `task cli:test -- -run TestLoad` | Focused tests |
| `task cli:check` | gofmt, vet, Staticcheck |
| `task race` | Race-enabled tests |
| `task smoke` | Build and exercise CLI in a temporary environment |
| `task security` | Git history secret scan and reachable Go vulnerability scan |
| `task check:staged` | Format/re-stage Go and scan staged secrets |
| `task fix:staged` | Staged formatter only |
| `task ci` | Full gate |
| `task docker:smoke` | Build image; check version/config in read-only container |
| `task backlog` | Ready, unclaimed work |
| `task changelog` | Generate changelog from Conventional Commits |

Stage only intended files and run `task check:staged` before every commit. Never
push without instruction; run the full `task ci` gate independently before pushing.
Use focused checks while iterating. GitHub Actions runs the gate on Linux/macOS;
container smoke runs on Linux. Release publication is not wired up yet.

## Structure

- `cmd/bump/main.go`: process boundary, signals, linker-injected build metadata.
- `internal/cli`: urfave commands, flag/env sources, human/JSON rendering.
- `internal/config`: filesystem discovery, strict YAML, precedence and validation;
  no dependency on CLI framework and no job execution.
- `docs/design.md`: intended scheduler architecture, not claims of implementation.
- `backlog/`: authoritative work queue, maintained only through Backlog.md.

Avoid speculative interfaces. Extract a shared application operation when CLI and
MCP actually need it. No in-process Go plugins or embedded LLM SDK in the core.

## Tests

Implement first, then test critical behavior. Before adding a test, identify the
real failure it prevents. Prefer a few public-interface tests over wiring checks.

- Configuration parsing, precedence, path anchoring, and discovery boundaries:
  `internal/config/config_test.go`, exercising `Load` / `ProjectDir` with temp files.
- User-facing validation, machine output, non-destructive init, and environment/flag
  precedence: `internal/cli/root_test.go`, exercising `Run`.
- Process-boundary smoke: `scripts/smoke.sh`; never touches real user settings.
- Future scheduler/executor tests live beside their owning package; real process
  lifecycle tests go in `integration/` only when necessary.

Do not test trivial getters, framework behavior, prose, or static configuration.
Never weaken tests to make checks pass. Prefer observable state/counters over
sleep-based timing assertions. A config parser fuzz target should share the
public loader rather than duplicate parsing logic.

## Build metadata

Release builds can set `main.buildVersion`, `main.buildCommit`, and
`main.buildTime` using Go linker `-X` flags. Development defaults are
`dev` / `unknown` / `unknown`. The Docker build accepts corresponding
`VERSION`, `COMMIT`, and `BUILD_TIME` build arguments.

## Worktrees and backlog

Only create a worktree when directed. Review `.config/wt.toml`, then use
`mise exec worktrunk -- wt switch --create NAME --base main --no-cd --format=json`.
Preparation installs tools and hooks; it never starts services. Plain Git worktrees
need `task init`. Keep task-state mutations in the primary checkout.

Read `backlog instructions overview` and use the CLI for task mutations. Each
ready task specifies scope/non-goals, allowed modified files, dependencies,
locally executable acceptance commands, and expected results. Future acceptance
commands are targets to implement, not assertions that today's bootstrap passes.
No second TODO queue in docs.
