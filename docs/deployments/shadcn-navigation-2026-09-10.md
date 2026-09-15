# React / shadcn/ui 导航更新部署

日期：2026-09-10。用户明确要求部署本轮改动，以便查看效果。

## 已部署

- 地址：https://demo.candlea.cn/ 。
- 首页采用品牌介绍与市场状态入口；顶栏仅有 Logo 和“策略 → 市场状态”。
- `/market` 为市场状态；`/admin` 和 `/admin/data` 为独立数据管理，前台没有管理入口。
- `/data` 跳转到 `/admin/data`，`/research` 跳转到首页。研究档案不再作为产品入口。
- 现有页面迁为 React + TypeScript + Vite、Tailwind CSS v4 和 shadcn/ui。策略可视化后续由 #28 细化。

## 发布与验证

- 从 `codex/18-website-data-catalog` 的当前工作区构建 `candela-web:shadcn-20260910`，包括本轮尚未提交的修改；此记录不代表已合并主分支。
- Docker 多阶段构建通过，前端类型检查与构建成功；发布前 Go race 测试、5 项隔离浏览器测试通过，涵盖键盘导航、手机布局、真实目录筛选／刷新与失败状态。
- 仅替换网站容器，沿用原环境、监听地址、Access 与 Tunnel 配置；syncer、MySQL、机器人容器 ID 未变，部署前后真实目录响应摘要一致。
- 新容器运行正常且重启次数为 0；源站首页、市场状态、管理页和目录 API 未认证均返回 401。
- 公网首页、市场状态、管理页和目录 API 使用 curl 检查均返回 302 到既有 Access 登录。Python urllib 请求曾被网关返回 403；同机 curl 检查正常。
- 用户个人登录后的公网页面效果由用户查看；未绕过生产鉴权。市场行情仍处于尚未接入状态。

## 回退

旧网站镜像保留为 `candela-web:before-shadcn-20260910`。本地忽略目录 `.scratch/deploy-issue18` 的 Compose override 已指向新镜像，原 override 备份为 `compose.before-shadcn.yaml`，发布检查结果记录于 `deployed-shadcn.json`。

需要回退时，以原运行环境将 override 的网站镜像改为旧镜像，仅执行网站服务的 `up -d --no-deps --no-build web`。保留数据库、syncer、机器人及其环境，不执行 compose down。
