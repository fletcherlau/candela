"""真实账户现金流模拟：生产基线策略（ER 动量 + δ=0.005 缓冲 + 安全阀 + 70/40 节流，
无吊灯），按真实资金规则执行：

- 首期投入 20 万元，每月第一个交易日定投 2 万元（先落入现金）；
- 四只标的：佣金万 2.5、不免五（单笔最低 5 元），滑点单边 7.5bps；
- 空仓现金部分免佣免滑点、不计息（实际用现金宝，收益难算按 0 计，保守口径；CASH_YIELD 可改）；
- 换仓按信号全额执行；不调仓日的仓位微调阈值 5%（|目标仓位−当前仓位| ≥5% 才动）；
- 份额法（unitization）计算 NAV，消除定投对回撤/年化口径的扭曲。

输出 dca_sim.json：日度 {date, value, invested, nav, dd, holding, mmf_weight} + 汇总统计。
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
MOM = 20
COMM_RATE, COMM_MIN, SLIP = 0.000235, 5.0, 0.00075   # 万2.35 不免五，滑点单边 7.5bps
MIN_ADJ = 0.05                                       # 调仓最小幅度 5%
INIT_CAP = float(os.environ.get("INIT_CAP", "200000"))
MONTHLY = float(os.environ.get("MONTHLY", "20000"))
CASH_YIELD = float(os.environ.get("CASH_YIELD", "0"))   # 空仓现金部分收益：实际用现金宝，难算按 0 计（保守）
SAFETY_VALVE = os.environ.get("SAFETY_VALVE", "0") == "1"   # 默认无安全阀（生产基线 v1.3）；1 → 旧版含安全阀
OUT = os.environ.get("OUT_JSON", "dca_sim.json")
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
cash_ret = np.array([CASH_YIELD / 240 for _ in dates])


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


def trade_cost(amount):
    """四标的单边交易成本：滑点 + 佣金（万2.5 不免五）"""
    return amount * SLIP + max(COMM_MIN, amount * COMM_RATE)


# ---------- 账户模拟 ----------
t0 = align_start + 1
month_of = np.array([d[:7] for d in dates])

etf_val = 0.0          # 持仓标的部分市值（按后复权口径记账，视作全收益单位）
mmf = 0.0              # 现金金额
pos = None
units = 0.0            # 份额法
nav = 1.0
invested = 0.0
switches = 0
total_comm = 0.0
total_slip = 0.0

rows = []
last_month = None
for t in range(t0, n):
    # 1) 持仓过夜收益（后复权全收益口径）
    if pos and etf_val > 0:
        etf_val *= ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1]
    # 2) 现金部分（不计息则恒等）
    mmf *= 1 + cash_ret[t]
    # 3) 月初定投（先落入现金），份额法增资
    if month_of[t] != last_month:
        last_month = month_of[t]
        add = INIT_CAP if t == t0 else MONTHLY
        port = etf_val + mmf
        nav = port / units if units > 0 else 1.0
        units += add / nav
        mmf += add
        invested += add
    port = etf_val + mmf

    # 4) 信号与决策（与生产基线一致）
    vals = {c: ind[c]["score"].iloc[t] for c in codes}
    vals = {k: v for k, v in vals.items() if not np.isnan(v)}
    if vals:
        tgt = max(vals, key=vals.get)
        if pos is not None and tgt != pos:
            held_losing = vals.get(pos, 0) < 0
            if not (SAFETY_VALVE and held_losing) and vals.get(tgt, -np.inf) - vals.get(pos, -np.inf) <= 0.005:
                tgt = pos
        w_new = throttle_w(pct1200[tgt].iloc[t])
        w_cur = etf_val / port if port > 0 else 0.0
        if tgt != pos:
            # 换仓：卖旧（全部）→ 买新（目标仓位），剩余留现金
            if etf_val > 0:
                c_sell = trade_cost(etf_val)
                total_slip += etf_val * SLIP
                total_comm += max(COMM_MIN, etf_val * COMM_RATE)
                mmf += etf_val - c_sell
                etf_val = 0.0
            port = etf_val + mmf
            buy = port * w_new
            c_buy = trade_cost(buy)
            total_slip += buy * SLIP
            total_comm += max(COMM_MIN, buy * COMM_RATE)
            etf_val = buy - c_buy
            mmf = port - buy
            pos = tgt
            switches += 1
        elif abs(w_new - w_cur) >= MIN_ADJ:
            # 仓位微调（≥5% 才动），现金反向同调
            port = etf_val + mmf
            target_val = port * w_new
            diff = target_val - etf_val
            c = trade_cost(abs(diff))
            total_slip += abs(diff) * SLIP
            total_comm += max(COMM_MIN, abs(diff) * COMM_RATE)
            if diff > 0:   # 买入
                etf_val += diff - c
                mmf -= diff
            else:          # 卖出
                etf_val += diff       # diff 为负
                mmf += -diff - c
    port = etf_val + mmf
    nav = port / units if units > 0 else nav
    rows.append({"date": dates[t], "value": round(port, 2), "invested": round(invested, 2),
                 "nav": round(nav, 6), "holding": pos or "MMF",
                 "mmf_w": round(mmf / port, 4) if port > 0 else 1.0})

df = pd.DataFrame(rows)
df["peak_nav"] = df["nav"].cummax()
df["dd"] = (df["nav"] / df["peak_nav"] - 1) * 100

years = len(df) / 240
total_ret_nav = df["nav"].iloc[-1] - 1
stats = {
    "start": df["date"].iloc[0], "end": df["date"].iloc[-1], "days": len(df),
    "init_capital": INIT_CAP, "monthly": MONTHLY,
    "invested_total": round(invested, 2),
    "final_value": round(df["value"].iloc[-1], 2),
    "profit": round(df["value"].iloc[-1] - invested, 2),
    "profit_pct_of_invested": round((df["value"].iloc[-1] / invested - 1) * 100, 2),
    "nav_ann": round((df["nav"].iloc[-1] ** (1 / years) - 1) * 100, 2),
    "maxdd": round(float(df["dd"].min()), 2),
    "switches": switches,
    "comm_total": round(total_comm, 2),
    "slip_total": round(total_slip, 2),
    "cost_total": round(total_comm + total_slip, 2),
}
payload = {"stats": {**stats, "safety_valve": SAFETY_VALVE},
           "names": {**CODES, "MMF": "现金"},
           "daily": df[["date", "value", "invested", "nav", "dd", "holding", "mmf_w"]].to_dict("records")}
(HERE / OUT).write_text(json.dumps(payload, ensure_ascii=False))
print(json.dumps(stats, ensure_ascii=False, indent=2))
