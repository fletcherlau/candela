# Candela 网站

独立 Go 服务提供受 Access 保护的网页、数据目录与中证全指后台同步操作。浏览器仅访问本站，网站通过内部密钥调用 syncer，不连接 MySQL。前端使用 React / TypeScript / Vite、Tailwind CSS v4 和 shadcn/ui；市场图表与状态分析由后续任务实现。

## 配置与运行

需要 Go 1.26.3 或兼容工具链、Node 24。安装依赖与构建：

```sh
npm --prefix web/frontend ci
npm --prefix web/frontend run build
go -C web build -o candela-web .
```

运行环境：`CF_ACCESS_ISSUER` 为现有 Access 团队 HTTPS 地址，`CF_ACCESS_AUD` 为应用 audience，`WEB_ORIGIN` 为网站 HTTPS origin，`SYNCER_API_BASE` 为内部 syncer origin，`SYNC_API_KEY` 为原有内部密钥。不得将密钥写入前端环境、镜像或 Git。静态资源默认 `frontend/dist`，可通过 `WEB_STATIC_DIR` 指定；`WEB_RESEARCH_DIR` 指向原研究资源目录。默认只监听 `127.0.0.1:8080`，没有关闭鉴权的运行选项。

在 web 目录启动二进制。生产通过已配置的 Cloudflare Tunnel 访问，直接裸访问本机页面会返回 401，这是预期行为；不要为了本地预览移除鉴权。

Compose 增加可选 web profile，保留现有服务的默认启动行为：

```sh
docker compose -f deployments/compose.yaml --env-file .env --profile web up -d --build syncer web
```

上线前确认 8080 没有其他服务占用，备份旧占位服务及 syncer 镜像。只替换网站和必要的 syncer，不重建数据库或机器人；保持现有 Tunnel / Zero Trust 配置不变。异常时停新 web 并恢复原占位服务，以旧镜像恢复 syncer。具体发布记录见部署记录文档。

## API 与数据口径

- `GET /api/catalog`：网站经服务端内部密钥调用 `GET /api/v1/data/catalog`。目录查询只读，不触发同步；失败详情不包含内部错误或凭据。
- 每个分类单独表示读取失败，保留其他成功分类。ETF 日线、因子各自统计最早／最晚日期和记录数，包括停用对象。申万日线独立统计；字典与成分是参考数据，不伪造行情日期。
- 中证全指独立统计 `index_daily` 的覆盖日期和记录数，尚未同步时显示“暂无数据”。“已有数据”不等于已更新至今天；刷新只重新查询覆盖。
- `GET /api/session`：生成 Secure、HttpOnly、SameSite=Strict 的 CSRF Cookie，返回配对令牌。提交同步任务必须同时携带本站 Origin、Cookie 和 X-CSRF-Token；仅允许白名单路径与模式，其他写操作返回 405。
- `POST /api/sync-runs` 提交历史回填／增量任务；`GET /api/sync-runs` 读取列表，`GET /api/sync-runs/<id>` 读取详情。见 [T02 验收与迁移说明](../docs/deployments/t02-sync-runs.md)。
- `/` 为品牌首页，顶栏仅含 Logo 与“策略 → 市场状态”；`/market` 为市场状态。`/admin` 与 `/admin/data` 为独立数据管理页面，前台无后台入口。旧 `/data` 跳转至 `/admin/data`，`/research` 跳转至首页；研究档案和版本对照不再进入产品导航。原研究静态文件仅保留旧链接兼容。
- 基础控件从 shadcn/ui 官方 registry 引入，源码归仓库维护（`src/components/ui`）；通过 `components.json` 添加组件，业务页面放在 `src/pages`。样式令牌集中在 `src/style.css`，沿用 Candela 配色。
- 所有页面、资源与 API 都验证 Access 的签名、issuer、audience、有效期和 nbf；内部转发不接受客户端自定义目的地址，也不转发 Access 凭据。

## 验证

```sh
go -C web test ./...
npm --prefix web/frontend run typecheck
npm --prefix web/frontend run test:e2e
```

端到端测试自动启动仅含隔离数据和临时签名密钥的 Go 测试进程；它不是生产服务的鉴权旁路。浏览器首次使用需在前端目录执行 `npx playwright install chromium`。需要可用的 18081 测试端口。

syncer 目录覆盖测试使用隔离 MySQL，数据库名称必须为 `candela_catalog_test`：

```sh
CATALOG_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_catalog_test?parseTime=true' go -C syncer test ./internal/store -count=1
```

未配置测试 DSN 时该集成测试跳过。测试会清理专用库的数据，禁止指向生产库。网站单元测试不依赖 Cloudflare 网络，使用本地 JWKS 和真实 RSA 签名验证拒绝路径。
