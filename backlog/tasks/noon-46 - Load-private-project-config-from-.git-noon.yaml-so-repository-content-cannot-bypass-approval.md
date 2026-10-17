---
id: NOON-46
title: >-
  Load private project config from .git/noon.yaml so repository content cannot
  bypass approval
status: Done
assignee:
  - '@pi'
created_date: '2026-10-17 02:05'
updated_date: '2026-10-17 02:21'
labels:
  - security
  - config
dependencies: []
priority: high
type: bug
ordinal: 13400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: .noon.yaml is private by filename alone; its jobs run unattended without approval. A PR can commit .noon.yaml (git add -f or a .gitignore edit); checking it out in an enabled project while noon serve runs makes serve reload within 2s and run the PR's jobs with no approval, while the same job in noon.yaml shows needs_approval. Git also overwrites an ignored .noon.yaml when checking out a branch that tracks it, and deletes it on switching back. Detecting tracking status at load (git ls-files) was rejected: git checkout writes files before the new index, so a reload can see the PR's file while the index still says untracked; it also runs git every reload and needs fail-closed handling. Same fix as tell TELL-15/TELL-24.

Outcome: the private layer is <git common dir>/noon.yaml (.git/noon.yaml in a normal checkout; linked worktrees share the main checkout's). Git refuses paths inside .git, so repository content can never supply it. The common dir is resolved from <project>/.git (directory, or gitdir file plus commondir) with bounded reads; git never runs. An in-tree .noon.yaml is ignored entirely (no compatibility, no migration warning) and no longer marks a project. Without .git in the project directory there is no private layer: init --local, add without --shared, and import --write fail with guidance. add/remove/import --write and init --local edit .git/noon.yaml; remove --local selects it. Serve's reload watches it. list/status/serve/MCP/run agree because the path is chosen in config.Load; noon config sources and list --json source show it. The Git ignore protection from NOON-43 (init --local, add notes, doctor local_config_ignored) is removed since it no longer applies.

Non-goals: trust or approval stores; walking above the project to find a Git directory; per-worktree private files; reading GIT_DIR/GIT_COMMON_DIR or executing git; changing approval digests, user-config, or --config semantics.

Modified-file contract: internal/config/config.go, internal/config/config_test.go, internal/cli/{manage.go,root.go,import.go} and their existing tests (manage_test.go, root_test.go, import_test.go, approval_test.go, shell_test.go), internal/config/jobs.go (messages), internal/config/jobs_test.go, internal/scheduler/serve.go, internal/app/doctor.go, integration/examples_test.go, README.md, docs/{configuration.md,design.md,cli.md,crontab.md}, examples (local-overrides and README mentions of .noon.yaml).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 In a real temp Git repo with HOME/XDG/NOON_* isolated (extend internal/cli/approval_test.go): a committed .noon.yaml with a scheduled job in an enabled project is not loaded (job absent; scheduler cannot run it) and the same job in noon.yaml reports needs_approval; the job in .git/noon.yaml reports not_required, loads from a linked worktree, keeps override semantics ({enabled: false}, whole-job replacement). Command: task cli:test -- -run 'Approval' -count=1 -> exit 0
- [x] #2 config package: precedence user < noon.yaml < <common dir>/noon.yaml, linked-worktree commondir resolution, malformed .git file yields an error naming it, no .git means no private layer, .noon.yaml no longer marks a project. Command: task config:test -- -count=1 -> exit 0 (or go test ./internal/config)
- [x] #3 Built binary in an isolated temp Git repo: bin/noon init --local creates .git/noon.yaml mode 0600; bin/noon add x --sh true writes it; bin/noon config lists it as a source; outside Git init --local and add without --shared exit nonzero with guidance.
- [x] #4 README.md, docs/configuration.md, docs/design.md security model table, docs/cli.md, examples state that privacy comes from .git/noon.yaml (not committable) and that .noon.yaml is ignored. task ci -> exit 0; commands and results recorded in Implementation Notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Approach decision (with user): moved the private layer to <git common dir>/noon.yaml instead of classifying .noon.yaml by git ls-files. Tracking detection was rejected: git checkout writes worktree files before the new index, so serve's 2s reload can see a PR's .noon.yaml while the index still says untracked; Git also overwrites an ignored .noon.yaml on checkout of a branch that tracks it. Same fix as tell TELL-15/TELL-24. User chose: in-tree .noon.yaml ignored, no backward compatibility or migration warning. add/remove/import --write/init --local edit .git/noon.yaml and refuse outside Git (no .git) with a --shared hint. No git process runs, so the original fail-closed git-error matrix no longer applies; a malformed .git file is a config error (covered in config tests).

AC#1: task cli:test -- -run 'Approval' -count=1 -> exit 0 (TestEnableApproval: shared needs_approval, .git/noon.yaml not_required, {enabled: false} and whole-job replacement). Regression TestCommittedDotNoonYAMLNeverRunsUnattended in internal/cli/manage_test.go (deviation from approval_test.go: reuses the injected-tick scheduler fixture there): real git init, HOME/XDG/NOON_* isolated, git add -f + commit .noon.yaml with a scheduled job, project enabled -> list --json has no jobs and two scheduler ticks do not run it; same manifest in .git/noon.yaml -> not_required and scheduler runs it. task cli:test -- -run 'Approval|CommittedDotNoon' -count=1 -v -> both PASS. Mutation check: temporarily loading project/.noon.yaml as private made the regression fail (approval not_required, source .noon.yaml). Linked-worktree loading is covered in internal/config (AC#2).

AC#2: go test ./internal/config -count=1 -> ok. TestPrivateConfigLivesInCommonGitDirectory covers main checkout and real-layout linked worktree (.git file + commondir) both loading main/.git/noon.yaml, in-tree .noon.yaml ignored with and without Git, malformed .git error naming the file; TestLoadLayersAndPathOrigins covers user < noon.yaml < .git/noon.yaml; TestLoadDiscoveryStopsAtGitBoundary now uses noon.yaml as the nested marker.

AC#3: task cli:build -> exit 0. Isolated env -i HOME/XDG fixture /var/folders/jp/1mwx72h172955139pth4h8800000gn/T/noon46.G2ByNBsySs: init --local -> Created repo/.git/noon.yaml, stat 600; add x --sh true -> 'Added x to .git/noon.yaml (private)'; noon config Files lists only repo/.git/noon.yaml; list --json x source .git/noon.yaml shared=false approval=not_required; a staged (git add -f) .noon.yaml job is not listed; in plain dir init --local and add exit 2 with '--shared' hint, directory left empty.

AC#4: README, docs/configuration.md (new Private config section), docs/design.md (approval rules + security model table rows for .git/noon.yaml and in-tree .noon.yaml), docs/cli.md, docs/crontab.md, examples updated. task ci -> exit 0 on HEAD d41228e containing implementation commit 9fb3ade (security, vet, staticcheck, tests, race, smoke). Before that, govulncheck failed environmentally (binary built with go1.26.6 vs pinned go1.27.2); reinstalled with mise install -f go:golang.org/x/vuln/cmd/govulncheck@1.9.0, no repo change.

Contract deviations: internal/cli/{history_test,run_test}.go use a new projectFiles helper (sandbox now has .git, and no-write checks must also see .git/noon.yaml); internal/{scheduler/queue_test,scheduler/serve_test,cronimport/cronimport_test,tui/tui_test}.go fixtures create .git; examples/basics/local-overrides/dot-noon.yaml renamed to private.yaml; repo noon.yaml comment updated. Removed tests only for removed behavior (NOON-43 .gitignore protection in init --local and add's ignore reminder; doctor local_config_ignored check); replaced with refusal-outside-Git checks. No test skipped or weakened. Concurrent agent commits (3869207 deps, d41228e README) landed in this checkout during the work; their files were not staged by this task. No push.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Private project config now lives in <git common dir>/noon.yaml, which Git cannot commit or check out, so a PR can no longer supply approval-free jobs. In-tree .noon.yaml is ignored. Writers target .git/noon.yaml and refuse outside Git. Docs and security model updated. Implemented in 9fb3ade; task ci passes.
<!-- SECTION:FINAL_SUMMARY:END -->
