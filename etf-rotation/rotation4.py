"""轮动策略 v4：样本外稳健性验证。

前半段 H1 = 2018-07 ~ 2022-07（定参期），后半段 H2 = 2022-07 ~ 2026-08（验证期）。
检验：基线 / 节流网格(3×3 @1200) / 分位窗口(240/600/1200 @70/40) / 波动率目标(3×3) 在两段的
年化、夏普、最大回撤是否一致地优于基线，以及最优参数区是否跨期稳定。
输出 oos.json。
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
SPLIT = "2022-07-27"

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

h1_lo, h1_hi = dates[align_start + 1], SPLIT
h2_lo, h2_hi = SPLIT, dates[-1]


def ranks(t):
    vals = {c: ind[c]["score"].iloc[t] for c in codes}
    vals = {k: v for k, v in vals.items() if not np.isnan(v)}
    return sorted(vals, key=vals.get, reverse=True)


def metrics_range(ret, d0, d1):
    r = pd.Series(ret, index=dates)
    m = (r.index >= d0) & (r.index < d1) if d1 < dates[-1] else (r.index >= d0) & (r.index <= d1)
    r = r[m]
    eq = (1 + r).cumprod()
    return {
        "ann": round(float(eq.iloc[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0, 2),
        "maxdd": round(float((eq / eq.cummax() - 1).min()) * 100, 2),
    }


def run(weight_fn):
    """weight_fn(code, t) -> 仓位。None 表示满仓。"""
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        r = ranks(t)
        if not r:
            continue
        tgt = r[0]
        w_new = weight_fn(tgt, t)
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos = tgt
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret


def two_halves(ret):
    return {"h1": metrics_range(ret, h1_lo, h1_hi), "h2": metrics_range(ret, h2_lo, h2_hi)}


rows = []

ret_base = run(lambda c, t: 1.0)
m = two_halves(ret_base)
rows.append({"group": "base", "label": "基线（不节流）", **m})
print(f"{'基线':<28} H1 {m['h1']}  H2 {m['h2']}")

# 节流网格 @1200
for pct0 in (50.0, 60.0, 70.0):
    for floor in (0.4, 0.5, 0.6):
        def wf(c, t, p0=pct0, fl=floor):
            p = pct_maps[1200][c].iloc[t]
            return 1.0 if np.isnan(p) else float(np.clip(1 - (1 - fl) * (p - p0) / (100 - p0), fl, 1.0))
        m = two_halves(run(wf))
        rows.append({"group": "throttle", "label": f"节流 {int(pct0)}/{int(floor*100)} @1200", **m})
        print(f"{f'节流 {int(pct0)}/{int(floor*100)} @1200':<28} H1 {m['h1']}  H2 {m['h2']}")

# 分位窗口 @70/40
for pw in (240, 600):
    def wf(c, t, pw=pw):
        p = pct_maps[pw][c].iloc[t]
        return 1.0 if np.isnan(p) else float(np.clip(1 - 0.6 * (p - 70) / 30, 0.4, 1.0))
    m = two_halves(run(wf))
    rows.append({"group": "pctw", "label": f"节流 70/40 @{pw}", **m})
    print(f"{f'节流 70/40 @{pw}':<28} H1 {m['h1']}  H2 {m['h2']}")

# 波动率目标
for sigma in (0.15, 0.18, 0.21):
    for wmin in (0.3, 0.4, 0.5):
        def wf(c, t, s=sigma, wm=wmin):
            yz = ind[c]["yz"].iloc[t]
            return 1.0 if (np.isnan(yz) or yz <= 0) else float(np.clip(s / yz, wm, 1.0))
        m = two_halves(run(wf))
        rows.append({"group": "voltarget", "label": f"σ*={int(sigma*100)}%/下限{int(wmin*100)}%", **m})
        print(f"{f'σ*={int(sigma*100)}%/下限{int(wmin*100)}%':<28} H1 {m['h1']}  H2 {m['h2']}")

payload = {
    "meta": {"h1": [h1_lo, h1_hi], "h2": [h2_lo, h2_hi], "cost_bps_side": COST * 1e4},
    "rows": rows,
}
(HERE / "oos.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved oos.json")
