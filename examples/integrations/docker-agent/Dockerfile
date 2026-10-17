# Build noon:dev first (task docker:build in the Noon repo).
FROM noon:dev
USER root
# Native Alpine installation: https://code.claude.com/docs/en/setup
ARG CLAUDE_VERSION=2.1.302
RUN apk add --no-cache bash git github-cli jq curl libgcc libstdc++ ripgrep \
    && curl -fsSL https://claude.ai/install.sh -o /tmp/install-claude.sh \
    && bash /tmp/install-claude.sh "${CLAUDE_VERSION}" \
    && cp -L /root/.local/bin/claude /usr/local/bin/claude \
    && rm -rf /root/.local /root/.claude /root/.claude.json /tmp/install-claude.sh \
    && mkdir -p /reports \
    && chown noon:noon /reports \
    && git config --system --add safe.directory /workspace
ENV USE_BUILTIN_RIPGREP=0 DISABLE_AUTOUPDATER=1
USER noon
RUN claude --version && gh --version && git --version && jq --version
