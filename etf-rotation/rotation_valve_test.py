"""安全阀开/关隔离验证（纯回测口径，排除 DCA 账户规则影响）。

基座 = rotation7 生产基线：ER 动量 + δ=0.005 差距缓冲 + 70/40 分位节流 + 货币停车，
无吊灯，收盘执行，单边 10bps。唯一差异：
  valve=on  → 持仓得分 <0 时无视差距直接换仓（生产基线）
  valve=off → 任何情况下差距 ≤0.005 都不换仓

输出全区间 + 前后半段（SPLIT=2022-07-27）+ 逐年指标，valve_test.json。
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
CASH_RATE = {"2013": 0.035, "2014": 0.035, "2015": 0.033, "2016": 0.026, "2017": 0.030, "2018": 0.028,
             "2019": 0.024, "2020": 0.020, "2021": 0.020, "2022": 0.018, "2023": 0.019, "2024": 0.016,
             "2025": 0.015, "2026": 0.015}

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
cash_ret = np.array([CASH_RATE.get(d[:4], 0.015) / 240 for d in dates])


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


def run(valve_on):
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    switches = 0
    valve_fires = 0          # 安全阀触发次数（持仓得分<0 且差距≤δ 时直接换）
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        elif pos is None:
            ret[t] += cash_ret[t]
        vals = {c: ind[c]["score"].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        if pos is not None and tgt != pos:
            held_losing = vals.get(pos, 0) < 0
            gap = vals.get(tgt, -np.inf) - vals.get(pos, -np.inf)
            if valve_on and held_losing and gap <= 0.005:
                valve_fires += 1          # 安全阀生效：本来 δ 会拦住，强制换
            elif gap <= 0.005:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t])
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos, switches = tgt, switches + 1
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret, switches, valve_fires


def metrics_range(ret, d0, d1):
    r = pd.Series(ret, index=dates)
    m = (r.index >= d0) & (r.index <= d1)
    r = r[m]
    if len(r) < 20:
        return None
    eq = (1 + r).cumprod()
    return {
        "ann": round(float(eq.iloc[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0, 2),
        "maxdd": round(float((eq / eq.cummax() - 1).min()) * 100, 2),
    }


results = {}
for label, valve_on in [("valve_on", True), ("valve_off", False)]:
    ret, sw, vf = run(valve_on)
    d0, d1 = dates[align_start + 1], dates[-1]
    full = metrics_range(ret, d0, d1)
    h1 = metrics_range(ret, d0, SPLIT)
    h2 = metrics_range(ret, SPLIT, d1)
    years = {}
    for y in range(2018, 2027):
        m = metrics_range(ret, f"{y}-01-01", f"{y}-12-31")
        if m:
            years[y] = m["ann"]
    results[label] = {"switches": sw, "valve_fires": vf, "full": full,
                      "h1": h1, "h2": h2, "by_year": years}
    print(f"{label}: 换仓 {sw} 次（安全阀触发 {vf} 次） 全区间 年化 {full['ann']} 夏普 {full['sharpe']} 回撤 {full['maxdd']}")
    print(f"   H1(2018~2022-07) 年化 {h1['ann']} 夏普 {h1['sharpe']} 回撤 {h1['maxdd']} | "
          f"H2(2022-07~2026) 年化 {h2['ann']} 夏普 {h2['sharpe']} 回撤 {h2['maxdd']}")
    print(f"   逐年: {years}")

payload = {"meta": {"window": f"{dates[align_start + 1]} ~ {dates[-1]}", "split": SPLIT,
                    "cost_bps_side": COST * 1e4, "base": "rotation7 生产基线口径，无吊灯"},
           "results": results}
(HERE / "valve_test.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved valve_test.json")
