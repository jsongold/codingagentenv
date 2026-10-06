# Worker image: same image serves the local devcontainer and cloud workers
# (#95). Stage 1 builds cad (Go, static). Stage 2 is the runtime: git,
# gh, jq, node, Claude Code CLI, cad, and agent/bootstrap.sh. jq is required
# by this repo's own harness (bin/codingenv install, hooks) when this image
# doubles as the local devcontainer, and by bootstrap.sh's JSON encoding.

FROM golang:1.27.1-bookworm AS cad-build
WORKDIR /src/cad
COPY apps/cad/go.mod ./
COPY apps/cad/*.go ./
RUN CGO_ENABLED=0 go build -o /out/cad .

FROM node:24-bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git gnupg jq \
    && install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code && npm cache clean --force

COPY --from=cad-build /out/cad /usr/local/bin/cad
COPY agent/bootstrap.sh /usr/local/bin/bootstrap
RUN chmod +x /usr/local/bin/cad /usr/local/bin/bootstrap

RUN useradd --create-home --shell /bin/bash agent
USER agent
WORKDIR /home/agent

# No ENTRYPOINT/CMD: dev-dispatch (or a test) invokes `bootstrap [agent-cmd...]`
# explicitly, since bootstrap is on PATH at /usr/local/bin/bootstrap.
