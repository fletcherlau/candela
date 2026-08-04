# feishubot 镜像：多阶段构建，产物为静态二进制。
# 构建上下文是 feishubot/ 目录（见 deployments/compose.yaml）。
FROM golang:1.24-alpine AS build
# 构建机在中国大陆，走镜像代理；go.sum 已锁定模块哈希
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/feishubot .

FROM alpine:3.20
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=build /out/feishubot /app/feishubot
# 密钥（FEISHU_APP_ID/FEISHU_APP_SECRET/FEISHU_PUSH_CHAT_ID/SYNC_API_KEY）运行时经环境变量注入
CMD ["/app/feishubot"]
