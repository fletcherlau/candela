# 四标的轮动正式页面验收 · 2026-09-20

本轮将 DESIGN.md v1.0 应用到 `/strategies/four-etf-rotation`，属于 issue #31 的真实历史回测页迁移阶段。设计方案未重新选型，算法、信号计算、账户和生产部署未改动。

## 页面与数据

- ResearchTheme 包裹本页及 Dialog；现有 shadcn/ui 负责控件，ECharts 负责收益、回撤及仓位图。首页、市场页、管理页保留原主题。
- 首屏提供策略身份、数据截至交易日、历史模型标的／ETF 权重／现金、与前一回测交易日的字段比较。发布更新时间单独显示为北京时间。权重变化不解释为成交、排名或调仓原因。
- 默认近一年，保留近三月／近三年／全部、日期、滑块、拖动缩放、平移、四 ETF 对照；图表与指标共享区间和日期光标。逐日查看支持键盘和手机。
- 沿用 `/api/rotation/backtest`。完整历史从初始净值 1 计算，包含首次买入成本；局部区间按起点净值归一化。区间回撤以区间内峰值为基准。费用与版本读取同一个结果，未知版本不冒用已知版本的规则。
- ready、pending、computing、syncing、failed、stale 和未知状态均有文本；更新失败可以保留历史结果。未知数值展示 `—`，曲线断开；净值缺口后的回撤不再假定已知。空数组、空结果和单日区间分别处理。

## 截图证据

以下是正式页面加载真实已发布历史数据后的截图，不是设计样板或生成数据：

- [桌面，1440px](desktop.png)
- [手机，390px](mobile.png)
- [1440 / 820 / 390 / 320px 浏览器检查](browser-checks.json)

预览采用既有 `production-result.json` 的真实发布快照：`updatedAt=2026-09-16T03:39:47Z`，有效历史 2018-07-26 至 2026-09-15，共 1,976 个交易日，版本 `v1.4-close-5pp-cash0-cost10-v1`，单边 10bps。默认一年是 2025-09-15 至 2026-09-15、243 个交易日；截图收益 11.24%、最大回撤 -12.11% 由该快照计算。它们是截图证据，不是硬编码页面指标，也不代表当前生产数据最新状态。

本轮当前生产接口的凭证读取未获自动审批，未进一步读取生产密钥或调用该接口。正式代码始终请求正常鉴权接口；快照仅进入隔离测试服务。未将该 JSON 或凭证提交到仓库。

## 可打开的本地预览

本任务预览服务：`http://127.0.0.1:18083/strategies/four-etf-rotation`。服务只监听 loopback，保留真实应用的 JWT 签名、issuer、audience、过期校验。测试专用入口用签名的本地测试凭证建立 HttpOnly 预览会话；无凭证请求仍返回 401。服务只读，不连接生产上游，也不进入正式二进制。

复现（准备一份现有已发布 View JSON，包含 status / updatedAt / result）：

```sh
npm --prefix web/frontend ci
npm --prefix web/frontend run build
CANDELA_BROWSER_TEST=1 CANDELA_PREVIEW_RESULT=/absolute/path/to/published-result.json \
  go -C web test ./internal/server -run TestBrowserHarness -count=1 -timeout=24h
```

另一个终端读取 `/tmp/candela-rotation-preview-url`，在浏览器打开里面的登录链接，再使用上述稳定服务地址。该凭证仅用于隔离本地 issuer，24 小时过期；重启服务会生成新会话，不复用临时转发端口。不要把测试凭证用于生产或公开服务。

## 验证结果

- `npm run design:check` / `npm run build`：通过；规范仍为原有 13 条 orphaned-tokens 警告，无新增规范告警。Vite 保留 ECharts 大 chunk 提示；轮动路由已懒加载，不进入首页初始脚本。
- `npm run test:e2e`：22 项通过。覆盖鉴权、原网站导航／管理页、默认一年、完整历史费用起点、范围与回撤联动、日期／拖动缩放、对照勾选、逐日键盘操作、各数据状态、缺失字段和未知规则版本。
- `DESIGN_TEST_PORT=4176 CI=1 npm run test:design`：5 项通过，包含主题隔离、弹窗与四种宽度。可选端口避免影响其他工作区正在运行的样板。测试在交互前等待实际字体加载，避免字体布局变化导致点击错位。
- `go -C web test -race ./...`：通过，覆盖原鉴权与新响应 nonce。真实快照预览四种宽度无整页溢出、无浏览器异常及 CSP 错误，实际中文标题字体为 Noto Serif SC 300，数字为 Source Serif 4。
- 弹窗焦点约束、Escape 关闭、焦点恢复、手机边界、图例键盘空格与逐日方向键经过浏览器检查。

两项工程适配：字体改为同源静态文件，避免 Vite 内嵌字体被既有 CSP 拦截；Radix 弹窗滚动锁通过每次 HTML 响应的随机 style nonce 授权，脚本策略不变，不加入 unsafe-inline。生产 Access 校验和上游密钥边界保持原样。

环境备注：此工作区 Chromium 需要 `LD_LIBRARY_PATH=/tmp/candela-browser-libs/root/usr/lib/x86_64-linux-gnu` 与 `FONTCONFIG_FILE=/tmp/candela-browser-fonts.conf`；复用已有本地依赖，未修改系统或其他工作区。内置浏览器工具受 sandbox 限制，实际检查使用本地 Playwright Chromium。

## 后续范围

- 每日排名、评分与变化原因：需要已发布信号的只读数据契约、截止日和策略版本，不从历史权重变化推导。
- 定投资金视图：需要投入计划、现金流、费用与统计口径，不混用旧实验。
- 默认范围：本轮保留近一年和完整历史入口；旧简报要求默认完整历史，仍待业务确认。
- 用户验收及目标用户试用尚未完成；本记录是实现和自动／人工浏览器检查，不代替用户反馈。issue #31 保持开放，未自动部署生产。
