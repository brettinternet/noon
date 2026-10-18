# Changelog

Notable changes to noon are documented here. This file is generated from conventional commits by [git-cliff](https://git-cliff.org/).

## Unreleased

### Added

- Report orphaned runs across inspection surfaces
- Record no-work runs as noop
- **agents:** Add Gemini, OpenCode, and Pi profiles
- Constrain jobs by operating system and architecture
- Support personal jobs with --user
- Add authenticated remote API and HTTP MCP
- Queue each event request independently
- **tui:** Redesign views and move shortcuts to help

### Fixed

- Preserve selectors in orphan notice commands

### Documentation

- **examples:** Add Tell integration
- **examples:** Add file-watch, worktrunk, and container-agent

## [v0.3.0](https://github.com/brettinternet/noon/compare/v0.2.0...v0.3.0) - 2026-10-17

### Added

- **cli:** Add restrained terminal styling
- Publish noon.yaml JSON schema

### Fixed

- **cli:** Simplify version output and add version command

### Documentation

- Add schema headers to noon examples

## [v0.2.0](https://github.com/brettinternet/noon/compare/v0.1.0...v0.2.0) - 2026-10-17

### Added

- **shell:** Complete option values and paths
- **cli:** Add consistent short flags

### Fixed

- **task:** Keep full test gate independent of ambient PKG; mark NOON-45 reviewed
- **shell:** Limit Bash filename handling to path values
- **cli:** Keep run IDs intact in bounded history tables
- **cli:** Integrate full-width job inspection
- **cli:** Preserve job names and add wide listings
- **config:** Load private config from .git/noon.yaml
- **cli:** Report missing remove target as not_found
- **examples:** Bound verify-gate setup errors; slim docker-agent image
- **cli:** Protect local init manifest from Git
- Isolate mise installation checks from host configuration
- Finalize published release installation guidance
- **cli:** Preserve invocation context in guidance
- **cli:** Make usage guidance command-specific and JSON-safe
- **cli:** Clarify no-target guidance and removal feedback

### Documentation

- Complete NOON-45 verification record

## [v0.1.0](https://github.com/brettinternet/noon/commits/v0.1.0) - 2026-10-17

### Added

- Add validated release packaging and publication
- **hooks:** Expose previous job status
- Integrate shell setup
- Add shell entry hooks and completion
- Add terminal job and run dashboard
- Add shell-only agent skill and log guide
- Separate job and hook log sections
- Filter run history and logs by status
- **logs:** Add bounded searchable cursor reads
- Expose run log paths and request context
- Accept signed webhooks
- Import crontabs with faithful defaults
- Verify production container scheduling
- Add job trigger payloads and safeguards
- Run lifecycle hooks after jobs finish
- **mcp:** Serve bounded job tools over stdio
- Add project dashboard and setup diagnostics
- Manage job definitions and scheduling state
- Queue and cancel durable runs
- **service:** Install per-user scheduler services
- Serve schedules for enabled projects
- **scheduler:** Plan due runs and dispatch
- Enable projects and approve shared jobs
- Inspect run history and logs
- Persist foreground run history and logs
- Run agent prompt jobs through profiles
- Run named jobs in the foreground
- Validate and list project jobs
- **schedule:** Parse schedules and compute occurrences
- **cli:** Standardize errors and exit codes
- Bootstrap repo-local CLI and configuration

### Fixed

- **examples:** Verify Docker agent execution and persistence
- **examples:** Verify vendor contracts and restrict review tools
- **examples:** Notify once per outage and recovery
- **examples:** Make database backups and restore drills safe
- Honor cancelled log requests after terminal transitions
- Follow queued runs and detect orphaned owners
- Skip blank MCP lines, keep user hook defaults out of digests, document git safe.directory
- Quote reserved YAML job values, isolate webhook depth, cache inactive webhook configs
- **cli:** Let job names h and help reach subcommands
- Ignore PATH in service drift and skip disabled jobs in doctor
- Preserve queued work on invalid config and reset state on add re-enable
- **cli:** Render em dash in history table
- Preserve config inheritance and JSON errors

### Documentation

- Rewrite README and docs to be concise and example-led
- Plan examples follow-ups
- Plan previous status for lifecycle hooks
- Reconcile event-driven examples
- Add examples library
- Record NOON-30 implementation review
- Complete NOON-30
- Record NOON-13 implementation review
- Tighten MCP tool and skill text
- Guide agents through job discovery and completion
- Plan shell integration and automation follow-ups
- Complete NOON-24 validation
- Build TUI task on log access tasks
- Record final webhook validation
- Complete NOON-8
- Plan log access tasks NOON-24 to NOON-29
- Complete NOON-6
- Complete NOON-12
- Complete NOON-7
- Complete NOON-21
- Complete NOON-20
- Complete BUMP-17 verification record
- Complete BUMP-3 verification record
- Settle scheduler design and decisions
