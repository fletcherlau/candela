# syncer 镜像：多阶段构建，产物为静态二进制 + 配置。
# 构建上下文是 syncer/ 目录（见 deployments/compose.yaml）。
FROM golang:1.24-alpine AS build
# 构建机在中国大陆，走镜像代理；go.sum 已锁定模块哈希
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/syncer .

FROM alpine:3.20
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=build /out/syncer /app/syncer
COPY etc/syncer-api.yaml /app/etc/syncer-api.yaml
# 密钥（MYSQL_DSN/TUSHARE_TOKEN/SYNC_API_KEY）运行时经环境变量注入
CMD ["/app/syncer", "-f", "etc/syncer-api.yaml"]
