# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=frontend /src/web ./web
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /panel .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates su-exec && addgroup -g 10001 nest && adduser -D -H -u 10001 -G nest nest
COPY --from=build /panel /app/panel
COPY docker-entrypoint.sh /app/entrypoint.sh
COPY LICENSE README.md THIRD-PARTY-NOTICES.md /app/docs/
COPY licenses/ /app/docs/licenses/
RUN chmod 755 /app/entrypoint.sh
LABEL org.opencontainers.image.title="Nest Panel" org.opencontainers.image.version="0.2.0" org.opencontainers.image.licenses="MIT" org.opencontainers.image.source="https://github.com/Stylle/nest-panel" org.opencontainers.image.description="Lightweight single-user navigation panel with portable icons, themes and Sun-Panel JSON import"
ENV DATA_DIR=/data LISTEN_ADDR=:8080 GOMEMLIMIT=96MiB GOGC=75
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/entrypoint.sh"]
