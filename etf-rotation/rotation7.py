"""轮动策略 v7：吊灯止损（Chandelier Exit）叠加测试。

吊灯止损：持仓期间维护 入场以来最高价 HH，止损线 = HH − mult × ATR(22)，只上移不下移；
收盘价跌破止损线 → 当日收盘离场转货币 ETF（合成收益），该标的冷却 cooldown 个交易日内不可买回。
基座：70/40 分位节流 + δ=0.005 差距缓冲（安全阀已于 2026-08 移除，见 valve_test.json）。
测试 mult ∈ {2.5, 3, 3.5} × cooldown ∈ {5, 10}，含前后半段（SPLIT=2022-07-27）。
输出 chand.json。
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
    tr = pd.concat([h - l, (h - c.shift(1)).abs(), (l - c.shift(1)).abs()], axis=1).max(axis=1)
    ind[code] = {"c": c, "h": h, "score": num.abs() / den * num, "yz": yz_vol(o, h, l, c),
                 "atr": tr.rolling(22).mean(), "hh22": h.rolling(22).max()}

pct1200 = {c: pct_of(ind[c]["yz"], 1200) for c in codes}
align_start = max(pct1200[c].index.get_loc(pct1200[c].first_valid_index()) for c in codes)


def throttle_w(p):
    if np.isnan(p):
        return 1.0
    return float(np.clip(1 - 0.6 * (p - 70) / 30, 0.4, 1.0))


def run(mult=None, cooldown=10):
    """mult=None → 无吊灯（生产基线）"""
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    hh = stop = np.nan
    barred = {}
    switches, stops = 0, 0
    cash_days = 0
    for t in range(align_start + 1, n):
        # 当日收益
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        elif pos is None:
            ret[t] += cash_ret[t]
            cash_days += 1
        vals = {c: ind[c]["score"].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        # 1) 吊灯止损检查（优先于动量换仓）
        if pos and mult is not None:
            hh = max(hh, ind[pos]["h"].iloc[t])
            a = ind[pos]["atr"].iloc[t]
            if not np.isnan(a):
                stop = max(stop, hh - mult * a)
            if ind[pos]["c"].iloc[t] < stop:
                ret[t] -= COST * w_prev
                barred[pos] = t + cooldown
                pos, w_prev = None, 0.0
                stops += 1
                continue  # 当日离场后空仓过夜
        # 2) 动量换仓（距离缓冲 δ=0.005，跳过冷却标的；安全阀已于 2026-08 移除，见 production-runbook v1.3）
        cands = {k: v for k, v in vals.items() if t > barred.get(k, -1)}
        if not cands:
            continue
        tgt = max(cands, key=cands.get)
        if pos is not None and tgt != pos:
            if cands.get(tgt, -np.inf) - vals.get(pos, -np.inf) <= 0.005:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t])
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos, switches = tgt, switches + 1
            if mult is not None:
                hh = ind[tgt]["hh22"].iloc[t]
                a = ind[tgt]["atr"].iloc[t]
                stop = hh - mult * a if not (np.isnan(hh) or np.isnan(a)) else -np.inf
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret, {"switches": switches, "stops": stops, "cash_pct": round(cash_days / (n - align_start - 1) * 100, 1)}


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


rows = []
configs = [("生产基线（无吊灯）", None, 10)]
for mult in (2.5, 3.0, 3.5):
    configs.append((f"吊灯 {mult}×ATR22 冷却10", mult, 10))
configs.append(("吊灯 3×ATR22 冷却5", 3.0, 5))

for label, mult, cd in configs:
    ret, info = run(mult, cd)
    m = metrics_range(ret, dates[align_start + 1], dates[-1])
    h1 = metrics_range(ret, dates[align_start + 1], SPLIT)
    h2 = metrics_range(ret, SPLIT, dates[-1])
    rows.append({"label": label, "mult": mult, "cooldown": cd, **m, **info,
                 "h1_ann": h1["ann"], "h2_ann": h2["ann"], "h1_sharpe": h1["sharpe"], "h2_sharpe": h2["sharpe"]})
    print(f"{label:<22} 年化 {m['ann']:>6} 夏普 {m['sharpe']:>5} 回撤 {m['maxdd']:>7} 止损 {info['stops']:>3} 空仓% {info['cash_pct']:>5} | H1 {h1['ann']:>6} H2 {h2['ann']:>6}")

payload = {
    "meta": {"start": dates[align_start + 1], "end": dates[-1], "split": SPLIT, "cost_bps_side": COST * 1e4},
    "rows": rows,
}
(HERE / "chand.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved chand.json")
