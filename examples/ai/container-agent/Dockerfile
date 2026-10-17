# The agent's sandbox. Add your project's toolchain so CHECK_CMD can run here too.
# docker build -t noon-codex-agent .
FROM node:22-bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git make \
  && rm -rf /var/lib/apt/lists/*
RUN npm install -g @openai/codex@0.163.0
