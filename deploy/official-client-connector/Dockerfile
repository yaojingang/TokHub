FROM golang:1.26.5-bookworm AS connector-build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tokhub-client-connector ./cmd/tokhub-client-connector

FROM node:24.6.0-bookworm-slim

ARG TARGETARCH
ARG GROK_VERSION=1.0.5
ARG GROK_SHA256_AMD64=9ba87444e1819e8f6104adbbf4676a870c204380aa5c3e1c38a926c4ea677238
ARG GROK_SHA256_ARM64=1c1fe67d7c35497fb09f44a451f57acc3787add4c9aea2c56f5c7c75dc5ffcf1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tini \
    && mkdir -p /etc/grok \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/official-clients
COPY deploy/official-client-connector/package.json deploy/official-client-connector/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts=false \
    && npm cache clean --force

RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) grok_arch="x86_64"; expected="${GROK_SHA256_AMD64}" ;; \
      arm64) grok_arch="aarch64"; expected="${GROK_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://x.ai/cli/grok-${GROK_VERSION}-linux-${grok_arch}" -o /usr/local/bin/grok; \
    echo "${expected}  /usr/local/bin/grok" | sha256sum -c -; \
    chmod 0755 /usr/local/bin/grok; \
    ln -s /usr/local/bin/grok /usr/local/bin/agent

COPY --from=connector-build /out/tokhub-client-connector /usr/local/bin/tokhub-client-connector
COPY deploy/official-client-connector/grok-requirements.toml /etc/grok/requirements.toml

RUN groupadd --gid 10001 tokhub \
    && useradd --uid 10001 --gid 10001 --create-home --home-dir /home/tokhub --shell /usr/sbin/nologin tokhub \
    && mkdir -p /data/codex /data/grok /data/connector /workspace \
    && ln -s /data/grok /home/tokhub/.grok \
    && chown -R 10001:10001 /data /workspace /home/tokhub \
    && chmod 0444 /etc/grok/requirements.toml

ENV HOME=/home/tokhub \
    CODEX_HOME=/data/codex \
    TOKHUB_CLIENT_CONNECTOR_HOME=/data/connector \
    TOKHUB_CONNECTOR_CONTAINER=1 \
    GROK_TELEMETRY_ENABLED=false \
    GROK_FEEDBACK_ENABLED=false \
    PATH=/opt/official-clients/node_modules/.bin:/usr/local/bin:/usr/bin:/bin

USER 10001:10001
WORKDIR /workspace
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/tokhub-client-connector"]
CMD ["run"]
