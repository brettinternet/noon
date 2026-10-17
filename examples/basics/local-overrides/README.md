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
