---
id: NOON-85
title: Support declared inputs for explicit on-demand job invocations
status: draft
type: feature
priority: medium
labels: [config, cli, security]
rank: af11dc47711dc4722
created: 2026-10-22T17:27:00Z
updated: 2026-10-23T04:34:00Z
---

Outcome: let a caller supply a small, explicitly declared set of per-run inputs to an existing job, including noninteractive one-off automation, without turning Noon into a general task runner or allowing arbitrary command overrides.

Context
Today, noon run review --data '{"pr":123}' writes a payload to $NOON_EVENT_FILE. Data never becomes part of a command. Raw trailing argv would not solve jobs shaped like [agent-job, launch, review, PROMPT, GATE]: new arguments would land after GATE rather than inside PROMPT. A declared PR number should reach an author-selected location while the executable, argument structure, and gate remain fixed.

Decision-2 pins approved definitions, not every execution outcome: scripts and event data already influence behavior, and foreground runs need no approval. Nevertheless, exposing arbitrary runtime flags through queued runs, MCP, HTTP, or webhooks would widen the existing invocation surface. This proposal instead makes permitted variability part of the approved definition.

Proposed contract (not implemented)
- Jobs declare ordered, named string inputs with validation, such as pr constrained by ^[1-9][0-9]*$. Callers cannot supply undeclared inputs.
- The first candidate CLI syntax is noon run review -- 123, mapping values to declarations in order. noon run review --detach -- 123 queues the same explicit invocation. A separator is mandatory for positional inputs; multi-job selection must remain unambiguous.
- An explicit binding selects where a value goes. Illustrative YAML only:

  review:
    inputs:
      - name: pr
        pattern: '^[1-9][0-9]*$'
    run:
      - agent-job
      - launch
      - review
      - "/skill:pr-review pr:{inputs.pr}"
      - "<gate>"

- Bind only inside approved argv elements, not argv[0], the shell command string, or arbitrary configuration fields. Substitution preserves element boundaries, is single-pass, and performs no shell evaluation or word splitting. Native prompt-job support is a separate scope decision; the example above is an ordinary argv job with prompt text in one argument.
- Input declarations, constraints, defaults if supported, and bindings participate in approval. Jobs without inputs retain their existing digest and behavior. A changed declaration or binding requires shared-job reapproval.
- CLI and automation resolve inputs identically. Missing required, excess positional, unknown named, duplicate, or invalid values fail before creating a runnable request. Never prompt, infer a target, or broaden a missing PR into "all PRs."
- A job without a schedule may require inputs and be invoked by a person, CI, MCP, or HTTP. Existing enablement and approval rules still apply to anything queued.
- There is no built-in default action. If declared defaults ship, omitted values use only those validated defaults. Recurring schedules, including temporary schedules, must not be accepted when required inputs cannot be resolved. Smallest initial slice: required inputs for on-demand jobs, with defaults deferred.
- Persist the validated input set with queued work and retain it for inspection. Distinct input sets must not silently coalesce, be dropped by cooldown/merge behavior, or be replaced by later requests. Existing idempotency behavior needs an explicit compatibility rule.
- Keep --data and raw webhook payloads as event data, never implicitly bound into commands. Supporting adapter inputs requires an explicit structured field or mapping and the same validation; unsupported surfaces must fail clearly rather than bypass validation.
- Inputs and rendered arguments are inspectable, not a secret channel. Regex constraints and fixed argv structure do not sandbox the invoked program or prevent prompt injection from permitted free text.

Non-goals
Task dependencies or a task graph; arbitrary executable/command/agent-flag overrides; automatic payload-to-command interpolation; a general template/expression language; an interactive input wizard; shell-string templating; secret transport; changing no-argument inspection into execution.

Promotion condition and open decisions
Keep this item a draft until a bounded first delivery and its contract are settled. Record the deliberate exception to docs/design.md's "payloads are files, never interpolated" and docs/configuration.md's literal run/prompt behavior in a new decision before implementation; do not reinterpret decision-2 silently.
1. Confirm direct declared binding versus validation plus $NOON_EVENT_FILE consumption by a wrapper. File-only delivery preserves today's contract but does not itself insert a PR into the fixed prompt argument.
2. Choose positional-only first delivery versus named syntax (candidate: noon run review pr=123); define missing/default semantics, full-value regex validation, and bounded input sizes.
3. Choose adapter rollout: CLI foreground/detached first versus structured inputs on MCP/HTTP too. Specify webhook mapping or explicitly defer it; decide how TUI and temporary schedules handle required inputs.
4. Set parameterized-request merge, cooldown, idempotency, and persisted-definition-change semantics so a request never executes with a silently different input set or newly unapproved binding.

Provisional modified-file contract
internal/config and noon.schema.json for declarations/validation/digests; internal/app for shared invocation rules; internal/cli for parsing and rendering; internal/runner only for argv materialization if needed; internal/store and internal/scheduler for persisted requests and dispatch; internal/mcp, internal/httpapi, and internal/tui only as required by the chosen adapter scope or explicit rejection behavior; focused tests in the affected packages and integration; docs/{configuration.md,cli.md,design.md,agents.md,triggers.md,webhooks.md}, README.md and one runnable example as applicable; a new backlog decision. Narrow this contract before promotion.

Dependencies: no unfinished prerequisite task identified. Coordinate with DRAFT-18 if user-config job templates ship first; parameterized template behavior is not implicitly included here.

Provisional verification before promotion
Finalize executable acceptance fixtures once binding syntax and adapter scope are settled. Use temporary HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR for every direct CLI check; never invoke a real agent or network.
- mise exec -- task cli:test PKG="./internal/config ./internal/app ./internal/cli ./internal/store ./internal/scheduler ./internal/runner" -- -count=1 -> exit 0; extend package selection for adapters included in the agreed scope.
- mise exec -- task cli:build && mise exec -- task ci -> exit 0.
Critical regression risks: an input becomes a new flag/shell expression, a missing PR broadens the action, a queued request loses or changes its inputs, a changed binding bypasses approval, or a job without inputs unexpectedly loses existing approval. Implement first, then add the fewest public-interface tests covering these risks.

## Acceptance criteria

- [ ] A recorded decision settles binding, first adapter scope, defaults, recurring schedules, and queue identity before promotion; documentation identifies declared inputs as an explicit exception rather than claiming arbitrary event payload interpolation is safe.
- [ ] An isolated fake-executable fixture demonstrates that supplied PR 123 reaches only its declared location in the prompt argument, the gate and argv boundaries remain unchanged, and missing/invalid/undeclared inputs create neither a process nor runnable queued work. Noninteractive invocation never prompts or infers a broader action.
- [ ] Foreground and detached invocations use the same validation. Queued inputs survive dispatch and remain inspectable; distinct inputs cannot silently merge or disappear. Every included adapter follows the same rules and unsupported input paths fail clearly. Scheduled invocations have resolvable declared values or are rejected.
- [ ] Changing an input declaration or binding invalidates approval for a shared job; an unchanged approved job without inputs remains approved. Event payloads are never implicitly interpolated, and callers cannot override the executable, gate, or arbitrary arguments.
- [ ] mise exec -- task cli:test PKG="./internal/config ./internal/app ./internal/cli ./internal/store ./internal/scheduler ./internal/runner" -- -count=1 and mise exec -- task cli:build && mise exec -- task ci exit 0; include affected adapter packages and record exact isolated fixture commands and expected results before promotion.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Refinement (2026-10-22 review): intended use is one-off runs with inputs, not a task runner. Recommended first slice:

- Delivery: export each validated input as an environment variable (e.g. NOON_INPUT_PR) instead of binding into argv. The motivating case becomes run: [sh, -c, 'agent-job launch review "/skill:pr-review pr:$NOON_INPUT_PR" GATE']; pattern validation plus shell quoting keeps the value one argument. This needs no exception to "payloads are files, never interpolated", so no new decision unless argv binding is added later.
- Avoid a second interpolation syntax: {inputs.pr} would sit beside the load-time vars_file ${...} grammar, which deliberately leaves run literal.
- Named syntax, not positional: noon run review --input pr=123 maps directly to MCP/HTTP inputs: {"pr": "123"} and cannot be confused with multi-job selection (noon run A B).
- Required inputs only; no defaults. Defaults only matter when one job serves both scheduled and on-demand use, and they silently fill a missing value (the "missing PR becomes all PRs" risk). Use two jobs if both uses are ever needed.
- Schedules: a job with required inputs has no caller when a schedule fires, so reject schedule and temporary schedules on such jobs at load/request time. This is a consequence of required inputs, not a separate policy.
- Queue identity: inputs are part of the merge/cooldown identity. Identical inputs coalesce; different inputs never do.
- Adapters: CLI (foreground and --detach) and MCP first; HTTP and webhooks reject inputs clearly until mapped.
