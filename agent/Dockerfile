# syntax=docker/dockerfile:1
# Agent 服务镜像：一个常驻的 HTTP 服务（src/serve.ts），只带编译产物 dist/ 和生产依赖。
# 构建上下文是 agent/ 目录，可以脱离整个仓库单独构建：docker build -t video-canvas-agent agent

# 依赖层放在源码之前，源码改动不会让依赖层失效；版本由 package-lock.json 精确锁定
FROM node:22-alpine AS build
WORKDIR /rt
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json tsconfig.build.json ./
COPY src ./src
RUN npm run build && test -f dist/serve.js

# 运行时只要生产依赖：--omit=dev 不装 typescript 和类型包
FROM node:22-alpine AS deps
WORKDIR /rt
COPY package.json package-lock.json ./
RUN npm ci --omit=dev

FROM alpine:3.22
WORKDIR /app
# node 二进制取自官方镜像（alpine 仓库里的版本可能低于 pi 要求的 22.19）；它动态链接 libstdc++。运行镜像里没有 npm
RUN apk add --no-cache libstdc++ && addgroup -S app && adduser -S -G app app
COPY --from=node:22-alpine /usr/local/bin/node /usr/local/bin/node
COPY --from=build /rt/dist ./dist
COPY --from=deps /rt/node_modules ./node_modules
COPY package.json ./

ENV NODE_ENV=production
USER app:app
EXPOSE 8090
# 健康检查不要令牌；收到停止信号时服务会放弃所有运行并在 25 秒内退出，所以容器的 stop_grace_period 给 30 秒
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8090/health >/dev/null || exit 1
ENTRYPOINT ["node", "dist/serve.js"]
