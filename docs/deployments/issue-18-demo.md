# #18 本机演示部署记录

日期：2026-09-09。用户授权实现 #18 并部署到本机，通过 https://demo.candlea.cn 查看。

## 已交付

- 市场总览独立入口，明确中证全指尚未接入，不展示模拟行情或提前输出指标。
- 数据管理分类显示已有 ETF、申万覆盖，支持分类和代码／名称搜索；刷新仅重新查询，不同步或修改数据。
- 原研究页面、数据资源及原目录入口保留。
- Go 网站独立于 syncer，经服务端内部鉴权读取目录；所有页面、资源与业务 API 验证 Cloudflare Access JWT。

## 部署结果

- 原 Cloudflare Tunnel 仍转发 demo.candlea.cn 到 127.0.0.1:8080，未修改隧道和 Zero Trust 登录配置。
- 8080 的旧占位服务已停用，由 candela-web 容器接替，设置自动重启，只读文件系统、非 root 用户、无额外 Linux capabilities。
- syncer 更新为含目录查询接口的镜像，仍只映射 127.0.0.1:8888。MySQL、机器人容器 ID 未改变，未发起生产同步或数据迁移。
- 上线时查询到 ETF 8 个数据集（28,134 条记录），申万 513 个数据集（1,550,517 条记录）；数据集含各独立日线、因子或参考数据。部署前后完整目录响应一致。
- 运行镜像为 candela-web:issue18 和 candela-syncer:issue18；旧 syncer 镜像保留为 candela-syncer:before-issue18。

## 验证与限制

- syncer 全套测试通过，包括隔离 MySQL 的覆盖范围、空数据、停用对象和分类读取失败测试。
- 网站 Go 测试及 race 检查通过；缺失、伪造、过期、错误 issuer／audience 凭据被拒绝，跨站写请求被拒绝，任意内部路径不可转发。
- 3 项浏览器端到端测试通过：未认证访问保护，页面分离／筛选／研究入口，移动端与数据服务失败提示。浏览器使用隔离测试进程和临时签名密钥，没有给生产服务添加鉴权旁路。
- 前端构建、类型检查、机器人全套回归通过。代码审查 Standards 0 项、Spec 0 项；另补充旧研究目录入口跳转。
- 正式源站未携带令牌返回 401；网站容器可获取真实 Access 签名公钥；公网首页、数据页和 API 均返回 302 到既有 Access 登录。
- 尚未代替用户完成其个人 Zero Trust 登录后的公网人工验收。合法签名后的页面与 API 行为已在隔离环境验证；用户可以使用已有账号直接打开演示域名查看。

## 运维与回退

此次镜像从 codex/18-website-data-catalog 工作分支构建。运行容器不依赖工作区文件挂载。后续重新构建须使用该功能已合入的代码，避免用旧版 syncer 覆盖新增目录接口。

本地忽略目录 `.scratch/deploy-issue18` 保存实际 Compose override、受限权限运行环境、部署检查和回退资料；不得提交这些含凭据的环境文件。网站 Access issuer、audience、origin 均来自既有公开应用配置。MySQL 和内部密钥沿用原容器，不应重新生成。

回退时先停止 candela-web，再通过保留的原 unit 文件完整路径重新启用占位服务（停用 unit 时用户目录符号链接可能已移除，不能只依赖 unit 名称）。原定义位于 `/home/fletcherlau/works/candela/.scratch/cloudflare-demo/candela-demo.service`；部署资料中也保存了副本。若需同时回退 syncer，将部署 override 中镜像改为 candela-syncer:before-issue18，再只重建 syncer，保留既有环境和网络。不执行 compose down，不删除数据库卷，不重建机器人。
