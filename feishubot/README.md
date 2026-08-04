# feishubot

飞书自建应用机器人服务（ADR-0004）。持飞书长连接收聊天命令，经 HTTP 调 syncer；暴露 cron 触发的日报推送端点。不连 MySQL，内部无调度逻辑。

## 命令

在飞书里给机器人发消息（单聊直接发，群聊 @ 机器人后接命令）：

| 命令 | 说明 |
| --- | --- |
| `help` / `帮助` | 显示命令列表 |
| `status` / `状态` | 查看各标的数据同步状态（卡片回复） |
| `sync` / `同步` | 触发一次增量同步，完成后推结果摘要 |

## 配置（环境变量）

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `FEISHU_APP_ID` | 是 | | 飞书自建应用 AppID |
| `FEISHU_APP_SECRET` | 是 | | 飞书自建应用 AppSecret |
| `FEISHU_PUSH_CHAT_ID` | 是 | | 日报推送目标会话 chat_id（获取方式见下） |
| `SYNCER_API_BASE` | 否 | `http://127.0.0.1:8888` | syncer 服务地址 |
| `SYNC_API_KEY` | 否 | 空 | 调 syncer 与本服务 HTTP 端点共用的 X-Api-Key；留空不鉴权（仅限本地调试） |
| `BOT_LISTEN_ADDR` | 否 | `:8889` | 本服务 HTTP 监听地址 |

## 飞书开放平台后台配置（手工）

1. 在[飞书开放平台](https://open.feishu.cn/)创建**自建应用**，在「应用能力」中启用**机器人**能力。
2. 「权限管理」开通以下权限：
   - `im:message`
   - `im:message:send_as_bot`（以机器人身份发消息）
   - `im:message.p2p_msg`（读单聊消息）
   - `im:message.group_msg`（读群消息，群命令需要）
   - `im:chat:readonly`
3. 「事件订阅」：订阅方式选**长连接**，添加事件 `im.message.receive_v1`（接收消息）。
4. 「版本管理与发布」：创建版本并发布（自建应用需发布后权限与事件订阅才生效）。
5. 把机器人拉进目标群，或与它单聊一次。

## 获取 FEISHU_PUSH_CHAT_ID

先用任意值（如 `todo`）启动 feishubot，然后给机器人发一条消息（单聊或目标群内 @ 它），从服务日志复制 `chat_id`：

```
feishubot: 收到消息 chat_id=oc_xxxxxxxx chat_type=p2p message_id=om_xxxxxxxx
```

把 `oc_...` 填入 `FEISHU_PUSH_CHAT_ID` 并重启。

## HTTP 端点

- `GET /api/v1/ping` — 健康检查，无鉴权。
- `POST /api/v1/push/daily-report` — 推送同步状态日报到 `FEISHU_PUSH_CHAT_ID`，X-Api-Key 鉴权。由系统 crontab 触发（ADR-0001 / ADR-0004）：

  ```
  45 14 * * * curl -fsS -X POST -H "X-Api-Key: $SYNC_API_KEY" http://127.0.0.1:8889/api/v1/push/daily-report
  0  18 * * * curl -fsS -X POST -H "X-Api-Key: $SYNC_API_KEY" http://127.0.0.1:8889/api/v1/push/daily-report
  ```

## 本地运行

```sh
cd feishubot
export FEISHU_APP_ID=cli_xxx FEISHU_APP_SECRET=xxx FEISHU_PUSH_CHAT_ID=oc_xxx SYNC_API_KEY=xxx
go run .
```

docker compose（仓库根目录）：

```sh
docker compose -f deployments/compose.yaml --env-file .env up -d --build feishubot
```

## 测试

```sh
cd feishubot && go test ./...
```
