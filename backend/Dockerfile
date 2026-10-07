# syntax=docker/dockerfile:1

FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILDTIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X video-canvas/pkg/version.Version=${VERSION} -X video-canvas/pkg/version.GitCommit=${COMMIT} -X video-canvas/pkg/version.BuildTime=${BUILDTIME}" \
    -o /out/server ./cmd/server

# 画布 Agent 运行时的依赖：单独一个阶段装，源码改动不会让依赖层失效。
# 版本由 package-lock.json 精确锁定；--omit=dev 不装测试用的依赖
FROM node:22-alpine AS agent-runtime-deps
WORKDIR /rt
COPY agent-runtime/package.json agent-runtime/package-lock.json ./
RUN npm ci --omit=dev

FROM alpine:3.22

WORKDIR /app
# node 二进制取自官方镜像（alpine 仓库里的版本可能低于 pi 要求的 22.19）；它动态链接 libstdc++
RUN apk add --no-cache libstdc++ && addgroup -S app && adduser -S -G app app
COPY --from=node:22-alpine /usr/local/bin/node /usr/local/bin/node
COPY --from=build /out/server /app/server
COPY agent-runtime/src /app/agent-runtime/src
COPY agent-runtime/package.json /app/agent-runtime/package.json
COPY --from=agent-runtime-deps /rt/node_modules /app/agent-runtime/node_modules
COPY --from=build /src/configs/config.yaml /app/configs/config.yaml
# /run/plugin-runner 是 backend 与 plugin-runner 容器共享 Unix socket 的卷挂载点，首次挂载时命名卷继承这里的属主
RUN mkdir -p /app/data/assets /run/plugin-runner && chown -R app:app /app /run/plugin-runner

USER app:app
EXPOSE 8080
ENTRYPOINT ["/app/server", "-c", "/app/configs/config.yaml"]
