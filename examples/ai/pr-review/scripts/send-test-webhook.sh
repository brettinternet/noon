#!/bin/sh
# send-test-webhook.sh URL EVENT FIXTURE: sign FIXTURE like GitHub and POST it to a local Noon listener.
#   scripts/send-test-webhook.sh http://127.0.0.1:8787/hooks/1/pr-review pull_request fixtures/pull_request.json
set -eu
url=$1 kind=$2 fixture=$3
: "${NOON_GITHUB_WEBHOOK_SECRET:?export the same secret the scheduler has}"
signature=$(openssl dgst -sha256 -hmac "$NOON_GITHUB_WEBHOOK_SECRET" -hex < "$fixture" | awk '{print $NF}')
curl --fail-with-body -sS \
  -H 'Content-Type: application/json' \
  -H "X-GitHub-Event: $kind" \
  -H "X-GitHub-Delivery: local-$(date +%s)" \
  -H "X-Hub-Signature-256: sha256=$signature" \
  --data-binary "@$fixture" \
  "$url"
echo
