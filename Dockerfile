# syntax=docker/dockerfile:1

FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-alpine AS go-builder
WORKDIR /app
RUN apk add --no-cache build-base
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/cpa-usage-keeper ./cmd/server/main.go

FROM alpine:3.20
WORKDIR /
RUN apk add --no-cache ca-certificates tzdata su-exec \
	&& addgroup -S app \
	&& adduser -S -G app app \
	&& mkdir -p /data \
	&& chown -R app:app /data
COPY --from=go-builder /out/cpa-usage-keeper /app/cpa-usage-keeper
COPY --from=web-builder /app/web/dist /app/web/dist
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q --spider "http://127.0.0.1:${APP_PORT:-8080}${APP_BASE_PATH:-}/healthz" || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/cpa-usage-keeper"]
