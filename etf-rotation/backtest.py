"""回测：高波动分位 + 深跌后的反弹策略，对比两种触发方式。

状态层：YZ 1200 日分位 >= 80% 且 收盘价较 20 日最高价回撤 <= -3%
触发 A：RSI(6) 回穿 30（昨日 <=30 今日 >30）且 收盘 > 前日最高
触发 B：收盘价上穿 MA5（昨收 <= 昨 MA5 且 今收 > 今 MA5）
离场（两种触发共用）：收盘跌破 MA5，或收盘创 10 日新低
执行：信号日收盘判定，次日开盘价成交；单边成本 6 bps（佣金+滑点）
输出 backtest.json 供页面展示。
"""
import json
from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).parent
d = json.loads((HERE / "518880_vol.json").read_text())
study = json.loads((HERE / "study.json").read_text())

COST = 0.0006          # 单边 6 bps
PCT_TH = 80.0          # YZ 分位阈值
DD_TH = -0.03          # 深跌阈值：较 20 日高点回撤 -3%
RSI_N, RSI_TH = 6, 30.0
MA_ENTRY_EXIT = 5
STOP_LOOKBACK = 10

dates = pd.Series(d["dates"])
o = pd.Series([r[0] for r in d["ohlc"]], dtype=float)
h = pd.Series([r[1] for r in d["ohlc"]], dtype=float)
l = pd.Series([r[2] for r in d["ohlc"]], dtype=float)
c = pd.Series([r[3] for r in d["ohlc"]], dtype=float)
pct = pd.Series([np.nan if v is None else float(v) for v in study["pct_series"]["pct"]])

# --- 指标 ---
delta = c.diff()
gain = delta.clip(lower=0).ewm(alpha=1 / RSI_N, adjust=False).mean()
loss = (-delta.clip(upper=0)).ewm(alpha=1 / RSI_N, adjust=False).mean()
rsi = 100 - 100 / (1 + gain / loss)
ma5 = c.rolling(MA_ENTRY_EXIT).mean()
high20 = h.rolling(20).max()
dd20 = c / high20 - 1
low10 = l.rolling(STOP_LOOKBACK).min()

setup = (pct >= PCT_TH) & (dd20 <= DD_TH)

trig_a = (rsi.shift(1) <= RSI_TH) & (rsi > RSI_TH) & (c > h.shift(1)) & setup
trig_b = (c.shift(1) <= ma5.shift(1)) & (c > ma5) & setup

start_idx = int(pct.first_valid_index())  # 与研究样本对齐


def run(trigger: pd.Series):
    """返回 (每日策略收益 Series, 交易列表)"""
    n = len(c)
    ret = np.zeros(n)
    pos = False
    entry_i = -1
    trades = []
    i = start_idx
    while i < n - 1:
        if not pos:
            if bool(trigger.iloc[i]):
                j = i + 1  # 次日开盘入场
                ret[j] += c.iloc[j] / o.iloc[j] - 1 - COST
                pos, entry_i = True, j
                i = j
                continue
        else:
            # 离场：收盘跌破 MA5 或创 10 日新低 → 次日开盘离场
            if c.iloc[i] < ma5.iloc[i] or c.iloc[i] < low10.iloc[i]:
                j = i + 1
                ret[j] += o.iloc[j] / c.iloc[i] - 1 - COST
                pos = False
                trades.append({
                    "entry": dates.iloc[entry_i], "exit": dates.iloc[j],
                    "ret": float(np.prod(1 + ret[entry_i:j + 1]) - 1),
                    "days": j - entry_i,
                })
                i = j
                continue
            ret[i + 1] += c.iloc[i + 1] / c.iloc[i] - 1
        i += 1
    if pos:  # 样本末端仍持仓，按最后收盘价标记
        trades.append({
            "entry": dates.iloc[entry_i], "exit": dates.iloc[-1] + "(持有中)",
            "ret": float(np.prod(1 + ret[entry_i:]) - 1), "days": n - 1 - entry_i,
        })
    return pd.Series(ret), trades


def metrics(ret: pd.Series, trades):
    r = ret.iloc[start_idx:]
    n = len(r)
    equity = (1 + r).cumprod()
    ann = float(equity.iloc[-1] ** (240 / n) - 1)
    sharpe = float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0
    dd = (equity / equity.cummax() - 1)
    maxdd = float(dd.min())
    wins = [t for t in trades if t["ret"] > 0]
    losses = [t for t in trades if t["ret"] <= 0]
    m = {
        "ann": round(ann * 100, 2),
        "sharpe": round(sharpe, 2),
        "maxdd": round(maxdd * 100, 2),
        "total": round((float(equity.iloc[-1]) - 1) * 100, 1),
        "trades": len(trades),
        "winrate": round(len(wins) / len(trades) * 100, 1) if trades else None,
        "avg_days": round(float(np.mean([t["days"] for t in trades])), 1) if trades else None,
        "avg_win": round(float(np.mean([t["ret"] for t in wins])) * 100, 2) if wins else None,
        "avg_loss": round(float(np.mean([t["ret"] for t in losses])) * 100, 2) if losses else None,
    }
    return m, equity, dd


ret_a, trades_a = run(trig_a.fillna(False))
ret_b, trades_b = run(trig_b.fillna(False))
ret_bh = c.pct_change().fillna(0)

m_a, eq_a, dd_a = metrics(ret_a, trades_a)
m_b, eq_b, metrics_dd_b = metrics(ret_b, trades_b)
m_bh, eq_bh, dd_bh = metrics(ret_bh, [])

out_dates = dates.iloc[start_idx:].tolist()
payload = {
    "meta": {
        "pct_th": PCT_TH, "dd_th": DD_TH, "rsi_n": RSI_N, "rsi_th": RSI_TH,
        "ma": MA_ENTRY_EXIT, "stop_lookback": STOP_LOOKBACK,
        "cost_bps_side": COST * 1e4, "start": out_dates[0], "end": out_dates[-1],
        "n_days": len(out_dates),
    },
    "dates": out_dates,
    "variants": [
        {
            "key": "rsi", "name": f"RSI({RSI_N}) 回穿 {int(RSI_TH)} + 前高确认",
            "metrics": m_a, "trades": trades_a,
            "equity": eq_a.round(4).tolist(),
            "dd": dd_a.round(4).tolist(),
        },
        {
            "key": "ma5", "name": f"MA{MA_ENTRY_EXIT} 上穿",
            "metrics": m_b, "trades": trades_b,
            "equity": eq_b.round(4).tolist(),
            "dd": metrics_dd_b.round(4).tolist(),
        },
        {
            "key": "bh", "name": "买入持有（基准）",
            "metrics": {**m_bh, "trades": 1, "winrate": None, "avg_days": None, "avg_win": None, "avg_loss": None},
            "trades": [],
            "equity": eq_bh.round(4).tolist(),
            "dd": dd_bh.round(4).tolist(),
        },
    ],
}
(HERE / "backtest.json").write_text(json.dumps(payload, ensure_ascii=False))

hdr = f"{'策略':<28}{'年化%':>8}{'夏普':>7}{'最大回撤%':>10}{'交易':>6}{'胜率%':>7}{'均持仓':>7}{'均盈%':>8}{'均亏%':>8}"
print(hdr)
for v in payload["variants"]:
    m = v["metrics"]
    wr = m["winrate"] if m["winrate"] is not None else "-"
    ad = m["avg_days"] if m["avg_days"] is not None else "-"
    aw = m["avg_win"] if m["avg_win"] is not None else "-"
    al = m["avg_loss"] if m["avg_loss"] is not None else "-"
    print(f"{v['name']:<28}{m['ann']:>8}{m['sharpe']:>7}{m['maxdd']:>10}{m['trades']:>6}{wr:>7}{ad:>7}{aw:>8}{al:>8}")
print("\n触发 A 交易明细（前 10 笔）:")
for t in trades_a[:10]:
    print(f"  {t['entry']} → {t['exit']}  {t['ret']*100:+.2f}%  {t['days']}d")
