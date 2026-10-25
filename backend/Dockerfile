# syntax=docker/dockerfile:1
# 构建上下文是仓库根目录（要同时拿到 backend/ 和 agent/）：docker build -f backend/Dockerfile .

FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILDTIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X video-canvas/pkg/version.Version=${VERSION} -X video-canvas/pkg/version.GitCommit=${COMMIT} -X video-canvas/pkg/version.BuildTime=${BUILDTIME}" \
    -o /out/server ./cmd/server

# 画布 Agent 运行时：TypeScript 源码在这里编译成 dist/，镜像里只带产物和生产依赖，不带 src 和 typescript。
# 依赖层放在源码之前，源码改动不会让依赖层失效；版本由 package-lock.json 精确锁定
FROM node:22-alpine AS agent-build
WORKDIR /rt
COPY agent/package.json agent/package-lock.json ./
RUN npm ci
COPY agent/tsconfig.json agent/tsconfig.build.json ./
COPY agent/src ./src
RUN npm run build && test -f dist/main.js

# 运行时只要生产依赖：--omit=dev 不装 typescript 和类型包
FROM node:22-alpine AS agent-deps
WORKDIR /rt
COPY agent/package.json agent/package-lock.json ./
RUN npm ci --omit=dev

FROM alpine:3.22

WORKDIR /app
# node 二进制取自官方镜像（alpine 仓库里的版本可能低于 pi 要求的 22.19）；它动态链接 libstdc++
# 画布 Agent 看视频、截帧需要 ffmpeg / ffprobe。
RUN apk add --no-cache libstdc++ ffmpeg && addgroup -S app && adduser -S -G app app
COPY --from=node:22-alpine /usr/local/bin/node /usr/local/bin/node
COPY --from=build /out/server /app/server
COPY --from=agent-build /rt/dist /app/agent/dist
COPY agent/package.json /app/agent/package.json
COPY --from=agent-deps /rt/node_modules /app/agent/node_modules
COPY --from=build /src/configs/config.yaml /app/configs/config.yaml
# /run/plugin-runner 是 backend 与 plugin-runner 容器共享 Unix socket 的卷挂载点，首次挂载时命名卷继承这里的属主
RUN mkdir -p /app/data/assets /run/plugin-runner && chown -R app:app /app /run/plugin-runner

ENV APP_AGENT_RUNTIME_DIR=/app/agent

USER app:app
EXPOSE 8080
ENTRYPOINT ["/app/server", "-c", "/app/configs/config.yaml"]
