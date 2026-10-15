# syntax=docker/dockerfile:1
# Multi-stage build: zero Node, zero CGO, purely static Go binary
FROM golang:1.24-alpine AS builder

WORKDIR /src

# 安装 ca-certificates 与 tzdata
RUN apk add --no-cache ca-certificates tzdata

# 缓存依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并静态编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/nocturnedesk .

# 运行镜像：极简 Alpine 运行环境
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/data

WORKDIR /app
COPY --from=builder /bin/nocturnedesk /app/nocturnedesk

VOLUME ["/app/data"]
EXPOSE 8080

ENTRYPOINT ["/app/nocturnedesk"]
CMD ["--data", "/app/data"]
