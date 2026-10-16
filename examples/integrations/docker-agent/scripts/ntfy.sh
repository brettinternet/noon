#!/bin/sh
# Failure push from inside the container; NTFY_TOPIC comes from .env.
[ -n "${NTFY_TOPIC:-}" ] || exit 0
exec curl --fail --silent --show-error -H "Title: $NOON_JOB $NOON_STATUS" \
  --data-binary "run $NOON_RUN_ID exited $NOON_EXIT_CODE" "https://ntfy.sh/$NTFY_TOPIC"
