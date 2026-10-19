# syntax=docker/dockerfile:1
FROM node:22-bookworm AS node
# 浮动 minor 标签 + GOTOOLCHAIN=auto：上游 go.mod 会随版本演进升 Go 要求
# （实测 211 个上游提交把要求从 1.26.6 提到 1.26.9，死钉补丁版直接构建失败）；
# 浮动标签自动跟最新 1.26.x，auto 兜底将来跳 1.27 也自动下载工具链，无需改本文件。
FROM golang:1.26-bookworm AS build
COPY --from=node /usr/local/ /usr/local/
RUN npm install -g pnpm@11.15.1
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
COPY agent/go.mod agent/go.sum ./agent/
RUN go mod download && cd agent && go mod download
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./web/
COPY web/patches ./web/patches
RUN pnpm --dir web install --frozen-lockfile
COPY . .
RUN go mod tidy -diff && (cd agent && go mod tidy -diff)
ARG DENOVA_VERSION=dev
RUN bash scripts/build.sh \
    && go build -trimpath -o output/denova-container-init ./docker/init-config.go

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates curl git python3 chromium fonts-noto-cjk tini tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 denova && useradd --uid 1000 --gid 1000 --home-dir /data --shell /bin/bash denova \
    && mkdir -p /data/.denova && chown -R denova:denova /data
COPY --from=build /src/output/ /opt/denova/
COPY LICENSE /opt/denova/LICENSE
COPY docker/entrypoint.sh /usr/local/bin/denova-entrypoint
COPY docker/chromium.sh /usr/local/bin/chromium
RUN chmod 755 /usr/local/bin/denova-entrypoint /usr/local/bin/chromium
ENV DENOVA_DIR=/data/.denova HOME=/data PATH=/opt/denova:/opt/denova/tools:/usr/local/bin:/usr/bin:/bin
WORKDIR /data
USER denova
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=10 CMD curl -fsS http://127.0.0.1:8080/api/auth/status >/dev/null || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/denova-entrypoint"]
CMD ["/opt/denova/denova", "--no-open", "--port", "8080"]
