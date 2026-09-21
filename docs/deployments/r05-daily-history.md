# R05 · 每日档案查询

对应 [#39](https://github.com/fletcherlau/candela/issues/39)，承接 R04 的两时点读取契约，无新增数据库迁移或生产调度变更。

## 接口

`GET /api/v1/rotation/daily/dates` 使用服务密钥鉴权；网页只读代理为 `/api/rotation/daily/dates`，仍要求有效 Access 登录。

可选 `before=YYYYMMDD` 为排他的较早日期游标，`limit` 默认 30，合法范围 1–100。拒绝非法、未来、重复及未知参数。响应包含 `status`、后端 `currentDate`、真实 `earliestDate`／`latestDate`、`dates` 及 `nextBefore`；日期项给出 `tradeDate`、`reference` 和 `close` 状态。来源是 `rotation_daily` 与 `rotation_capture_run` 的日期并集，包含失败尝试，按降序去重，既不从日历推导档案也不从回测日期补造参考。

起止边界、分页项、两时点状态和修订读取同一只读事务快照；页面读取不调用行情源或写入任务。新增记录不使日期游标指向重复页。后端保留已有的显式 `GET /api/v1/rotation/daily?tradeDate=YYYYMMDD`，无记录时仍返回请求日期及空值；不自动换日。

## 页面与验证

历史控件独立于回测组件，重新读取和可见页低频刷新带上已选择日期；用户显式返回最新才清空选择。列表读取独立超时和重试，异常不影响每日和回测。页面检查返回日期，拒绝错日响应，快速切换取消旧请求并清除旧日期内容。

真实接口、隔离数据库、签名鉴权浏览器、截图与复现入口见 [历史日期验收](../design/rotation-history/README.md)。生产发布时间安排仍跟随全部七张工单完成后的统一验收，本票没有部署或改 cron。
