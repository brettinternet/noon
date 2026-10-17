#!/bin/sh
# watch.sh JOB [--detach]
# Re-run a Noon job after Go source changes. watchexec watches and debounces; Noon runs the job
# with its timeout and records history.
#   watch.sh test            runs in this terminal; needs no noon enable and no scheduler
#   watch.sh vet --detach    queues for noon serve; needs noon enable and a running scheduler
set -eu
job=${1:?usage: watch.sh JOB [--detach]}

case ${2:-} in
  "")
    # --on-busy-update queue: a change during a run starts exactly one more run when it ends, so
    # the last edit is never skipped. -n runs noon directly instead of through $SHELL.
    exec watchexec -n --postpone --exts go,mod,sum --debounce 1s --on-busy-update queue \
      -- noon run "$job"
    ;;
  --detach)
    # Each request returns at once. A request made while one is already waiting joins it, so a
    # burst of saves becomes one background run, and a save during a run queues one more.
    exec watchexec -n --postpone --exts go,mod,sum --debounce 1s \
      -- noon run "$job" --detach
    ;;
  *)
    echo "usage: watch.sh JOB [--detach]" >&2
    exit 2
    ;;
esac
