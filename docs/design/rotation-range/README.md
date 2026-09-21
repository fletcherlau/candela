# 四标的回测区间 R02 验收

对应 #36。本工作区从最新 origin/master `3c17415375fea02205dcb34a154875f79bb581db` 建立，复用 PR #33 和 R01 PR #42。R02 独立提交；未修改原工作区的未提交内容。

## 最终行为

新增只读 `GET /api/v1/rotation/backtest/range` 及网页代理 `/api/rotation/backtest/range`，保留原 `/backtest` 的调用兼容性。默认观察最新发布交易日向前一个日历年；自定义 `start=YYYYMMDD&end=YYYYMMDD` 最长十个日历年，非法、未来和超限参数返回 400。区间边界截取实际重叠历史；底层完整连续模型和预热历史不被截断。

后端从同一已发布结果生成收益、回撤、四条对照、摘要和缺数标志。完整历史首日以净值 1 计入首次费用；其他范围以选中首日净值为基准。净值缺口后的回撤及完整区间最大回撤保持未知；单日不输出区间收益或最大回撤汇总。权重、现金、费用、换手和停牌字段仍来自原模型。

前端已移除 `rangeSeries` 和 `holdingChange` 的业务计算。日期、拖动缩放、范围滑块及预设区间均读取后端视图；数字格式化与图表绘制仍在前端。迟到响应不能覆盖新选择；请求失败保留原结果及其真实观察日期，可重新读取所选范围。无重叠历史显示空态，可返回近一年。每日日期与回测范围独立。

## 数据与截图

[桌面 1440px](desktop.png) · [手机 390px](mobile.png)

截图使用明确标识的隔离测试行情，不是真实市场收益。回测夹具先保存到独立 MySQL，再通过真实区间接口、鉴权代理和网页展示；未连接交易日历时按实际状态提示滞后。此组截图中的每日区未接入，故显示空态。R01 的完整每日区另有独立验收，两个区域的组合测试也通过。

## 验证

- 后端真实 HTTP/MySQL：初始费用、相对基准、四条对照及缺数、连续持仓不重置、默认一年、十年上限、单日／无交集、非法日期和只读滞后提示。
- 新增浏览器 9 项：后端读取、快速切换与故意迟到响应、失败恢复、登录过期、十年上限、覆盖改变导致空区间，以及 1440 / 820 / 390 / 320px 与键盘／图例联动。
- 原浏览器 22 项、每日链路 7 项、设计 5 项通过；syncer 全量 race（四个隔离数据库）和 web 全量 race 通过。类型检查与构建通过，保留原有 13 条设计 token 提示和图表 chunk 提示。

```sh
# 数据库名称是测试保护条件，不能改成生产库。
export ROTATION_DAILY_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_daily_test?parseTime=true&loc=UTC'
export ROTATION_RANGE_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_range_test?parseTime=true&loc=UTC'
go -C syncer test -race ./internal/rotation -run TestRange -count=1
npm --prefix web/frontend ci
npm --prefix web/frontend run build
cd web/frontend
npm run test:range
npm run test:e2e
npm run test:daily
```

浏览器测试使用仅编入测试的 loopback 服务：18081 签名登录网页、18085 已发布回测夹具及真实区间 API；每日测试另启动 18084。18085 的夹具写入仅存在于测试二进制并要求测试密钥，不加入生产服务或网页代理。两个测试数据库需先创建。

本机 Chromium 沿用 `LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu` 和 `FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`；其他环境按正常 Playwright 依赖安装运行。

## 后续范围

#37–#41 的固定 14:45、阶段切换、bps 对比、历史日期、管理重试和更正传播仍需完成。本票没有生产部署；用户已授权全部七票验证后部署至 candlea.cn。
