# ADR-0004: 飞书机器人独立成 feishubot 服务，长连接收事件，推送 cron 触发

- 状态：已接受
- 日期：2026-08-04

## 背景

需要两个界面能力：(a) 每日两次把同步状态推送到飞书会话；(b) 在飞书里用聊天命令查状态（`status`）、触发增量同步（`sync`）。最初方案是企业微信智能机器人，调研后改用**飞书自建应用机器人**：飞书发消息不要求用户先发过消息（企微有 24h 会话窗口限制），且官方 Go SDK 内置长连接，无需公网回调域名。

可选方案：(a) 机器人能力做进 syncer 进程；(b) 独立服务持飞书长连接，经 HTTP 调 syncer。

## 决策

采用 (b)：新增独立模块 `feishubot/`，与 syncer 平级、独立进程：

- **独立服务**：feishubot 不连 MySQL，只经 syncer 的 HTTP API（`GET /api/v1/sync/status`、`POST /api/v1/sync/etf-daily`，X-Api-Key 鉴权）读写数据，是 syncer 之上的界面层。进程崩溃互不影响，可独立发版。
- **长连接收事件**：用官方 SDK（`github.com/larksuite/oapi-sdk-go/v3`）的 WebSocket 长连接订阅 `im.message.receive_v1`，心跳与断线重连由 SDK 维护，进程级兜底靠 compose `restart: unless-stopped`。无需公网 IP / 回调域名。
- **推送 cron 触发**（遵循 ADR-0001）：feishubot 暴露 `POST /api/v1/push/daily-report`（X-Api-Key 鉴权），由系统 crontab 每日 14:45 / 18:00 curl 触发；服务内部不含调度逻辑。crontab 示例：
  ```
  45 14 * * * curl -fsS -X POST -H "X-Api-Key: $SYNC_API_KEY" http://127.0.0.1:8889/api/v1/push/daily-report
  0  18 * * * curl -fsS -X POST -H "X-Api-Key: $SYNC_API_KEY" http://127.0.0.1:8889/api/v1/push/daily-report
  ```
- **命令界面作为早期 UI**：聊天命令（`help`/`status`/`sync`，支持中文 `帮助`/`状态`/`同步`）是平台的早期交互界面，回复用飞书卡片（lark_md markdown）引用原消息。
- 不用 go-zero：HTTP 侧只有 ping 与推送两个端点，stdlib net/http 足够；配置纯环境变量，与 syncer 注入方式一致。

## 后果

- 飞书凭证与机器人行为独立于数据管道演进；换掉/新增一个 IM 渠道不动 syncer。
- 日报推送的调度语义与 syncer 的同步触发完全同构：运维层 crontab + 带鉴权的 HTTP 端点，服务无状态。
- 代价：多一个常驻进程；聊天命令链路跨两个服务（飞书事件 → feishubot → syncer），排障需看两侧日志。
