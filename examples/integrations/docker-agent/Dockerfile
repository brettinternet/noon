# Noon plus the tools agent jobs need. Build noon:dev first (task docker:build in the Noon repo).
FROM noon:dev
USER root
RUN apk add --no-cache git github-cli jq curl nodejs npm \
    && npm install -g @anthropic-ai/claude-code \
    && git config --system --add safe.directory /workspace
USER noon
