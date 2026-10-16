# Development

## Setup

Install mise, then run `mise trust && mise exec -- task init`. This installs the
pinned toolchain, downloads Go modules, and installs Lefthook hooks. No scheduler
or application service starts. No Python or Node runtime is required.

Tooling follows ../hum (Go CLI) and ../project (Task, mise, worktree preparation,
staged checks). Web app/container-stack machinery is deliberately not copied.
Go 1.27.2 and urfave/cli v3 match hum. The module path matches the Noon origin.
MIT licensing follows hum.

## Commands

| Command | Purpose |
| --- | --- |
| `task` | List development commands |
| `task cli:run -- --help` | Run from source |
| `task cli:build` | Build bin/noon |
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

- `cmd/noon/main.go`: process boundary, signals, linker-injected build metadata.
- `internal/cli`: urfave commands, flag/env sources, human/JSON rendering.
- `internal/config`: filesystem discovery, strict YAML, precedence and validation;
  no dependency on CLI framework and no job execution.
- `docs/design.md`: intended scheduler architecture, not claims of implementation.
- `backlog/`: authoritative work queue, maintained only through Backlog.md;
  `backlog/decisions/` holds settled architecture decisions.

Planned packages and their single owners are in
[design § Architecture](design.md#architecture). Create a package when the first
task needs it, not before. Operations shared by CLI, MCP, HTTP, and TUI live in
`internal/app` and return plain structs; adapters only parse and render. No
in-process Go plugins or embedded LLM SDK in the core.

Dependencies are decided in the design. Adding any other module needs a reason in
the task's Implementation Notes: what it replaces, its maintenance status, and that
`CGO_ENABLED=0` builds still work.

## Tests

Implement first, then test critical behavior. Before adding a test, identify the
real failure it prevents. Prefer a few public-interface tests over wiring checks.

Test each rule once, at the package that computes it:

| Rule | Package and file |
| --- | --- |
| Settings and job manifests: parsing, precedence, overrides, digests | `internal/config` (`config_test.go`, `jobs_test.go`) |
| Schedule grammar, next occurrence, DST | `internal/schedule` (table tests plus `FuzzScheduleParse`) |
| Planner decisions: missed, overlap, watermarks | `internal/scheduler` (`plan_test.go`, fake clock) |
| Processes: argv, env, groups, timeouts, capture | `internal/runner` (real `sh` children in temp dirs) |
| Store: migrations, transitions, retention, recovery | `internal/store` (real SQLite in `t.TempDir()`) |
| Shared operations | `internal/app` |
| Flags, rendering, JSON shape, exit codes | `internal/cli` via `cli.Run` with isolated env |
| Built binary end to end | `integration/` (one test per user-visible behavior) |

- Never read the real clock, home, config, or state in tests: pass time as an
  argument and set `HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and
  `NOON_STATE_DIR` to temp dirs.
- Poll for conditions with a deadline; never sleep a fixed time unless elapsed
  time is the behavior. Shorten grace periods and poll intervals through options,
  not by weakening assertions.
- Shared helpers go in `internal/testutil` once a second package needs them.
- Parsers of free-form text (schedules, crontabs) get a fuzz target that reuses
  the public parser.
- Tests never invoke real agent CLIs or the network; use fake executables.
- Process-boundary smoke: `scripts/smoke.sh`; never touches real user settings.

Do not test trivial getters, framework behavior, prose, or static configuration.
Never weaken tests to make checks pass.

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
No second TODO queue in docs. Drafts are ideas, not work: never implement one.

Acceptance commands that run `bin/noon` against a fixture use
`env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon ...` with `noon_ac_home=$(mktemp -d)`,
so real user config, state, and `NOON_*` variables cannot change the result. Write
new ones the same way.

One task, start to finish:

1. `task backlog:next` shows the earliest ready task. Read it in full, then the
   design sections and decisions it cites. Decisions are settled: if a task
   cannot follow one, record the conflict in its notes and stop rather than
   diverge.
2. Claim it in the primary checkout with `backlog task edit ID --status "In
   Progress"`, then reread it.
3. Implement within the modified-file contract. Justify any expansion in
   Implementation Notes before making it.
4. Run every acceptance command. Append `AC#N: command -> result` to
   Implementation Notes and check the criterion only when it passed as written.
5. Meet the project Definition of Done, stage the intended files, run
   `task check:staged`, and commit with a Conventional Commit.
6. Mark the task Done with a final summary, or record the blocker, evidence, and
   unblock condition.
