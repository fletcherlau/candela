"""轮动策略 v3：分位窗口对比 + 波动率目标法（vol targeting）vs 分位节流。

方向 1：YZ 分位窗口 240 / 600 / 1200，同参数（70/40）同评估窗口对比
方向 2：波动率目标法 w = clip(σ_target / σ_YZ, 下限, 1)，σ_target ∈ {15,18,21}% × 下限 {30,40,50}%
全部：收盘执行，单边 10bps，log 信号。输出 ext.json 供页面展示。
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
pct_maps = {pw: (pct1200 if pw == 1200 else {c: pct_of(ind[c]["yz"], pw) for c in codes}) for pw in (240, 600, 1200)}


def ranks(t):
    vals = {c: ind[c]["score"].iloc[t] for c in codes}
    vals = {k: v for k, v in vals.items() if not np.isnan(v)}
    return sorted(vals, key=vals.get, reverse=True)


def metrics(ret, start):
    r = pd.Series(ret, index=dates).iloc[start + 1:]
    eq = (1 + r).cumprod()
    return {
        "ann": round(float(eq.iloc[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0, 2),
        "maxdd": round(float((eq / eq.cummax() - 1).min()) * 100, 2),
        "total": round((float(eq.iloc[-1]) - 1) * 100, 1),
    }, eq


def run_throttle(pct_map, pct0, floor):
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        r = ranks(t)
        if not r:
            continue
        tgt = r[0]
        p = pct_map[tgt].iloc[t]
        w_new = 1.0 if np.isnan(p) else float(np.clip(1 - (1 - floor) * (p - pct0) / (100 - pct0), floor, 1.0))
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos = tgt
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret


def run_voltarget(sigma, wmin):
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        r = ranks(t)
        if not r:
            continue
        tgt = r[0]
        yz = ind[tgt]["yz"].iloc[t]
        w_new = 1.0 if (np.isnan(yz) or yz <= 0) else float(np.clip(sigma / yz, wmin, 1.0))
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos = tgt
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret


# 基线（不节流）
def run_base():
    ret = np.zeros(n)
    pos = None
    for t in range(align_start + 1, n):
        if pos:
            ret[t] += ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1
        r = ranks(t)
        if not r:
            continue
        tgt = r[0]
        if tgt != pos:
            ret[t] -= (2 if pos else 1) * COST
            pos = tgt
    return ret


m_base, _ = metrics(run_base(), align_start)
print(f"基线（不节流）: {m_base}")

pctw_rows = []
for pw in (240, 600, 1200):
    m, _ = metrics(run_throttle(pct_maps[pw], 70.0, 0.4), align_start)
    m["pct_w"] = pw
    pctw_rows.append(m)
    print(f"分位窗口 {pw}: {m}")

vt_rows = []
for sigma in (0.15, 0.18, 0.21):
    for wmin in (0.3, 0.4, 0.5):
        m, _ = metrics(run_voltarget(sigma, wmin), align_start)
        row = {"sigma": int(sigma * 100), "wmin": int(wmin * 100), **m}
        vt_rows.append(row)
        print(f"波动率目标 σ*={int(sigma*100)}% 下限{int(wmin*100)}%: {m}")

payload = {
    "meta": {"start": dates[align_start + 1], "end": dates[-1], "cost_bps_side": COST * 1e4,
             "base": m_base, "throttle_70_40_1200": pctw_rows[-1]},
    "pctw": pctw_rows,
    "voltarget": vt_rows,
}
(HERE / "ext.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved ext.json")
