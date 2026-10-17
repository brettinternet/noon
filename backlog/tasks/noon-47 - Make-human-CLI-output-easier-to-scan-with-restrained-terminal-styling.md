---
id: NOON-47
title: Make human CLI output easier to scan with restrained terminal styling
status: In Progress
assignee:
  - '@pi'
created_date: '2026-10-17 05:54'
updated_date: '2026-10-17 06:05'
labels: []
dependencies: []
priority: medium
type: enhancement
ordinal: 14400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human dashboard, list, history, doctor, and foreground-run output currently give identifiers, metadata, and urgent states nearly equal visual weight. Add hierarchy and semantic color without changing layouts or machine contracts. Scope: bold project/job names, headers and next commands; red failures, yellow attention states, cyan running, green success marks; default foreground for metadata. Automatic per-stream terminal detection, NO_COLOR and TERM=dumb, explicit auto/always/never override. Non-goals: TUI redesign, themes, RGB palette, animations, styling job output or logs, business-logic changes. Dependencies: none (existing CLI rendering and isolation contracts remain authoritative). Modified-file contract: internal/cli/{style,root,dashboard,jobs,history,run,shell}.go and relevant existing tests, docs/cli.md and README.md; .github/workflows/release.yml only to align its stale version-output assertion with the existing version command. Delivery: commit, push main, publish next feature release through the tagged workflow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 task cli:test PKG=./internal/cli passes regression checks: styling on status/list/history/doctor/run and errors, explicit overrides and invalid values, clean JSON and redirected output, sanitized and aligned tables, unchanged child output and retained logs.
- [x] #2 task cli:build and isolated direct executions demonstrate automatic per-stream TTY behavior, NO_COLOR and TERM=dumb suppression, explicit always/never, readable plain fallback and the documented palette; inspect representative output on light and dark backgrounds.
- [x] #3 task ci passes; docs describe supported surfaces, palette, precedence, machine-output and raw-log guarantees; release-version smoke assertions match the built version output.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
- [x] #6 Independent review checks ANSI injection, column alignment, stream detection, JSON/raw-output boundaries, and flag parsing; concrete defects resolved.
- [ ] #7 Changes committed and pushed; next feature release published by the existing workflow, with immutable assets and successful release checks.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a small per-stream styling policy and semantic table cells; sanitize and measure text before adding trusted ANSI sequences. Preserve existing layouts and raw writers. 2. Apply hierarchy and semantic states to status/list/history/doctor/run plus common errors; add color flag completion and document precedence. 3. Add focused public CLI regressions for machine/raw-output boundaries, ANSI injection/alignment and flag parsing, then exercise real PTYs and visual samples. 4. Independent risk-focused review, full CI, staged checks, commit and push main; publish v0.3.0 through the existing release workflow and record delivery evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Existing decisions and completed NOON-14/44 preserve plain/JSON contracts but do not prohibit new opt-in terminal styling. No new module dependencies planned; reuse terminalWriter. Found release.yml still expects "noon version VERSION" although current version output and release-smoke.sh use "noon VERSION"; align the workflow assertion as required for this requested release. Work stays in current clean main checkout.

AC#1: task cli:test PKG=./internal/cli and focused TestColorOutputBoundaries/TestListHumanIdentifiers/TestRunColorPreservesChildOutput/TestValueFlagsAreNotMistakenForJSON -> PASS. Forced styling preserves visible table content/alignment and escaped controls; JSON, child output and retained logs retain their contracts. LSP diagnostics clean for affected Go files. AC#2: task cli:build -> exit 0; bun /tmp/noon-47-check.Okax1z/check.ts -> all 16 real-PTY/redirected assertions PASS (auto per stream, NO_COLOR=0, TERM=dumb, always/never, parse-error flag order, Unicode guidance, JSON and raw logs). Captured actual output rendered with representative light/dark ANSI palettes in Chrome and screenshot inspected: headings/statuses readable, columns aligned, no dim metadata. Visual check kept timestamps/durations in default foreground. Artifacts retained in /tmp/noon-47-check.Okax1z; browser preview page subsequently disappeared. AC#3: task ci -> exit 0 after final source changes (security, vet/staticcheck, all tests, race, smoke). README/docs/help document only implemented behavior and output precedence. Release workflow stale version assertion aligned with existing noon VERSION output. No design contract conflict or new dependency. Independent reviewer found parse-error color ordering and escaped Unicode hint paths; both fixed, regression-tested and rechecked via PTY. Final reviewer PASS: /tmp/noon-47-review.md. No tests removed, skipped or weakened; all files within contract. Remaining delivery: commit, verify on commit, push main and publish v0.3.0; no blocker.
<!-- SECTION:NOTES:END -->
