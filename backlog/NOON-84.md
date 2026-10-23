---
id: NOON-84
title: Reuse user-config job templates from private project config
status: draft
type: feature
priority: low
rank: aee23b88ee23b88a0
created: 2026-10-20T20:42:00Z
updated: 2026-10-20T20:42:00Z
---

Outcome: one job set defined once in user config can run in many repositories without copying or symlinking .git/noon.yaml, and without weakening the approval model.

Promotion condition: draft until the open questions below are settled and recorded as a decision in backlog/decisions/ (this is a deliberate exception to "cross-file references and includes are unsupported" in docs/configuration.md#yaml-composition).

Context: user config jobs: belong only to the separate user project and never reach repository projects. YAML anchors and merges work only within one file. Reusing a job set across repositories today means copying it into each .git/noon.yaml or symlinking the whole file, which cannot be mixed with repository-specific jobs and drifts when copied. Decision-2 already treats user config and .git/noon.yaml as the user's own, so jobs from a user template referenced by private config need no approval.

Proposed behavior:
- User config gains a top-level templates: mapping. Each template is a named set of jobs: templates.NAME.jobs.JOB uses the existing job schema. Template names follow job-name rules. templates: is separate from user jobs:, which keep meaning the user project.
- .git/noon.yaml gains use: [NAME, ...]. Each named template's jobs join the project as private jobs (shared: false, no approval). An unknown template name is a config error naming the user config file.
- use: is rejected in shared noon.yaml (a committed file must not opt a repository into a user's possibly privileged agent jobs by guessing a name), in --config/NOON_CONFIG files (CI and container runs must not depend on machine user config), and in user config itself (no nesting).
- Template job name collisions with jobs in .git/noon.yaml, noon.yaml, or another used template are errors, except the existing local {enabled: false} toggle, which disables a template job without copying it. To vary a job per repository, copy it into .git/noon.yaml under a different name.
- Template jobs run with the consuming project as root and default cwd. Editing a template changes every repository that uses it at the next config reload; noon serve must notice user config changes for repository projects.
- noon list and list --json report source as the user config file plus the template name; config shows used templates.
- Optional: noon use NAME... writes use: to .git/noon.yaml and enables the project, removing the remaining per-repository edit.

Non-goals: parameters, variables, or interpolation in templates (scripts can derive the repository from cwd or git remote); field-level merging of template jobs with local jobs; applying templates automatically by path pattern or to every enabled project; file includes or arbitrary cross-file YAML references; templates in shared noon.yaml or the JSON schema.

Open questions to settle before ready:
- Relative paths. Template jobs run in the consuming repository, but their scripts and prompts naturally live beside the user config. Proposal: prompt_file resolves against the user config directory; cwd defaults to the project root; run commands either require absolute paths or receive a reserved environment variable such as NOON_USER_DIR (YAML never expands ~). Pick one.
- Whether noon use NAME ships with this task or separately.
- How history and approval state behave when a template job is removed from a template or a project stops using it (expected: same as removing a private job; history stays).

Modified-file contract (provisional): internal/config/{config.go,jobs.go,yaml.go,edit.go} and tests; internal/app and internal/cli for list/config source reporting and optional noon use; internal/scheduler if reload needs user-config change detection; README.md, docs/{configuration.md,design.md,cli.md}, internal/skill/SKILL.md; a new backlog decision.

Dependencies: none.

## Acceptance criteria

- [ ] mise exec -- task cli:test PKG="./internal/config ./internal/app ./internal/cli" -- -count=1 -> exit 0. Tests cover: use: in .git/noon.yaml adds template jobs as private jobs needing no approval; use: rejected in noon.yaml, --config, and user config with an error naming the file; unknown template name errors; name collisions error except the local {enabled: false} toggle; list --json source names the user config file and template.
- [ ] mise exec -- task cli:build && mise exec -- task ci -> exit 0. With isolated HOME/XDG config/state, define a template in user config, reference it from two repositories, run noon list and noon run JOB in each, edit the template while noon serve runs, and confirm both projects pick up the change; record exact commands and output in task notes.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md and a backlog decision record the settled template contract
