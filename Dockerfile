# syntax=docker/dockerfile:1

ARG NODE_IMAGE=node:24.22.1-alpine
ARG GO_IMAGE=golang:1.26.10-alpine

FROM ${NODE_IMAGE} AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS go-builder
WORKDIR /app
RUN apk add --no-cache build-base
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-builder /app/web/dist ./web/dist
COPY web/static.go ./web/static.go
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X cpa-usage-keeper/internal/version.Version=${VERSION}" \
    -o /out/cpa-usage-keeper ./cmd/server/main.go

FROM alpine:3.24
WORKDIR /
RUN apk add --no-cache ca-certificates tzdata su-exec \
	&& addgroup -S app \
	&& adduser -S -G app app \
	&& mkdir -p /data \
	&& chown -R app:app /data
COPY --from=go-builder /out/cpa-usage-keeper /app/cpa-usage-keeper
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh \
	&& chmod +x /usr/local/bin/docker-entrypoint.sh
VOLUME ["/data"]
EXPOSE 8080
# 先去除前缀外围的空格、tab 与换行；路径规范化由就绪路由处理。
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD base_path="${APP_BASE_PATH:-}"; \
    base_path="${base_path#"${base_path%%[![:space:]]*}"}"; \
    base_path="${base_path%"${base_path##*[![:space:]]}"}"; \
    wget -q -O /dev/null "http://127.0.0.1:${APP_PORT:-8080}${base_path}/readyz" || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/cpa-usage-keeper"]
