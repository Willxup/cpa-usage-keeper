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
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go-builder /out/cpa-usage-keeper /app/cpa-usage-keeper
COPY --from=web-builder /app/web/dist /app/web/dist
EXPOSE 8080
CMD ["/app/cpa-usage-keeper"]
