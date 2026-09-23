# T09 的 ETF 前置能力：指定区间历史重同步

本变更完成 [#26](https://github.com/fletcherlau/candela/issues/26) 中轮动 [R07 / #41](https://github.com/fletcherlau/candela/issues/41) 所需的 ETF 历史取数入口，依赖 PR #49 的已验证分支栈。它不代表 #26 的申万、中证全指／市场状态部分完成，也不代表 R07 的全部历史结果传播已经完成；不关闭 #26，不部署生产。

## 行为

在原 `/admin/data` 的 ETF 面板选择增量同步或历史重同步。历史模式接受启用名单或指定代码及起止日期闭区间；后端重新获取日线与因子，按主键补缺和更新，既不预先清空，也不改区间外原始记录。源端本次未返回的旧记录保留，原始价格和因子分别存储，不存复权价格。

新提交的内部接口及网页代理分别为 `POST /api/v1/data/etf-syncs` 和 `POST /api/etf-syncs`：

```json
{"codes":["510880.SH"],"mode":"historical","startDate":"20250102","endDate":"20250103"}
```

日期必须有效、开始不晚于结束、结束不晚于提交时北京时间当日。省略模式仍沿用增量同步的旧行为，包括当日 15:00–18:00 取收盘行情。增量提交不能偷偷指定范围；重试及取消继续只接受原任务标识和空请求体。Cloudflare Access、同源／CSRF 和服务密钥隔离保持。

原模式、起止日期和对象持久保存；相同活动对象集合、模式与范围复用原批次，不同范围分别排队。同一 ETF 使用既有对象互斥。历史日线与因子均从指定开始日期取数，不看全表最大日期；各自分段、各自检查点。失败重试仅复制失败对象与原阶段进度；中断重启沿原范围继续，取消保留已提交数据且不会自动恢复。列表和详情显示固定范围、阶段进度及原批次关联。

新迁移 v10 只给 ETF 批次添加模式和原起点；旧批次默认为增量，原对象检查点不变。迁移可在 DDL 已提交而版本记录未写入后重放，已存在列须符合类型、长度、可空性及默认值。原始行情写入与轮动 revision 失效继续在同一事务；局部历史重同步不冒充完整增量覆盖。

## 验证与复现

测试边界沿用真实 HTTP、隔离 MySQL、行情源／时钟及受签名鉴权浏览器。存储结果通过既有 Store 公开读取接口核对，未直接查询内部表断言业务结果；迁移重放使用隔离库故障注入。

新增验证包含：已有更晚数据时补旧洞并修正日线／因子，区间前后原始值不变；模式和区间去重；非法或未来范围拒绝；失败因子仅按原范围重试；历史模式中断恢复、取消保留；迁移重放保留已接受任务；通过轮动每日 HTTP 确认局部同步不会冒充全量覆盖。

```sh
export SYNC_RUN_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_sync_test?parseTime=true&loc=UTC'
export ROTATION_DAILY_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_daily_test?parseTime=true&loc=UTC'
go -C syncer test -race ./internal/syncrun ./internal/rotation -run 'TestETF|TestHistoricalETFHTTP' -count=1
go -C web test -race ./...
npm --prefix web/frontend ci
npm --prefix web/frontend run build
cd web/frontend
npm run test:etf
```

浏览器测试会启动 loopback 18081 网页与 18089 ETF 后端，使用真实签名凭证及隔离数据库；`npm run test:etf -- --headed` 可打开并重现操作。请勿与其他会重置 `candela_sync_test` 的套件并发运行。当前机器另需 `LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu` 和 `FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`。

完整 syncer、web、feishubot race 测试与 syncer 构建、前端类型／构建、design:check 通过。syncer 完整回归使用五个各自隔离测试库；没有生产数据写入。

浏览器共 28 项通过：ETF 14 项、恢复管理 9 项、设计 5 项。覆盖 1440／820／390／320px、键盘、固定范围与刷新、取消、错误／超时、登录失效及日期错误。修复了明确拒绝请求仍显示“可能已接受”的提示；审查进一步要求服务端日期错误关联输入框，已通过白名单 `X-Validation-Field: range` 传递到 `FieldError`，不透传上游详情，日期控件的 `aria-invalid` 和描述同步更新。此拒绝路径再次通过真实 HTTP／MySQL 浏览器验证，受影响的历史 HTTP 和网页竞态测试也通过。

[桌面截图](../design/etf-history/desktop.png) · [320px 手机截图](../design/etf-history/mobile.png)。截图已实际查看，表单、日期范围、长标识及对象详情无整页横向溢出；数据均为隔离测试行情，不代表真实源覆盖。

双轴审查以用户固定基准 `3c17415375fea02205dcb34a154875f79bb581db` 进行。需求轴和规范轴均完成 `3b7c6ee` 最终复核：规范 0 项、需求 0 项；初轮规范轴指出的服务端日期错误字段关联已修复。审查为只读代码审查，测试结果来自上述实际执行。

## 剩余工作

R07 仍需将历史更正传递至受影响及后续已记录收盘日、滑点和完整连续回测，并验证固定 14:45 原输入与首次发布参考不变。现有单日收盘恢复不等同于整个历史区间传播。本文不把原始同步成功称作所有历史轮动结果已更新。

历史 14:45 无原始输入时无法补造；本次仍不增加事后快照、模拟排名或实际账户数据。截图与浏览器使用明确的隔离行情夹具，真实用户试用和生产发布尚未完成。
