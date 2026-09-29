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

FROM alpine:3.22

WORKDIR /app
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/server /app/server
COPY --from=build /src/configs/config.yaml /app/configs/config.yaml
RUN mkdir -p /app/data/assets && chown -R app:app /app

USER app:app
EXPOSE 8080
ENTRYPOINT ["/app/server", "-c", "/app/configs/config.yaml"]
