# syntax=docker/dockerfile:1
# check=skip=InvalidDefaultArgInFrom
#
# The container that runs the frontend in a browser against the real agentx serve, on a home built
# from a machine preset. Agents drive it with agent-browser for screenshots and recordings. Build
# and run it with apps/desktop/container/browser, which runs it with no network, no host folder and
# no published port. The build context is the repository root.

ARG GO_VERSION
ARG NODE_VERSION
FROM golang:${GO_VERSION}-trixie AS go

FROM node:${NODE_VERSION}-trixie

ARG AGENT_BROWSER_VERSION=0.44.0

# A build behind a TLS-intercepting proxy passes the proxy's CA bundle as the secret `ca`.
RUN --mount=type=secret,id=ca \
    if [ -s /run/secrets/ca ]; then \
      awk '/BEGIN CERTIFICATE/ { n++ } n { print > ("/usr/local/share/ca-certificates/build-" n ".crt") }' /run/secrets/ca \
      && update-ca-certificates; \
    fi \
    && sed -i 's|http://|https://|g' /etc/apt/sources.list.d/debian.sources
ENV NODE_USE_SYSTEM_CA=1

# Fonts for the scripts and emoji the app's own fonts lack, and ffmpeg for recordings.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg fonts-noto-cjk fonts-noto-color-emoji fonts-noto-core \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
    GOTOOLCHAIN=local \
    GOPATH=/opt/go \
    GOFLAGS=-mod=readonly \
    PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright \
    npm_config_cache=/tmp/npm-cache

# The app runs as a user other than root, so that a permission refusal is real.
RUN useradd --create-home --shell /bin/bash dev \
    && install -d -o dev -g dev /src /opt/go /opt/pnpm-store /opt/ms-playwright /srv/remotes \
    && touch /.agentx-container

WORKDIR /src
COPY package.json ./
RUN npm install --global "$(node -p 'require("./package.json").packageManager')" "agent-browser@${AGENT_BROWSER_VERSION}"

USER dev
COPY --chown=dev:dev package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --chown=dev:dev apps/desktop/package.json apps/desktop/
RUN pnpm install --frozen-lockfile --store-dir /opt/pnpm-store

USER root
RUN apps/desktop/node_modules/.bin/playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/* \
    && ln -s "$(ls -d /opt/ms-playwright/chromium-*/chrome-linux*/chrome)" /usr/local/bin/chromium

USER dev
COPY --chown=dev:dev apps/cli/go.mod apps/cli/go.sum apps/cli/
RUN cd apps/cli && go mod download

COPY --chown=dev:dev . .
RUN cd apps/cli && CGO_ENABLED=0 go build -o /opt/go/bin/agentx .

ENV PATH=/opt/go/bin:$PATH \
    AGENTX_CLI=/opt/go/bin/agentx \
    AGENTX_REMOTES=/srv/remotes \
    AGENT_BROWSER_EXECUTABLE_PATH=/usr/local/bin/chromium

CMD ["apps/desktop/container/start.sh"]
