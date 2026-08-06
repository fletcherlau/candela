"""轮动策略 v6：walk-forward 滚动样本外选择 δ（带安全阀 + 70/40 节流）。

每个 OOS 期（120 个交易日）：用过去 750 个交易日的各 δ 日收益序列计算训练夏普，
选夏普最高的 δ 应用于下一期，严格样本外拼接。对照：固定 δ=0（无缓冲）、固定 δ=0.005。
输出 wf.json。
"""
import json
import os
import sys
from pathlib import Path

import numpy as np
import pandas as pd
import tushare as ts

TOKEN = os.environ.get("TUSHARE_TOKEN")
if not TOKEN:
    sys.exit("TUSHARE_TOKEN required")

HERE = Path(__file__).parent
CODES = {"510880.SH": "红利ETF", "518880.SH": "黄金ETF", "159915.SZ": "创业板ETF", "513100.SH": "纳指ETF"}
MOM, COST = 20, 0.001
DELTAS = [0.0, 0.002, 0.005, 0.01, 0.02]
TRAIN, STEP = 750, 120

pro = ts.pro_api(TOKEN)
panels = {}
for code in CODES:
    df = pro.fund_daily(ts_code=code, start_date="20100101", end_date="20260805").sort_values("trade_date")
    adj = pro.adj_factor(ts_code=code, start_date="20100101", end_date="20260805")
    adj = adj.sort_values("trade_date")[["trade_date", "adj_factor"]] if adj is not None and not adj.empty else None
    df["adj_factor"] = 1.0 if adj is None else df.merge(adj, on="trade_date", how="left")["adj_factor"].ffill().bfill().fillna(1.0)
    df["date"] = pd.to_datetime(df["trade_date"]).dt.strftime("%Y-%m-%d")
    for c in ["open", "high", "low", "close"]:
        df[c] = df[c].astype(float) * df["adj_factor"]
    panels[code] = df.set_index("date")[["open", "high", "low", "close"]]

common = sorted(set.intersection(*[set(p.index) for p in panels.values()]))
px = {c: panels[c].loc[common] for c in CODES}
dates = pd.Index(common)
n = len(dates)
codes = list(CODES)


def yz_vol(o, h, l, c, win=20):
    k = 0.34 / (1.34 + (win + 1) / (win - 1))
    o_ret, c_ret = np.log(o / c.shift(1)), np.log(c / o)
    rs = np.log(h / c) * np.log(h / o) + np.log(l / c) * np.log(l / o)
    var = (o_ret.rolling(win).var(ddof=1) + k * c_ret.rolling(win).var(ddof=1) + (1 - k) * rs.rolling(win).mean())
    return np.sqrt(var * 240)


def pct_of(yz, pct_w):
    yv = yz.values
    pct = np.full(len(yv), np.nan)
    for t in range(len(yv)):
        if np.isnan(yv[t]):
            continue
        w = yv[max(0, t - pct_w + 1):t + 1]
        w = w[~np.isnan(w)]
        if len(w) < pct_w:
            continue
        pct[t] = ((w < yv[t]).sum() + 0.5 * (w == yv[t]).sum()) / len(w) * 100.0
    return pd.Series(pct, index=yz.index)


ind = {}
for code in codes:
    p = px[code]
    o, h, l, c = p["open"], p["high"], p["low"], p["close"]
    m4 = (o + h + l + c) / 4
    logm = np.log(m4)
    num = logm - logm.shift(MOM - 1)
    den = logm.diff().abs().rolling(MOM - 1).sum()
    ind[code] = {"c": c, "score": num.abs() / den * num, "yz": yz_vol(o, h, l, c)}

pct1200 = {c: pct_of(ind[c]["yz"], 1200) for c in codes}
align_start = max(pct1200[c].index.get_loc(pct1200[c].first_valid_index()) for c in codes)


def throttle_w(p):
    if np.isnan(p):
        return 1.0
    return float(np.clip(1 - 0.6 * (p - 70) / 30, 0.4, 1.0))


def run_margin(delta):
    """70/40 节流 + margin + 安全阀，全窗口日收益"""
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        vals = {c: ind[c]["score"].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        if pos is not None and tgt != pos:
            held_losing = vals.get(pos, 0) < 0
            if not held_losing and vals[tgt] - vals.get(pos, -np.inf) <= delta:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t])
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos = tgt
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret


series = {d: run_margin(d) for d in DELTAS}

oos_start = align_start + 1 + TRAIN
periods = []
wf_ret = []
p = oos_start
while p < n:
    q = min(p + STEP, n)
    best_d, best_s = None, -np.inf
    sharpes = {}
    for d in DELTAS:
        r = series[d][p - TRAIN:p]
        s = float(np.mean(r) / np.std(r) * np.sqrt(240)) if np.std(r) > 0 else -np.inf
        sharpes[d] = round(s, 2)
        if s > best_s:
            best_s, best_d = s, d
    periods.append({"start": dates[p], "end": dates[q - 1], "chosen": best_d, "train_sharpe": round(best_s, 2)})
    wf_ret.extend(series[best_d][p:q])
    p = q

wf_ret = np.array(wf_ret)
span_dates = dates[oos_start:]


def span_metrics(r):
    eq = np.cumprod(1 + r)
    return {
        "ann": round(float(eq[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(np.mean(r) / np.std(r) * np.sqrt(240)) if np.std(r) > 0 else 0.0, 2),
        "maxdd": round(float((eq / np.maximum.accumulate(eq) - 1).min()) * 100, 2),
    }


m_wf = span_metrics(wf_ret)
m_fix0 = span_metrics(series[0.0][oos_start:])
m_fix005 = span_metrics(series[0.005][oos_start:])

chosen_counts = {d: sum(1 for x in periods if x["chosen"] == d) for d in DELTAS}

payload = {
    "meta": {"oos_start": dates[oos_start], "end": dates[-1], "train": TRAIN, "step": STEP,
             "cost_bps_side": COST * 1e4, "deltas": DELTAS},
    "periods": periods,
    "chosen_counts": chosen_counts,
    "metrics": {"walkforward": m_wf, "fixed_0": m_fix0, "fixed_0005": m_fix005},
}
(HERE / "wf.json").write_text(json.dumps(payload, ensure_ascii=False))

print(f"OOS 区间: {dates[oos_start]} ~ {dates[-1]}（{len(span_dates)} 天，{len(periods)} 期）")
print(f"{'walk-forward(选δ)':<20}{m_wf}")
print(f"{'固定 δ=0':<20}{m_fix0}")
print(f"{'固定 δ=0.005':<20}{m_fix005}")
print(f"选择分布: {chosen_counts}")
for x in periods:
    print(f"  {x['start']} ~ {x['end']}  选 δ={x['chosen']}  训练夏普 {x['train_sharpe']}")
