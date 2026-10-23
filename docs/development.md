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
| `task changelog` | Refresh `CHANGELOG.md` from Conventional Commits, including unreleased work |
| `task release:changelog VERSION=...` | Generate changelog and release notes for an existing version tag |

Focused iteration (`PKG` defaults to `./...`; arguments after `--` go to `go test`):

```sh
task cli:test PKG=./internal/config                                          # one package
task cli:test PKG=./internal/config -- -run '^TestLoadLayersAndPathOrigins$' # one test
```

Before each commit, stage only intended files and run `task check:staged`. Run `task ci` before
pushing; never push without being asked. GitHub Actions runs the gate on Linux and macOS, plus
container smoke on Linux.

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
builds. `docs/decisions/` holds settled decisions; [design](design.md) holds contracts.

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

`task release:check` is credential-free and never publishes:

```sh
task release:check \
  VERSION=1.2.3 \
  COMMIT="$(git rev-parse HEAD)" \
  BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
task release:smoke
```

It builds the four `CGO_ENABLED=0` targets into `dist/release/v<VERSION>/`
(`noon-v<VERSION>-{linux-x64,linux-arm64,macos-x64,macos-arm64}.tar.gz`, `checksums.txt`,
`native-noon`) and verifies members, checksums, and embedded metadata. To smoke a release artifact,
pass `BINARY=dist/release/v1.2.3/native-noon VERSION=1.2.3` to `task release:smoke`.

Pushing a stable `v*` tag is the publication action. The release workflow smoke-tests the archives
on four native runners, requires a green `ci.yaml` for the tagged commit, and refuses to overwrite
an existing release; the `release` environment has no required reviewers. `workflow_dispatch` on an
existing tag validates without publishing.

### Changelog and release notes

[CHANGELOG.md](../CHANGELOG.md) is generated by git-cliff (`cliff.toml`). Refresh it with
`task changelog` and commit; never hand-edit entries. Backlog bookkeeping and merge commits are
omitted.

Each release build runs `task release:changelog VERSION=1.2.3`, which needs the `v1.2.3` tag and
full history. It writes `CHANGELOG.md` (attached to the release) and `release-notes.md` (the release
body, that version only) into `dist/release/v1.2.3/`, stopping at the tag. The workflow never
commits or moves tags.

After a release is published, generate the tap formula from its real checksums only. From the Noon
repository root:

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

Review the formula and update the tap README. Then, from a disposable Homebrew environment, run the
tap's `.github/scripts/check-homebrew-tap.sh brettinternet/tap noon` and:

```sh
task release:smoke BINARY="$(brew --prefix brettinternet/tap/noon)/bin/noon" VERSION="$VERSION"
```

The formula's `brew test` confirms a bare invocation runs no job, creates no state, and installs no
service. Tap publication is separate from the Noon release workflow.

Release CI also installs `latest` and the pinned version through mise on all four platforms, with
fresh mise directories, isolated HOME, an empty environment, and `MISE_MINIMUM_RELEASE_AGE=0`. It
calls mise by absolute path with no shims on PATH; shims would let `gh auth token` re-enter mise
recursively. Don't reproduce this with only directory overrides in a normal shell.

## Worktrees and backlog

Create a worktree only when asked. Review `.config/wt.toml`, then:

```sh
mise exec worktrunk -- wt switch --create NAME --base main --no-cd --format=json
```

Preparation installs tools and hooks; plain Git worktrees need `task init`. Change task state only
in the primary checkout.

`backlog/` is the only work queue; read and change tasks only through `mise exec -- bl` (CLI or
MCP), never direct Markdown edits. Read `mise exec -- bl instructions` and
`mise exec -- bl instructions workflow` first. Drafts are ideas, never work. A ready task has
scope, non-goals, allowed files, dependencies, and local acceptance commands with expected results.

Acceptance commands isolate Noon from your real config and state:

```sh
noon_ac_home=$(mktemp -d)
env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon list
```

One task, start to finish, in the primary checkout with one stable session for the whole loop:

1. `mise exec -- bl next --claim --start --session SESSION --json`. This picks and claims work in
   one step; a status change is not a claim. Read the task, then the design sections and decisions
   it cites. If it conflicts with a decision, record that in the task notes and stop.
2. Implement within the allowed files. Justify any expansion in Implementation Notes first.
3. Run every acceptance command. Record each criterion's command and observed result with
   `mise exec -- bl ac ID check N --evidence TEXT`, checking a criterion only if it passed as
   written. Renew with `mise exec -- bl heartbeat --session SESSION` at least once per half the
   claim TTL.
4. Meet the Definition of Done, stage, run `task check:staged`, and commit (Conventional Commits).
5. Finish with `mise exec -- bl done ID --session SESSION`, which writes then releases before
   returning, with a summary. If stopping incomplete, persist progress and evidence, then use
   `mise exec -- bl release ID --session SESSION`.
