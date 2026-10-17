# Development

## Setup

```sh
mise trust && mise exec -- task init   # pinned tools, Go modules, Git hooks
task cli:build                         # bin/noon
task cli:run -- --help                 # run from source
```

`task init` starts no services. No Python or Node is needed. Tooling follows the sibling `hum`
(Go CLI) and `project` (Task, mise, worktrees, staged checks) repos.

## Commands

| Command | Purpose |
| --- | --- |
| `task` | List tasks |
| `task test` | Unit and CLI tests |
| `task cli:test PKG=./internal/config -- -run TestLoad -count=1 -v` | Uncached, focused config tests |
| `task cli:check` | gofmt, vet, Staticcheck |
| `task race` | Tests with the race detector |
| `task smoke` | Exercise the built binary in a temp environment |
| `task release:check VERSION=... COMMIT=... BUILD_TIME=...` | Build and verify four local release archives; never publishes |
| `task release:smoke` | Verify isolated no-argument behavior of the native binary |
| `task shell:smoke` | Real Bash, Zsh, and Fish integration |
| `task security` | Secret scan of Git history; govulncheck |
| `task check:staged` | Format and re-stage Go; scan staged secrets |
| `task ci` | Full gate: security, check, test, race, smoke |
| `task docker:smoke` | Build the image; check it read-only |
| `task docker:integration` | Compose scheduling, signals, persistence (Linux engine) |
| `task backlog` / `backlog:next` | Ready, unclaimed work |
| `task changelog` | Changelog from Conventional Commits |

Bare `task` or `task --list` lists the available checks. `task test` and `task ci` run all
packages by default; `task cli:check` checks formatting, vet, and Staticcheck across all packages.
For focused iteration, select packages on the existing test task (still using pinned mise tools):

```sh
task cli:test PKG=./internal/config                            # one package
task cli:test PKG=./internal/config -- -run '^TestLoadLayersAndPathOrigins$' # one test
task cli:test PKG=./internal/config -- -run TestLoad -count=1 -v # uncached matching tests
```

`PKG` defaults to `./...` when omitted. Arguments after `--` are forwarded unchanged to `go test`;
`-count=1` disables test-result caching. Use full checks before delivery, not just the focused run.
Before each commit, stage only intended files and run
`task check:staged`. Run `task ci` before pushing. Never push without being asked. GitHub Actions
runs the gate on Linux and macOS, plus container smoke on Linux. The explicit tagged-release
workflow is described below.

## Structure

```text
cmd/noon ─▶ internal/cli ─┐
internal/mcp ─────────────┼─▶ internal/app ─▶ config, schedule, store, runner, scheduler
internal/httpapi ─────────┤
internal/tui ─────────────┘
```

See [design § Architecture](design.md#architecture) for decided dependencies.

| Package | Owns |
| --- | --- |
| `cmd/noon` | Process entry, signals, build metadata |
| `internal/cli` | Commands, flags, human and JSON output |
| `internal/app` | Operations shared by CLI, MCP, HTTP, and TUI; returns plain structs |
| `internal/config` | Discovery, strict YAML, precedence, validation, digests |
| `internal/schedule` | Schedule grammar and next-time math (pure) |
| `internal/scheduler` | Planner (pure) and the serve loop |
| `internal/store` | SQLite, logs, locks, recovery |
| `internal/runner` | Processes, environment, timeouts, output capture |
| `internal/service` | launchd and systemd units |
| `internal/cronimport` | Crontab → jobs |
| `internal/mcp`, `httpapi`, `tui` | Adapters |
| `internal/skill` | Embedded shell-only agent skill |

Adapters parse and render; rules live in `internal/app` and below. Add a package when a task
needs it. No Go plugins or LLM SDKs. A new module dependency needs a reason in the task's
Implementation Notes: what it replaces, whether it's maintained, and proof `CGO_ENABLED=0` still
builds. `backlog/decisions/` holds settled decisions; [design](design.md) holds contracts.

## Tests

Implement first, then test critical behavior. Name the real failure a test prevents before adding
it. Prefer a few public-interface tests over wiring checks.

Test each rule once, where it's computed:

| Rule | Where |
| --- | --- |
| Settings, manifests, precedence, overrides, digests | `internal/config` |
| Schedule grammar, next occurrence, DST | `internal/schedule` (tables + `FuzzScheduleParse`) |
| Missed, overlap, watermarks | `internal/scheduler` (fake clock) |
| argv, env, process groups, timeouts, capture | `internal/runner` (real `sh` in temp dirs) |
| Migrations, transitions, retention, recovery | `internal/store` (real SQLite in `t.TempDir()`) |
| Shared operations | `internal/app` |
| Flags, rendering, JSON, exit codes | `internal/cli` via `cli.Run` |
| Built binary end to end | `integration/` (one test per user-visible behavior) |

- Never read the real clock, home, config, or state. Pass time in; point `HOME`,
  `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and `NOON_STATE_DIR` at temp dirs.
- Poll with a deadline instead of sleeping. Shorten waits through options, not weaker assertions.
- Move shared helpers to `internal/testutil` once a second package needs them.
- Free-form parsers (schedules, crontabs) get a fuzz target.
- Never call real agent CLIs or the network; use fake executables.
- Don't test getters, framework behavior, prose, or static config. Never weaken a test to pass.

## Build metadata

Release builds set `main.buildVersion`, `main.buildCommit`, and `main.buildTime` with `-ldflags -X`
(defaults `dev`, `unknown`, `unknown`). Docker takes `VERSION`, `COMMIT`, and `BUILD_TIME` build
args.

## Release packaging

`task release:check` is credential-free and never publishes or updates the sibling tap. Supply a
stable version without the `v` prefix, the full commit SHA, and a UTC RFC 3339 build time (to whole
seconds):

```sh
task release:check \
  VERSION=1.2.3 \
  COMMIT="$(git rev-parse HEAD)" \
  BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
task release:smoke
```

The task builds all four `CGO_ENABLED=0` targets and verifies archive members, SHA-256 checksums,
and the native binary's version/commit/build-time metadata. Output is in
`dist/release/v<VERSION>/`: four `noon-v<VERSION>-{linux-x64,linux-arm64,macos-x64,macos-arm64}.tar.gz`
archives, `checksums.txt`, and `native-noon` for native smoke tests. `task release:smoke` builds and
checks the local binary by default; to exercise a release artifact, pass
`BINARY=dist/release/v1.2.3/native-noon VERSION=1.2.3`.

The release workflow runs on a `v*` tag push. It smoke-tests the same archives on four native
runners, verifies successful `ci.yaml` for the exact tagged commit, uses the `release` environment
as its publication gate, and refuses to overwrite an existing GitHub release. Configure required
approval for that environment in repository settings. `workflow_dispatch` accepts an existing tag
for validation only and never publishes. Neither local validation nor a tap update publishes a
release. A stable tag push is the explicit publication action.

After an authorized release is actually published, generate the tap formula only from its real
checksums; do not invent or copy checksum values from another build. From the Noon repository root:

```sh
VERSION=1.2.3
noon_release_dir="$(mktemp -d)"
gh release view "v$VERSION" --repo brettinternet/noon
gh release download "v$VERSION" --repo brettinternet/noon \
  --pattern 'noon-v*.tar.gz' --pattern checksums.txt --dir "$noon_release_dir"
(cd "$noon_release_dir" && shasum -a 256 -c checksums.txt)
scripts/generate-homebrew-formula.sh "$VERSION" "$noon_release_dir/checksums.txt" \
  > ../homebrew-tap/Formula/noon.rb
```

The generated formula uses the tap's existing `on_macos`/`on_linux` and `on_arm`/`on_intel`
platform pattern. Review it, update the tap README, then run the tap's `.github/scripts/check-homebrew-tap.sh`
with `brettinternet/tap noon` from a disposable Homebrew environment. The formula's `brew test`
checks version reporting and confirms that a bare invocation neither executes a fixture job, creates
state, nor installs a per-user service. Run the same full isolated safety check against the installed
Homebrew executable with `task release:smoke BINARY="$(brew --prefix brettinternet/tap/noon)/bin/noon" VERSION="$VERSION"`.
Formula creation and tap publication remain separate from the Noon release workflow.

Release CI installs both `latest` and the pinned version on all four native platforms using fresh
mise cache/data/state directories, isolated HOME/config, and an empty inherited environment.
The check invokes the mise binary by absolute path with only system tools on PATH: never retain
mise shims or personal tool configuration when changing mise's data directory. Otherwise credential
commands such as `gh auth token` and gem discovery can re-enter mise through shims recursively.
Do not run these checks with only cache/data/state overrides in a normal developer shell.
CI sets `MISE_MINIMUM_RELEASE_AGE=0` only for this immediate
post-publication check; normal installations retain the user's release-age policy. A newly published
release may therefore require an explicit version until that policy's waiting period passes.

## Worktrees and backlog

Create a worktree only when asked. Review `.config/wt.toml`, then:

```sh
mise exec worktrunk -- wt switch --create NAME --base main --no-cd --format=json
```

Preparation installs tools and hooks; plain Git worktrees need `task init`. Change task state only
in the primary checkout.

`backlog/` is the only work queue; change it through the Backlog.md CLI (`backlog instructions
overview`). Drafts are ideas, never work. A ready task has scope, non-goals, allowed files,
dependencies, and local acceptance commands with expected results.

Acceptance commands isolate Noon from your real config and state:

```sh
noon_ac_home=$(mktemp -d)
env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon list
```

One task, start to finish:

1. `task backlog:next`. Read the task, then the design sections and decisions it cites. If it
   conflicts with a decision, record that in the task notes and stop.
2. Claim it in the primary checkout: `backlog task edit ID --status "In Progress"`. Reread it.
3. Implement within the allowed files. Justify any expansion in Implementation Notes first.
4. Run every acceptance command. Append `AC#N: command -> result` to Implementation Notes, and
   check a criterion only if it passed as written.
5. Meet the Definition of Done, stage, run `task check:staged`, and commit (Conventional Commits).
6. Mark the task Done with a summary, or record the blocker, evidence, and unblock condition.
