# Failure notifications with Tell

**Level:** beginner · **Shows:** inline `on_failure` string hooks, an explicit Tell config beside `noon.yaml`, failing fast on missing scheduler environment

[Tell](https://github.com/brettinternet/tell) sends one notification and exits. Noon owns the
schedule and run history; the hook only points you at `noon logs`.

```sh
cp examples/integrations/tell/noon.yaml examples/integrations/tell/noon-tell.yaml ~/.dotfiles/
cd ~/.dotfiles
export APPRISE_URLS='ntfys://ntfy.sh/your-long-random-topic'   # in the scheduler's environment
tell --config noon-tell.yaml doctor     # checks Apprise matches a service; sends nothing
noon enable
noon run darwin                         # hooks run for foreground runs too
noon logs darwin --section hook
```

| File | Owns |
| --- | --- |
| [`noon.yaml`](noon.yaml) | jobs, and an `on_failure` hook per job that calls `tell notify` |
| [`noon-tell.yaml`](noon-tell.yaml) | Tell routing for those hooks: `notify.via: apprise` |

How the hook works:

- It is a string, so Noon runs it with `/bin/sh -c` in the job's directory. The shell expands
  `NOON_STATUS` and `NOON_RUN_ID`; the message is a pointer to the run, never log content.
- `: "${APPRISE_URLS:?…}"` fails the hook with a clear line in `noon logs RUN --section hook`
  when the variable is missing, instead of Apprise silently matching nothing.
- `--config noon-tell.yaml` loads only that file. Don't name it `tell.yaml`: Tell auto-discovers
  `tell.yaml` as shared config and rejects routing (`notify.via`) there.
- `--timeout 10s` keeps delivery inside Noon's 30-second hook budget. A failed notification never
  changes the job's result and is not retried.

`APPRISE_URLS` holds credentials, so keep it out of YAML: export it in your shell for `noon serve`,
or in the service environment for `noon service install`. That environment also needs `tell` and
`apprise` on `PATH`. For desktop banners instead, set `notify.via: desktop` and drop the
`APPRISE_URLS` guard.

To notify on first failure and recovery instead of every failure, use Tell's
[Noon example](https://github.com/brettinternet/tell/tree/main/examples/noon) hook, or the
[notifications](../notifications/) wrappers.
