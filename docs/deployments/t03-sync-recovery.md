# T03：同步取消与服务重启恢复

对应 [#20](https://github.com/fletcherlau/candela/issues/20)，也是轮动固定 14:45 采集 R03 的前置能力。当前先在中证全指任务上实现，ETF 批量同步与其他入口归 #21。未连接或修改生产数据库。

## 行为

管理页 `/admin/data` 的任务详情可取消排队或运行中的任务。排队任务直接变为“已取消”；运行任务先保存“取消中”，在执行者确认或恢复服务处理时变为“已取消”。取消已结束任务返回当前终态，重复取消不会重复写事件。已保存分段保留，取消不意味着回滚历史数据。

取消和分段提交使用同一对象行锁，具有明确先后顺序：先完成的分段属于保留数据；取消事务提交后，旧执行者返回的行情不能写入新段、进度或成功终态。服务运行时每 8 秒检查执行权；外部请求终止和取消确认可能需要短暂等待，但取消意图一经接受便阻止新的段提交。

服务正常停机时释放为可恢复状态；崩溃或失联后等待原数据库租约到期（30 秒）再取得新执行权。恢复保留原任务编号、对象、起止日期、已确认历史起点及依据，从检查点后的下一段继续。未确认结果的源请求可重读；已成功分段不重跑。取消任务和数据源导致的最终失败任务不自动恢复。

中断、恢复、取消请求和取消完成事件与对应状态变更同事务保存，包含当时检查点和 UTC 时间；管理页按北京时间展示。查询和刷新不启动新任务，详情状态与事件来自同一数据库快照。

## 接口与迁移

- 内部 `POST /api/v1/data/sync-runs/{id}/cancel`，空请求体，继续使用服务密钥鉴权。
- 网页 `POST /api/sync-runs/{id}/cancel`，精确路径白名单，保留 Cloudflare Access、同源与 CSRF 验证；服务密钥不进入浏览器。
- 原列表／详情接口兼容，详情新增可选 `events`。终态取消返回 HTTP 200 和当前状态；未知编号 404。
- 迁移 v5 添加 `sync_run_event`，不改写现有行情；DDL 可重放。旧版本已标记最终失败的记录不会在升级时擅自变为待执行。

## 验证与复现

测试源只提供隔离夹具，不代表真实行情服务覆盖。测试仅接受数据库 `candela_sync_test`；会清理测试任务和测试行情。

```sh
export SYNC_RUN_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_sync_test?parseTime=true&loc=UTC'
go -C syncer test -race ./internal/syncrun -count=1
go -C web test -race ./...
npm --prefix web/frontend ci
npm --prefix web/frontend run build
cd web/frontend
npx playwright test --config playwright.sync.config.ts
```

本机浏览器另使用 `LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu` 和 `FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`。

已验证：排队／运行取消、重复请求、终态幂等、迁移重放、已提交数据保留、迟到源响应、失权保护、跨日固定范围、正常停机、真实子进程强制终止后重建及取消后重启。结果通过维护 HTTP 接口观察；不以生产环境试错。全量 syncer/web race 和构建检查通过。管理页验证真实提交／刷新／取消／恢复记录、权限错误、键盘焦点与 1440 / 820 / 390 / 320px 布局。

[桌面截图](../design/sync-recovery/desktop.png) · [手机截图](../design/sync-recovery/mobile.png)

浏览器服务仅编译进测试二进制，监听 loopback 18081／18082；慢源和重建控制入口需要测试密钥，不加入生产路由。截图为测试数据，不是生产任务记录。

## 发布与回退

本票未独立部署。用户已授权七张轮动任务全部验证后发布至 candlea.cn；现有 R01/R02 之后仍需 R03–R07。发布时先迁移并升级 syncer，再升级网页；等待旧执行权到期后核对原任务续跑记录。回退前停止新的提交并停止 worker，保留任务／事件／行情表；旧版本不能正确处理新取消状态，不能作为本次取消／恢复语义的等价回退方案。需要回退时应先确认所有未完成任务的状态并保持新版协调服务，不能删除表或修改历史任务伪装完成。
