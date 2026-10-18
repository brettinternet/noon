# Shared jobs, local overrides, and user config

**Level:** beginner · **Shows:** configuration layers, toggles, whole-job replacement, private jobs, `default_agent`

Layers, lowest to highest: defaults → user config → `noon.yaml` → `.git/noon.yaml` → `NOON_*`
environment → CLI flags.

| File | Committed | Holds |
| --- | --- | --- |
| [`user-config.yaml`](user-config.yaml) → `~/.config/noon/config.yaml` | no | timezone, agent profiles, hook defaults, `state_dir` |
| [`noon.yaml`](noon.yaml) | yes | team jobs; need `noon enable` approval before they run unattended |
| [`private.yaml`](private.yaml) → `.git/noon.yaml` | **can't be** | private jobs, toggles, replacements, `default_agent` |

The private file lives in the Git directory because Git can't commit or check out paths there.
Try it in a scratch checkout:

```sh
cp -R examples/basics/local-overrides /tmp/local-overrides && cd /tmp/local-overrides
git init -q
noon list                       # shared jobs only
cp private.yaml .git/noon.yaml && chmod 600 .git/noon.yaml
noon list                       # test replaced, lint disabled, notes-sync added
noon list --json | jq '.jobs[] | {name, source, overrides, approval}'
noon config                     # effective settings and which files were loaded
```

`noon init --local` creates an empty `.git/noon.yaml` with the right mode. `noon add NAME ...`
writes to `.git/noon.yaml` by default; `--shared` writes to `noon.yaml`.

Private jobs skip approval, which is why only an uncommittable file can hold them. Noon ignores
an in-tree `.noon.yaml`.

## Share jobs across machines

The manual `mac-info`, `linux-info`, and `arm-info` jobs are harmless platform examples:

```sh
noon list
noon list --json | jq '.jobs[] | {name, os, arch, platform_supported}'
noon run mac-info      # succeeds on a darwin Noon binary; rejects on Linux
noon run linux-info    # succeeds on a linux Noon binary; rejects on macOS
noon run arm-info      # succeeds only with an arm64 Noon binary
```

A nonmatching job stays visible as **other platform**. Trying to run or queue it exits 1 with
`unsupported_platform`; approval does not bypass that constraint. It is not scheduled, and
`doctor` skips its command/cwd checks. Matching uses the **Noon binary's** GOOS/GOARCH, which may
differ from physical hardware under emulation. Use `darwin`, not `macos`, and `amd64`, not
`x86_64`. If both `os` and `arch` are specified, both must match.

These shared jobs need no private override just because a teammate uses another platform.
A private replacement still replaces the whole definition, including its constraints.
