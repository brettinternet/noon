# Shared jobs, local overrides, and user config

**Level:** beginner · **Shows:** configuration layers, toggles, whole-job replacement, private jobs, `default_agent`

Layers, lowest to highest: defaults → user config → `noon.yaml` → `.noon.yaml` → `NOON_*`
environment → CLI flags.

| File | Committed | Holds |
| --- | --- | --- |
| [`user-config.yaml`](user-config.yaml) → `~/.config/noon/config.yaml` | no | timezone, agent profiles, hook defaults, `state_dir` |
| [`noon.yaml`](noon.yaml) | yes | team jobs; need `noon enable` approval before they run unattended |
| [`dot-noon.yaml`](dot-noon.yaml) → `.noon.yaml` | **never** | private jobs, toggles, replacements, `default_agent` |

```sh
cd examples/basics/local-overrides
noon list                       # shared jobs only
cp dot-noon.yaml .noon.yaml && chmod 600 .noon.yaml
noon list                       # test replaced, lint disabled, notes-sync added
noon list --json | jq '.jobs[] | {name, source, overrides, approval}'
noon config                     # effective settings and which files were loaded
rm .noon.yaml                   # clean up
```

`noon init --local` creates an empty `.noon.yaml` with the right mode. `noon add NAME ...`
writes to `.noon.yaml` by default; `--shared` writes to `noon.yaml`.

Local jobs bypass approval, which is why `.noon.yaml` must stay out of Git.
