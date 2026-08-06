# 四 ETF 动量轮动策略（etf-rotation）

生产策略：**红利（510880）/ 黄金（518880）/ 创业板（159915）/ 纳指（513100）四标的 ER 动量轮动**
策略构成（runbook v1.4，2026-08-06）：ER 动量 + δ=0.005 差距缓冲 + 70/40 分位节流 + 现金停车，无吊灯、无安全阀。

回测口径（2018-07 ~ 2026-08，收盘执行，单边 10bps）：**年化 +29.2%，夏普 1.32，最大回撤 −17.1%**。

## 目录

| 文件 | 内容 |
|---|---|
| `production-runbook.md` | **生产基线每日操作文档（先读这个）** |
| `strategy-spec.md` | 策略说明书（完整研究结论） |
| `rotation7.py` | 生产基线回测（`run(mult=None)`），每日决策的代码实现 |
| `dca_sim.py` | 账户级现金流模拟（定投、佣金/滑点、现金口径），输出 `dca_sim.json` |
| `rotation_valve_test.py` | 安全阀开/关隔离验证（v1.4 移除安全阀的依据） |
| `rotation_div_swap.py` | 红利腿标的替换对比（512890 / 515080） |
| `rotation2.py` ~ `rotation6.py`、`backtest.py`、`throttle_analysis.py` 等 | 研究脚本（主回测、窗口、样本外、δ 网格、walk-forward、归因） |
| `*.json` | 各脚本输出的回测/状态数据（页面驾驶舱与看板数据源） |
| `dca-dashboard/` | 定投资金曲线看板（Vite 静态页：资金曲线 + 持仓色带 + 回撤线） |

## 运行

```bash
export TUSHARE_TOKEN=<你的 tushare 令牌>   # 所有脚本从环境变量读取，不入库
python3 rotation7.py                        # 生产基线回测 → chand.json
python3 dca_sim.py                          # 账户模拟 → dca_sim.json
# dca_sim.py 可选环境变量：INIT_CAP（首期）、MONTHLY（月投）、SAFETY_VALVE（1=旧版含安全阀）、
# CASH_YIELD（现金年化，默认 0）、OUT_JSON（输出文件名）

cd dca-dashboard && npm install && npm run dev   # 看板本地预览
```
