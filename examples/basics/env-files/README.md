# Env files

**Level:** beginner · **Shows:** config-time `vars_file` vs run-time `env_file`, and env precedence

| File | Read | Fills | Exported to the job |
| --- | --- | --- | --- |
| `vars.env` (`vars_file`) | every config load | `${...}` in `schedule`, `cwd`, paths, durations | no |
| `runtime.env` (`env_file`) | every run start | the job's environment | yes |

```console
$ noon -C examples/basics/env-files list
JOB    SCHEDULE           NEXT            LAST   COMMAND
greet  weekdays at 09:00  needs approval  never  echo "$GREETING, $TARGET (GR...

$ noon -C examples/basics/env-files run greet
▶ greet · run 1 · echo "$GREETING, $TARGET (GREET_SCHEDULE=${GREET_SCHEDULE:-not exported})"
hello, noon (GREET_SCHEDULE=not exported)
✓ greet succeeded in 5ms
```

`vars.env` turned the schedule into `weekdays at 09:00`; delete it and the `:-every 1h` default
applies. `run` is never interpolated, so the shell sees `${GREET_SCHEDULE...}` and the variable is
unset. `TARGET` comes from `env:`, which beats `runtime.env`.

Both files are committed here because they hold sample values. In a real project, gitignore them
and never put secrets in a `vars_file`: its values show up in `noon list`.
