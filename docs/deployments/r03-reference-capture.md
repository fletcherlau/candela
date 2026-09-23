# R03 · 固定 14:45 原始参考采集

对应 [#37](https://github.com/fletcherlau/candela/issues/37)、[PRD #34](https://github.com/fletcherlau/candela/issues/34)。本票保存计算输入并提供管理查询；六指标发布与每日页面切换由 R04 接续，失败重试入口由 R06 接续。原始数据 4/4 不等于参考已发布，也不保证历史窗口足以计算全部指标。

## 时点与原始依据

目标固定为交易日北京时间 14:45:00，接受行情的窗口为 `[14:45:00, 14:46:00)`。这是采集容差，不把每只标的的成交时间声称为 14:45:00：请求时间、实际收到时间都必须在窗口内；源完整时间必须属于同一交易日和窗口，且不得晚于实际收到时间。管理页分别显示计划、来源、请求、采集时间。14:45 前不能提交；迟到或跨日恢复不再请求现价，记录无法补取。

首次有效 OHLC/Latest 与当时的历史日线、逐日截至日因子、当日因子、日历证据、覆盖截止日、参数版本共同冻结。查询先按目标日截断历史再取窗口，不使用未来因子。原始缺数保留为空，留给后续发布模块作完整性判断。每只成功对象单独提交；失败对象不影响已保存对象。来源不可信的旧 `intraday_snapshot` 不迁入新档案。

`rotation_capture_run` 的交易日主键就是固定参考任务身份；`rotation_reference_input` 的交易日与标的主键保存首次有效输入。与 T03 同样采用数据库时钟租约、递增执行权和提交前校验，原任务行串行保护领取、心跳、检查点和完成。恢复不重新选日期、参数或已保存输入。该执行只采集固定参考，不取得 ETF 日线同步执行权或另建日线同步入口。

## 当前接入能力证据

- `syncer/gtimg.go` 的 `FetchRealtime` 只请求 `/q=<codes>`，入参只有代码；解析第 30 字段的完整 `yyyyMMddHHmmss`，验证日期和时分秒后保留原值及 `gtimg` 来源。
- `core.RealtimeSource` 没有历史日期或历史时点参数；当前代码没有可验证的 14:45 历史报价适配器。
- Tushare 日线及因子不能重建某天 14:45 的真实价格，现有盘中覆盖表也不能证明原始采集时点。
- 因此，**当前集成不具备原时点历史补取能力**。这不是对供应商全部产品能力的断言；没有新购数据或用收盘价冒充参考。
- 接受规则由真实 HTTP / 隔离 MySQL 和受控源测试验证。尚未在正式交易时段证明该源连续可用，不能把测试成功解释为每日必定取得 4/4。

## 接口与外部调度

服务内网接口均沿用 `X-Api-Key`：

- `POST /api/v1/rotation/reference-captures`，仅接受 `{"tradeDate":"YYYYMMDD"}`。首次接受 202，重复 200；接受成功不等于采集或发布成功。
- `GET /api/v1/rotation/reference-captures`：最近 50 条任务。
- `GET /api/v1/rotation/reference-captures/YYYYMMDD`：任务和四标的原始依据。

网页 `/admin/data` 经过 Cloudflare Access，再由 `/api/rotation/reference-captures[/YYYYMMDD]` 只读代理访问。禁止网页 POST 和任意子路径／查询；不转发浏览器令牌，服务密钥只在服务端。读取和可见页面的 30 秒状态刷新不请求行情。

以下为**待启用示例**，本票没有修改生产 cron。部署本轮全部任务后再一起启用并验证。触发前需已有可信缓存交易日历；缓存不存在、非交易日返回 409，不按星期推测。

在权限 0600 的 `/etc/candela/reference-capture.curl` 中设置内网地址与密钥，避免写进仓库或截图：

```text
url = "http://127.0.0.1:8888/api/v1/rotation/reference-captures"
header = "X-Api-Key: <部署环境密钥>"
header = "Content-Type: application/json"
```

外部脚本示例（不包含密钥）：

```sh
#!/bin/sh
set -eu
capture_date=$(TZ=Asia/Shanghai date +%Y%m%d)
printf '{"tradeDate":"%s"}' "$capture_date" |
  curl --config /etc/candela/reference-capture.curl \
    --silent --show-error --fail --max-time 15 \
    --output /dev/null --data-binary @-
```

如果 cron 所在主机时区经核实为 UTC，`45 6 * * * /部署脚本绝对路径` 对应北京时间 14:45；主机若为 Asia/Shanghai，则使用 `45 14 * * *`。日期始终由脚本显式使用 Asia/Shanghai。每天触发，由后端缓存日历决定是否接受，不能仅以周一至周五代替交易日历。非交易日与日历缺失的 409 需结合任务查询区分；重复提交仍沿用同一原目标。服务内每秒只处理已接受任务，不新增每日业务定时器。

## 验收范围

采集接口验证首次冻结、并发重复提交、读取无源请求、旧信号覆盖表隔离、后续日线／因子更正隔离、部分成功、错日／过早／超前源时间、窗口外响应、原目标恢复、旧执行权迟到不能提交。管理浏览器使用生产路由、真实 worker、隔离数据库及签名鉴权；只有时间和源输入受控。

截图、可复现命令及剩余能力见 [管理页验收](../design/rotation-capture/README.md)。R04 参考发布、R05 历史每日数据、R06 管理重试和 R07 更正仍不在本票中声称完成；不改变现有机器人操作建议或历史模拟算法。
