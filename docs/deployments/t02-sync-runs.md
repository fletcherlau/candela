# T02：中证全指持久同步任务

对应 [#19](https://github.com/fletcherlau/candela/issues/19)。已于 2026-09-15 部署演示站，等待用户验收；本记录同时说明实现、隔离验证及回退步骤。

## 实现与验收入口

在 `/admin/data`（`/admin` 同样可用）点击“历史回填”或“增量同步”。提交成功后 URL 带有 `?run=<id>`，刷新或重新打开该 URL 会查询同一记录。最近 50 条任务列在左侧，详情显示固定范围、实际起点、分段进度、已处理记录、已提交截止日、覆盖依据及失败原因。

网站经 Access 认证及同源 CSRF 验证后，向 syncer 转发白名单请求。内部密钥不进入浏览器；前台导航不增加管理入口。市场图表、MACD、市场状态分析仍由后续任务实现。

| 浏览器 API | syncer API | 作用 |
| --- | --- | --- |
| `POST /api/sync-runs` | `POST /api/v1/data/sync-runs` | `{ "mode": "backfill" }` 或 `{ "mode": "incremental" }`；新记录返回 202，复用未结束记录返回 200 |
| `GET /api/sync-runs` | `GET /api/v1/data/sync-runs` | 最近 50 条执行记录 |
| `GET /api/sync-runs/<id>` | `GET /api/v1/data/sync-runs/<id>` | 持久执行详情 |

对象仅允许 `000985.CSI`，不接受客户端指定对象、源站地址、日期或取消操作。提交时按北京时间固定截止日：18:00 前为前一日，18:00 起为当日。交易日历决定区间内应有的交易日；若源端仍未发布应有行情，该段明确失败，不将数据缺失当作成功。

## 历史范围与数据口径

- 回填记录的固定范围从日历下界 `00010101` 到提交截止日，表示请求全部历史；这个下界不作为 Tushare 的起始参数，不假设指数在该日成立。
- 后台调用 `index_daily`，省略起始参数并逐次将截止日移到当前最早记录的前一日；即使返回短页也继续探测，直到一次成功的空响应确认此前没有可取得行情。探测结果记录为实际起点和覆盖依据；只有空结果、权限不足、限流或响应畸形均不能完成回填。
- 从实际起点按最多 366 个日历日分段获取。返回 `has_more` 或达到请求上限时递归拆分日期窗口，单日仍截断则失败；逐段用 `trade_cal` 检查遗漏和异常交易日。起点之前的成功空集、周末等闭市区间与交易日缺失分别处理。
- 增量起点在提交时取当前已存最后一个交易日（含该日，重取以更新最近记录），无历史时同样探测全部历史。排队期间不重新计算起点或“今天”。
- 独立 `index_series`、`index_daily` 表保存指数对象与原始日线，不写入 ETF Instrument、复权因子、快照或策略名单。开高低收和交易日期必需；源端缺失的昨收、涨跌和量额以 SQL NULL 原样保留，不伪造为零。
- `processedRows` 是已成功提交分段中的输入记录数，包含幂等重写，不是 MySQL 的实际改动行数。失败分段不增加计数或检查点。

接口依据：[Tushare 指数日线](https://tushare.pro/document/2?doc_id=95)、[指数基本信息](https://tushare.pro/document/2?doc_id=94)。指数的发布日期或基日不被当作源端最早日线日期。

## 执行与事务边界

`syncer/internal/syncrun` 统一负责接受、查询、执行权与分段提交。接受事务锁定指数对象，先复用相同模式及截止日的未结束记录，再计算新请求的增量边界。该事务只写请求，不调用行情源。服务生命周期中的 worker 轮询已接受任务，不依赖 HTTP 上下文，也不负责每日业务定时触发。

同一对象通过 `index_series.active_run` 串行执行，以数据库中的递增 fence 和 30 秒租约标识执行权，运行时每 8 秒续租。写入事务锁定同一对象并核对运行记录、fence 和数据库时间，提交前再次检查原租约。每段行情 upsert、处理数量和连续日期检查点在同一 MySQL 事务里提交；重复提交同一段会被拒绝。

T02 不提供手动取消或中断自动续跑。进程中断后，服务可用时将租约过期的运行任务记为 `failed / interrupted`，保留已提交数据，再处理后续排队任务；不会将中断假报为完成。[T03 / #20](https://github.com/fletcherlau/candela/issues/20) 在此持久边界上实现取消及原任务恢复。

## 可复现的隔离验证

启动独立 MySQL 容器，避免接触现有 `candela-mysql`、生产卷和生产凭据：

```sh
docker run -d --name candela-t02-mysql --label candela.test=t02 \
  -e MYSQL_ROOT_PASSWORD=candela-test-only -e MYSQL_DATABASE=candela_sync_test \
  -p 127.0.0.1:13319:3306 --tmpfs /var/lib/mysql mysql:8.4
# 等待 mysqladmin ping 成功后：
export SYNC_RUN_TEST_DSN='root:candela-test-only@tcp(127.0.0.1:13319)/candela_sync_test?parseTime=true'
go -C syncer test -race ./... -count=1
go -C web test -race ./...
npm --prefix web/frontend ci
npm --prefix web/frontend run build
npm --prefix web/frontend run test:e2e
(cd web/frontend && npx playwright test --config playwright.sync.config.ts)
```

测试仅接受数据库名 `candela_sync_test`，会清理其中的任务、指数和 ETF 测试表。浏览器同步测试需要同一 DSN 和空闲的 18081、18082 端口；测试二进制提供本地签名密钥、可控 Tushare 协议响应和真实 MySQL，不加入生产鉴权旁路。Chrome 首次运行需要安装 Playwright 浏览器及操作系统依赖。

已验证：2004 年起完整历史、截断分段、增量幂等及计数、空数据与权限／限流分类、并发重复提交、同对象互斥、数据与检查点回滚、已提交分段保留、失权写入拦截、断开请求后后台完成、迁移重放、Access／CSRF 与转发白名单，以及浏览器提交／刷新／增量／失败状态和手机布局。测试行情是可控夹具，不代表真实 Tushare 历史覆盖或现有账号权限已验证。

## 发布与回退

1. 使用本分支构建 syncer 和 web 镜像，记录镜像摘要并保留现有镜像。只重建这两个服务，沿用现有环境及网络；不重建数据库、机器人或 Tunnel。
2. syncer 启动自动执行 `schema.Ensure`。新表由版本 1 迁移创建，`schema_migration` 记录版本；MySQL 命名锁串行化多个实例的迁移，DDL 可重复执行，所有语句成功后才写版本。旧表结构及已有 ETF／申万数据不修改。
3. 先确认 syncer 启动、受鉴权保护的任务列表及目录正常，再更新 web。登录后在管理页人工提交真实回填，检查权限、覆盖依据、任务进度与数据库实际记录；生产不注入测试源或测试令牌。
4. 验收关注刷新后的同一任务编号、重复提交复用、失败原因、完成后的中证全指覆盖范围，以及增量不重复产生记录。手动取消与自动恢复不在本次验收范围。

回退前停止新的任务接受和 worker，再恢复旧 syncer／web 镜像。新增表保留，不执行 DROP 或删除任务／行情；旧版服务忽略这些表。重新升级后未结束执行按上述租约过期规则显式记为中断。不要执行 `compose down` 或删除数据库卷。

## 2026-09-15 演示站发布记录

- 地址：[数据管理](https://demo.candlea.cn/admin/data)，来自 `codex/19-csi-background-sync` 的实现提交 `e652e23`，尚未合入 master。
- 镜像：`candela-syncer:t02-20260915`、`candela-web:t02-20260915`；旧镜像保存为对应的 `before-t02-20260915` 标签。
- syncer 镜像摘要：`sha256:512c9d037379091e2e812d31b9c90fd6a02492ee4fdb362993c6e57869a97c49`；web 镜像摘要：`sha256:931aa402403fb80a432f3a21246cbcb7e16fc218e0b10e250fcdd06eb6516e3f`。
- 版本 1 新表迁移后，内部受鉴权任务列表正常返回空列表，指数目录显示零条“暂无数据”；未发起真实回填或增量任务。
- 发布前后 ETF、申万目录内容一致；MySQL 和机器人容器 ID 保持不变，syncer、web 正常运行且重启计数为零。
- 源站页面及新旧 API 未认证均返回 401；公网 `/admin/data` 返回 302 进入原有 Access 登录。未代替用户完成个人 Access 登录后的人工验收。
- 本地忽略目录 `.scratch/t02` 保存发布脚本、镜像报告和回退 override；脚本复用原运行环境、验证失败自动恢复旧镜像，不把凭据写入仓库。新表和已有数据在回退时保留。

## 2026-09-16 首日空值修复

首次真实回填在历史探测阶段失败，任务 `3f71e5096910739139bcc7860882c7fb` 未写入数据。根因是源端 `20041231` 记录的开高低收均为 1000，但 `pre_close/change/pct_chg/vol/amount` 为 null；T02 的全字段非空校验及 v1 列约束错误地拒绝了这条有效首日记录。原测试夹具所有数值都有值，未覆盖源端首日形态。

修复将这五个字段建模为可空数值，版本 2 迁移只放宽对应列的 NULL 约束，保留已有值和价格列的非空约束。缺少必需字段仍失败并指出字段名称。真实首日的最小响应已作为不含凭据的回归夹具，覆盖历史探测、按日获取、NULL upsert、v1 升级和迁移重放；其余日期继续按原交易日历校验。

修复验证：全部 syncer race 测试通过；使用当前真实行情源只读获取 `20041231..20260915`，在隔离真实 MySQL 中完成 5,273 条、22/22 分段回填，首日五个空字段以 SQL NULL 保留。此验证未写入演示站数据库。
