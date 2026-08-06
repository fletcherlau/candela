"""红利腿标的替换对比：将生产基线四标的中的红利 ETF（510880）分别换成
512890（红利低波ETF）和 515080（中证红利ETF招商），看整体策略表现。

口径与 rotation7.py 生产基线完全一致：ER 动量 + δ=0.005 差距缓冲（含安全阀）
+ 70/40 分位节流 + 货币停车，收盘执行，单边 10bps，无吊灯（mult=None）。

注意：512890 上市于 2018-12，515080 上市于 2019-11；叠加 1200 日分位窗口后，
变体的可回测区间明显短于基线（2018-07 起）。因此对每个变体，同时输出
基线组合在「同一区间」内的指标，保证可比。

输出 div_swap.json。
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
BASE_DIV = "510880.SH"
VARIANTS = {"512890.SH": "红利低波ETF", "515080.SH": "中证红利ETF招商"}
OTHERS = {"518880.SH": "黄金ETF", "159915.SZ": "创业板ETF", "513100.SH": "纳指ETF"}
ALL = {BASE_DIV: "红利ETF", **VARIANTS, **OTHERS}
MOM, COST = 20, 0.001
CASH_RATE = {"2013": 0.035, "2014": 0.035, "2015": 0.033, "2016": 0.026, "2017": 0.030, "2018": 0.028,
             "2019": 0.024, "2020": 0.020, "2021": 0.020, "2022": 0.018, "2023": 0.019, "2024": 0.016,
             "2025": 0.015, "2026": 0.015}

pro = ts.pro_api(TOKEN)
panels = {}
for code in ALL:
    df = pro.fund_daily(ts_code=code, start_date="20100101", end_date="20260805").sort_values("trade_date")
    adj = pro.adj_factor(ts_code=code, start_date="20100101", end_date="20260805")
    adj = adj.sort_values("trade_date")[["trade_date", "adj_factor"]] if adj is not None and not adj.empty else None
    df["adj_factor"] = 1.0 if adj is None else df.merge(adj, on="trade_date", how="left")["adj_factor"].ffill().bfill().fillna(1.0)
    df["date"] = pd.to_datetime(df["trade_date"]).dt.strftime("%Y-%m-%d")
    for c in ["open", "high", "low", "close"]:
        df[c] = df[c].astype(float) * df["adj_factor"]
    panels[code] = df.set_index("date")[["open", "high", "low", "close"]]


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


def throttle_w(p):
    if np.isnan(p):
        return 1.0
    return float(np.clip(1 - 0.6 * (p - 70) / 30, 0.4, 1.0))


def build(codes):
    """对给定标的池构造对齐后的指标（等价于 rotation7 的全局计算）。"""
    common = sorted(set.intersection(*[set(panels[c].index) for c in codes]))
    dates = pd.Index(common)
    ind = {}
    for code in codes:
        p = panels[code].loc[common]
        o, h, l, c = p["open"], p["high"], p["low"], p["close"]
        m4 = (o + h + l + c) / 4
        logm = np.log(m4)
        num = logm - logm.shift(MOM - 1)
        den = logm.diff().abs().rolling(MOM - 1).sum()
        ind[code] = {"c": c, "score": num.abs() / den * num, "yz": yz_vol(o, h, l, c)}
    pct1200 = {c: pct_of(ind[c]["yz"], 1200) for c in codes}
    align_start = max(pct1200[c].index.get_loc(pct1200[c].first_valid_index()) for c in codes)
    cash_ret = np.array([CASH_RATE.get(d[:4], 0.015) / 240 for d in dates])
    return dates, ind, pct1200, align_start, cash_ret


def run(dates, ind, pct1200, align_start, cash_ret):
    """生产基线：无吊灯，δ=0.005 + 安全阀 + 70/40 节流。"""
    n = len(dates)
    ret = np.zeros(n)
    pos, w_prev = None, 0.0
    switches = 0
    hold_days = {c: 0 for c in ind}
    for t in range(align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
            hold_days[pos] += 1
        elif pos is None:
            ret[t] += cash_ret[t]
        vals = {c: ind[c]["score"].iloc[t] for c in ind}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        if pos is not None and tgt != pos:
            held_losing = vals.get(pos, 0) < 0
            if not held_losing and vals.get(tgt, -np.inf) - vals.get(pos, -np.inf) <= 0.005:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t])
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos, switches = tgt, switches + 1
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
    return ret, switches, hold_days


def metrics_range(ret, dates, d0, d1):
    r = pd.Series(ret, index=dates)
    m = (r.index >= d0) & (r.index <= d1)
    r = r[m]
    eq = (1 + r).cumprod()
    return {
        "ann": round(float(eq.iloc[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0, 2),
        "maxdd": round(float((eq / eq.cummax() - 1).min()) * 100, 2),
    }


results = []

# 基线：原始四标的，全区间
base_codes = [BASE_DIV, *OTHERS]
dates_b, ind_b, pct_b, start_b, cash_b = build(base_codes)
ret_b, sw_b, hd_b = run(dates_b, ind_b, pct_b, start_b, cash_b)
d0, d1 = dates_b[start_b + 1], dates_b[-1]
m = metrics_range(ret_b, dates_b, d0, d1)
results.append({"label": "基线 510880（全区间）", "window": f"{d0} ~ {d1}", "switches": sw_b,
                "div_hold_pct": round(hd_b[BASE_DIV] / sum(hd_b.values()) * 100, 1), **m})
print(f"{'基线 510880（全区间）':<28} {d0}~{d1} 年化 {m['ann']:>6} 夏普 {m['sharpe']:>5} 回撤 {m['maxdd']:>7} 换仓 {sw_b:>3}")

# 变体：替换红利腿
for vcode, vname in VARIANTS.items():
    codes = [vcode, *OTHERS]
    dates_v, ind_v, pct_v, start_v, cash_v = build(codes)
    ret_v, sw_v, hd_v = run(dates_v, ind_v, pct_v, start_v, cash_v)
    vd0, vd1 = dates_v[start_v + 1], dates_v[-1]
    mv = metrics_range(ret_v, dates_v, vd0, vd1)
    results.append({"label": f"换成 {vcode[:-3]} {vname}", "window": f"{vd0} ~ {vd1}", "switches": sw_v,
                    "div_hold_pct": round(hd_v[vcode] / sum(hd_v.values()) * 100, 1), **mv})
    # 基线在同一区间的表现（用基线收益序列截取）
    mb = metrics_range(ret_b, dates_b, vd0, vd1)
    results.append({"label": "  基线 510880（同区间对照）", "window": f"{vd0} ~ {vd1}", "switches": None,
                    "div_hold_pct": None, **mb})
    print(f"{'换成 ' + vcode[:-3] + ' ' + vname:<28} {vd0}~{vd1} 年化 {mv['ann']:>6} 夏普 {mv['sharpe']:>5} 回撤 {mv['maxdd']:>7} 换仓 {sw_v:>3}")
    print(f"{'  基线 510880（同区间对照）':<28} {vd0}~{vd1} 年化 {mb['ann']:>6} 夏普 {mb['sharpe']:>5} 回撤 {mb['maxdd']:>7}")

payload = {"meta": {"cost_bps_side": COST * 1e4, "div_leg_replaced": BASE_DIV,
                    "note": "口径=rotation7 生产基线(mult=None)；变体区间受上市日+1200日分位窗口限制，附基线同区间对照"},
           "rows": results}
(HERE / "div_swap.json").write_text(json.dumps(payload, ensure_ascii=False))
print("saved div_swap.json")
