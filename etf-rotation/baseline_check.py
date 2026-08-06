"""分解基线年化差异：窗口 × 成本 × 执行价 三个因素的影响。"""
import json
import os
import sys

import numpy as np
import pandas as pd
import tushare as ts

TOKEN = os.environ.get("TUSHARE_TOKEN")
if not TOKEN:
    sys.exit("TUSHARE_TOKEN required")

CODES = {"510880.SH": "红利", "518880.SH": "黄金", "159915.SZ": "创业板", "513100.SH": "纳指"}
MOM = 20

pro = ts.pro_api(TOKEN)
panels = {}
for code in CODES:
    df = pro.fund_daily(ts_code=code, start_date="20100101", end_date="20260805").sort_values("trade_date")
    adj = pro.adj_factor(ts_code=code, start_date="20100101", end_date="20260805")
    adj = adj.sort_values("trade_date")[["trade_date", "adj_factor"]] if adj is not None and not adj.empty else None
    if adj is not None:
        df = df.merge(adj, on="trade_date", how="left")
        df["adj_factor"] = df["adj_factor"].ffill().bfill().fillna(1.0)
    else:
        df["adj_factor"] = 1.0
    df["date"] = pd.to_datetime(df["trade_date"]).dt.strftime("%Y-%m-%d")
    for c in ["open", "high", "low", "close"]:
        df[c] = df[c].astype(float) * df["adj_factor"]
    panels[code] = df.set_index("date")[["open", "high", "low", "close"]]

common = sorted(set.intersection(*[set(p.index) for p in panels.values()]))
px = {c: panels[c].loc[common] for c in CODES}
dates = pd.Index(common)
n = len(dates)
codes = list(CODES)

score, score_raw = {}, {}
for code in codes:
    p = px[code]
    m4 = (p["open"] + p["high"] + p["low"] + p["close"]) / 4
    logm = np.log(m4)
    num = logm - logm.shift(MOM - 1)
    den = logm.diff().abs().rolling(MOM - 1).sum()
    score[code] = num.abs() / den * num                       # log 版
    num_r = m4 / m4.shift(MOM - 1)                            # 比值版
    den_r = m4.diff().abs().rolling(MOM - 1).sum() / m4.shift(MOM - 1)
    score_raw[code] = num_r.sub(1).abs() / den_r * num_r.sub(1)  # ER × (比值-1)


def run(sc, start_date, cost, exec_at="open"):
    ret = np.zeros(n)
    pos = None
    sw = 0
    start_idx = dates.get_loc(start_date)
    for t in range(start_idx, n - 1):
        j = t + 1
        vals = {c: sc[c].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        if pos is None:
            a = px[tgt]
            ret[j] += (a["close"].iloc[j] / a["open"].iloc[j] - 1) - cost
            pos, sw = tgt, sw + 1
        elif tgt != pos:
            a, b = px[pos], px[tgt]
            if exec_at == "open":
                ret[j] += (a["open"].iloc[j] / a["close"].iloc[t] - 1) - cost
                ret[j] += (b["close"].iloc[j] / b["open"].iloc[j] - 1) - cost
            else:  # 收盘执行：老标的吃到收盘，新标的从收盘建仓
                ret[j] += (a["close"].iloc[j] / a["close"].iloc[t] - 1) - cost * 2
            pos, sw = tgt, sw + 1
        else:
            a = px[pos]
            ret[j] += a["close"].iloc[j] / a["close"].iloc[t] - 1
    r = pd.Series(ret, index=dates).iloc[start_idx:]
    nn = (r.index <= dates[-1]).sum()
    eq = (1 + r).cumprod()
    ann = float(eq.iloc[-1] ** (240 / len(r)) - 1)
    dd = float((eq / eq.cummax() - 1).min())
    return ann * 100, dd * 100, sw


rows = []
for label, kw in [
    ("2013-08 起 · log · 无成本 · 开盘", dict(sc=score, start_date="2013-08-26", cost=0.0)),
    ("2013-08 起 · log · 双边12bps · 开盘", dict(sc=score, start_date="2013-08-26", cost=0.0006)),
    ("2018-07 起 · log · 双边12bps · 开盘（=页面基线）", dict(sc=score, start_date="2018-07-26", cost=0.0006)),
    ("2013-08 起 · 比值 · 无成本 · 开盘（≈你的版本）", dict(sc=score_raw, start_date="2013-08-26", cost=0.0)),
    ("2013-08 起 · 比值 · 无成本 · 收盘执行", dict(sc=score_raw, start_date="2013-08-26", cost=0.0, exec_at="close")),
]:
    ann, dd, sw = run(**kw)
    rows.append((label, ann, dd, sw))
    print(f"{label:<40} 年化 {ann:6.2f}%  最大回撤 {dd:7.2f}%  换仓 {sw}")
