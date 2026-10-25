# syntax=docker/dockerfile:1
# 构建上下文是仓库根目录：docker build -f backend/Dockerfile .
# 画布 Agent 是独立的服务（agent/Dockerfile，镜像 video-canvas-agent），这个镜像里没有 Node。

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

FROM alpine:3.22

WORKDIR /app
# 画布 Agent 看视频、截帧需要 ffmpeg / ffprobe（抽帧由 backend 做，Agent 服务不联网也不碰素材）。
RUN apk add --no-cache ffmpeg && addgroup -S app && adduser -S -G app app
COPY --from=build /out/server /app/server
COPY --from=build /src/configs/config.yaml /app/configs/config.yaml
# /run/plugin-runner 是 backend 与 plugin-runner 容器共享 Unix socket 的卷挂载点，首次挂载时命名卷继承这里的属主
RUN mkdir -p /app/data/assets /run/plugin-runner && chown -R app:app /app /run/plugin-runner

USER app:app
EXPOSE 8080
ENTRYPOINT ["/app/server", "-c", "/app/configs/config.yaml"]
