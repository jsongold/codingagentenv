# cad + orchd + Claude Code. Build: docker build -t cad-local .   Runbook: deploy/gcp/README.md
# Runtime HOME is /data (the mounted volume) so ~/.aienv/.store/<id> (claude/codex/opencode stores)
# and ~/.claude* survive restarts. Claude Code itself is installed under /home/cad (in the image),
# because a volume on /data would hide anything installed there at build time.
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY cad/ cad/
COPY orchd/ orchd/
RUN cd cad && CGO_ENABLED=0 go build -trimpath -o /out/cad . \
 && cd ../orchd && CGO_ENABLED=0 go build -trimpath -o /out/orchd .

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git jq \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home --home-dir /home/cad --shell /bin/bash cad \
 && mkdir -p /data && chown cad:cad /data
USER cad
# Official installer (https://claude.ai/install.sh) -> /home/cad/.local/bin/claude.
RUN curl -fsSL https://claude.ai/install.sh | bash && /home/cad/.local/bin/claude --version
USER root
COPY --from=build /out/cad /app/cad/bin/cad
COPY --from=build /out/orchd /app/orchd/bin/orchd
COPY cad/config.json /app/cad/config.json
COPY orchd/policy.json /app/orchd/policy.json
COPY deploy/entrypoint.sh /app/entrypoint.sh
# namespaces.json lives on the volume: <app>/config -> /data/cad/config.
RUN ln -s /data/cad/config /app/cad/config
USER cad
# The image is rebuilt on every push to main, so the in-image claude is refreshed there;
# the auto-updater would otherwise write into $HOME=/data.
ENV HOME=/data \
    PATH=/app/cad/bin:/app/orchd/bin:/home/cad/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    CAD_HOME=/app/cad \
    ORCHD_HOME=/app/orchd \
    ORCHD_STATE_DIR=/data/orchd/state \
    CAD_STATE_DIR=/data/cad/state \
    CAD_CLAUDE_BIN=/home/cad/.local/bin/claude \
    DISABLE_AUTOUPDATER=1
WORKDIR /data
VOLUME /data
ENTRYPOINT ["/app/entrypoint.sh"]
# Swarm gates updates on this: a task counts as running only once healthy, and update-failure-action=rollback
# reverts an image whose task never gets healthy (deploy/gcp/startup.sh). start-period covers cad's first
# usage collection. https://docs.docker.com/reference/dockerfile/#healthcheck
HEALTHCHECK --interval=15s --timeout=3s --start-period=90s --retries=3 \
  CMD curl -fsS -o /dev/null 'http://127.0.0.1:7878/healthz?ready' || exit 1
# cad listens on 127.0.0.1:7878 (its default); run with --network host so the host's loopback is cad's.
CMD ["cad"]
