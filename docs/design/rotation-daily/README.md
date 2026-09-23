# 四标的每日数据 R01 验收

对应 #35，页面基础包含 PR #33 的两个提交。工作区从 origin/master `3c17415375fea02205dcb34a154875f79bb581db` 创建后快进到该页面基础；旧工作区的未提交修改未动。

## 本次实现

后台从已同步日线、截至行情日的复权因子及已验证覆盖读取同一输入快照，复用现有 ER20、YZ20/240、1200 有效值分位及仓位公式。四标的原始数据完整后才整组发布；历史不足、源日期异常和未知缺口保持空值与原因，任一动量不可用时不提供正式排名。

`rotation_daily` 保存每个交易日当前有效收盘结果；与现有行情修订号和发布锁配合，过期计算不能覆盖新输入。失败保留完整结果，后台重试。现有后台发布循环及一次性刷新接通每日结果；新增 GET 不拉行情、不计算、不写库。

- 内部：`GET /api/v1/rotation/daily[?tradeDate=YYYYMMDD]`，API key 保护。
- 网页：`GET /api/rotation/daily`，Cloudflare Access 验证及仅服务端凭证转发。
- 六指标在桌面以表格、手机以标的卡片展示；缺失原因直接可读。每日区与历史回测独立加载、失败和重新读取。
- 重新读取和每 30 秒可见页面状态刷新只读已发布结果；不采集当前行情。

## 数据来源和截图

本目录截图为**隔离测试行情**，页面顶部有标识，不是真实市场表现。每日结果经过真实计算服务、HTTP、MySQL 和网页鉴权代理生成；测试输入为固定价格 100/101/102/103 和可控日历。下方回测使用既有浏览器测试夹具。它们验证界面与链路，不作为策略收益证据。

- [桌面 1440px](desktop.png)
- [手机 390px](mobile.png)
- 820px、320px 及缺数截图由以下测试生成在 `/tmp/candela-daily-*.png`。

## 可复现检查

在独立测试 MySQL 中创建 `candela_daily_test` 和 `candela_range_test`，不能指向生产库。数据库测试入口会校验库名。

```sh
export ROTATION_DAILY_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_daily_test?parseTime=true&loc=UTC'
# R02 接入后，同页回测区使用第二个隔离数据库。
export ROTATION_RANGE_TEST_DSN='root@tcp(127.0.0.1:13316)/candela_range_test?parseTime=true&loc=UTC'
go -C syncer test -race ./internal/rotation -run TestDaily -count=1
npm --prefix web/frontend ci
npm --prefix web/frontend run build
cd web/frontend
npx playwright test --config playwright.daily.config.ts
```

浏览器测试自动启动真实每日服务（18084）及签名登录网页测试服务（18081）；均仅监听 loopback，测试凭证独立于生产，不引入鉴权绕过。完整链路使用隔离 MySQL；读取失败、过期登录等展示边界另用明确的状态夹具覆盖。

本机 Chromium 使用已有运行库：`LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu`、`FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`。其他环境按 Playwright 正常安装浏览器依赖。

验证范围：四只正常、3/4 到齐后第四只补齐、历史不足、无记录、计算失败后恢复、过期计算被拒绝、非法源日期、历史缺口、已有未来行情/因子、查询校验及只读行为。浏览器 7 项覆盖六字段、四种屏宽、无整页溢出、键盘焦点、加载、失败保留、未知值及登录过期；既有浏览器 22 项、设计 5 项通过。构建和类型检查通过，设计保持原有 13 条 orphaned-tokens 警告。

## 尚未完成的后续能力

#36 后端区间统计及十年上限；#37 固定 14:45 采集及记录；#38 阶段切换与 bps 比较；#39 历史日期选择；#40 管理页重试；#41 历史更正传播。当前 14:45 缺失如实展示，不以旧快照或现价补造，也不提供价格差异或变化原因猜测。

用户已授权七票完成并验证后发布到 `candlea.cn`；本 R01 验收没有部署生产或修改生产调度。最终发布仍需完成全部依赖与部署验收。
