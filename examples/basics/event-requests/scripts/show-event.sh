#!/bin/sh
set -eu
# Data is parsed, never evaluated as a command.
jq -c '.data' "${NOON_EVENT_FILE:?Queue this job with --detach --data @fixtures/first.json}"
