# T04：ETF 批量同步与原范围失败重试

对应 [#21](https://github.com/fletcherlau/candela/issues/21)，为轮动 R06 管理恢复提供统一的 ETF 执行协调。继承 R05 的页面与历史复盘实现；本票不代表 R06/R07 或生产发布完成。旧工作区未改动，测试与浏览器均使用隔离数据库和测试行情。

## 行为与兼容

`/admin/data` 新增 ETF 批量同步面板，可同步当前启用名单或明确指定代码，不修改名单开关。提交时固定标的与北京时间当日截止日期；沿用旧 ETF 接口的当日取数行为，包括 15:00–18:00 调用收盘报告的情况。ETF 不沿用指数在 18:00 前截止昨日的规则。

日线和复权因子分别冻结起点、分段大小、检查点及已处理记录数，允许日期不齐。失败不阻断其他对象。批次区分排队、处理中、全部成功、部分失败、失败和取消；对象详情显示两个步骤及真实失败原因。已覆盖的步骤显示无需取数，不将空查询范围计为取得新行情。

失败重试仅复制失败对象，沿用原截止日、范围和检查点，关联父批次与原执行；同一父批次重复重试返回原有子批次，失败子批次可再次重试。成功对象不会重新请求。批次取消保留成功项和已提交步骤，阻止后续提交。完全相同的未结束批次去重；不同批次中相同对象按序执行，取消某批次不会取消另一个批次的执行。

旧 ETF HTTP、机器人相关调用、收盘报告、one-shot 和 rotation-refresh 均通过持久批次等待结果。浏览器或 HTTP 请求断开只结束等待，后台继续。重启沿用原任务和检查点；新旧执行者遵守 T03 的租约、执行权及对象事务锁。

管理详情与检查点从同一快照读取，重试以单条 JOIN 读取一致的状态／进度。中断、恢复和取消记录持久保存，事件注明发生时的日线／因子检查点，管理详情展示最近 200 条。列表最多 50 个批次摘要，不把全部对象明细放入轮询列表。

## 数据与权限边界

迁移 v8 仅新增 ETF 对象、批次、执行、关联、进度和事件表；不删除或改动旧指数外键，也不把 ETF 写入 Index Series。执行权及恢复机制共享实现，行情身份和表独立。

- 内部接口 `/api/v1/data/etf-syncs`：GET 列表、POST 提交；`/{id}` GET 详情；`/{id}/retry` 与 `/{id}/cancel` POST 空请求体。
- 网页代理 `/api/etf-syncs` 对应上述操作，显式白名单；Cloudflare Access、同源、CSRF 和服务密钥隔离保持。
- 浏览器只提交代码或原批次标识，不能指定重试日期、源地址或服务密钥。
- 四标的原始写入与发布 revision 失效同事务；当前 ETF 执行未结束或最新执行失败时，轮动发布保留上一套完整结果。R06 将接通完整的原日期恢复与计算发布流程。

## 验收与复现

需要隔离 MySQL，`SYNC_RUN_TEST_DSN` 只允许 `candela_sync_test`。以下测试会清理隔离数据，不能指向生产。测试数据不代表真实行情源的历史覆盖。

```sh
export SYNC_RUN_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_sync_test?parseTime=true&loc=UTC'
go -C syncer test -race ./internal/syncrun -count=1
go -C web test -race ./...
go -C feishubot test -race ./...
npm --prefix web/frontend ci
npm --prefix web/frontend run build
cd web/frontend
npm run test:etf
```

完整 syncer 回归同时设置独立 `ROTATION_DAILY_TEST_DSN`、`ROTATION_TEST_DSN`、`ROTATION_RANGE_TEST_DSN`、`CATALOG_TEST_DSN`，分别指向同名 `candela_daily_test`、`candela_rotation_test`、`candela_range_test`、`candela_catalog_test`。不要并发运行会重置同一测试库的套件。

本机浏览器另设 `LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu` 和 `FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`。可用 `npm run test:etf -- --headed` 打开浏览器重放；测试服务仅在 loopback 18081／18089 运行，验证真实签名鉴权、Web 代理、ETF HTTP 和 MySQL，只有行情源／时钟为可控夹具。

已覆盖：20 只 ETF 中 18 成功、2 只因子失败，重试仅补两只因子；重复并发提交；服务中断恢复、取消与已成功步骤保留；旧 HTTP 和收盘报告与管理入口竞争同一批次且等待完成；调用方断开不取消；迁移重放保留已接受 ETF 与指数任务；15:30 提交保留当日截止。完整 syncer/web/feishubot race 与类型、构建、设计检查通过。

浏览器共 41 项通过：ETF 专项 9 项、既有网站 22 项、指数同步 1 项完整流程、固定参考管理 9 项。浏览器覆盖真实提交／失败重试／取消、刷新与重新进入、原批次关联、1440／820／390／320px 布局、字体与键盘焦点、加载超时、空态、错误响应、登录失效和字段错误关联。截图来自隔离测试行情；真实用户试用尚未完成。

[桌面](../design/etf-sync/desktop.png) · [手机](../design/etf-sync/mobile.png) · [部分失败](../design/etf-sync/partial.png)

## 审查与发布状态

规范轴修复字段错误未关联输入框的问题；需求轴修复中断／恢复记录不可查询，以及错误套用指数截止日的问题。复查又发现多语句读取可能复制旧检查点，已改为单条 JOIN，两个审查轴最终复核通过。

本票不单独部署。用户已授权七张轮动任务全部完成并验证后发布至 candlea.cn；R06 的原日期恢复、R07 的历史更正传播与实际发布验收仍待完成。历史 14:45 原始数据无法补取的限制仍如实保留，不能用当下报价补造。
