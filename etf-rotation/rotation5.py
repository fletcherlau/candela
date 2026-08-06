"""轮动策略 v5：换仓差距缓冲（margin hysteresis）网格。

规则：新第一名得分 score_new 必须超过当前持仓得分 score_old + δ 才换仓，否则继续持有。
δ ∈ {0, 0.002, 0.005, 0.01, 0.02, 0.04}，分别测试 纯动量 与 动量+70/40节流 两层。
收盘执行，单边 10bps，log 信号。输出 hyst.json。
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
DELTAS = [0.0, 0.002, 0.005, 0.01, 0.02, 0.04]

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


def run_margin(delta, throttle, valve=False):
    ret = np.zeros(n)
    pos, w_prev, sw = None, 0.0, 0
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        vals = {c: ind[c]["score"].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        # 差距缓冲：只有当新第一名的得分显著更高才换；安全阀：持仓得分为负时无视 margin
        if pos is not None and tgt != pos:
            held_losing = valve and vals.get(pos, 0) < 0
            if not held_losing and vals[tgt] - vals.get(pos, -np.inf) <= delta:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t]) if throttle else 1.0
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos, sw = tgt, sw + 1
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret, sw


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


SPLIT = "2022-07-27"
rows = []
for throttle in (False, True):
    for d in DELTAS:
        ret, sw = run_margin(d, throttle)
        m = metrics_range(ret, dates[align_start + 1], dates[-1])
        h1 = metrics_range(ret, dates[align_start + 1], SPLIT)
        h2 = metrics_range(ret, SPLIT, dates[-1])
        rows.append({"throttle": throttle, "delta": d, "valve": False, "switches": sw,
                     "ann": m["ann"], "sharpe": m["sharpe"], "maxdd": m["maxdd"],
                     "h1_ann": h1["ann"], "h2_ann": h2["ann"], "h1_sharpe": h1["sharpe"], "h2_sharpe": h2["sharpe"]})
        print(f"{'节流' if throttle else '纯动量'} δ={d:<6} 年化 {m['ann']:>6} 夏普 {m['sharpe']:>5} 回撤 {m['maxdd']:>7} 换仓 {sw:>4}  | H1 {h1['ann']:>6} H2 {h2['ann']:>6}")

# 安全阀：margin 只过滤持有赢家时的噪音，持仓得分为负时直接换
print("\n安全阀版（持仓得分<0 时无视 margin）:")
for throttle in (False, True):
    for d in (0.005, 0.02, 0.04):
        ret, sw = run_margin(d, throttle, valve=True)
        m = metrics_range(ret, dates[align_start + 1], dates[-1])
        h1 = metrics_range(ret, dates[align_start + 1], SPLIT)
        h2 = metrics_range(ret, SPLIT, dates[-1])
        rows.append({"throttle": throttle, "delta": d, "valve": True, "switches": sw,
                     "ann": m["ann"], "sharpe": m["sharpe"], "maxdd": m["maxdd"],
                     "h1_ann": h1["ann"], "h2_ann": h2["ann"], "h1_sharpe": h1["sharpe"], "h2_sharpe": h2["sharpe"]})
        print(f"{'节流' if throttle else '纯动量'} δ={d:<6} 年化 {m['ann']:>6} 夏普 {m['sharpe']:>5} 回撤 {m['maxdd']:>7} 换仓 {sw:>4}  | H1 {h1['ann']:>6} H2 {h2['ann']:>6}")

payload = {
    "meta": {"start": dates[align_start + 1], "end": dates[-1], "split": SPLIT, "cost_bps_side": COST * 1e4, "deltas": DELTAS},
    "rows": rows,
}
(HERE / "hyst.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved hyst.json")
