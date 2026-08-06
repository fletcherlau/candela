"""拆解：仓位节流（70%/40%）的收益与回撤改善到底来自哪里。

输出：逐年对比、基线最大回撤时段逐段复盘、收益让渡 vs 回撤躲过的总量分解。
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
MOM, PCT_W, COST = 20, 1200, 0.001
PCT0, FLOOR = 70.0, 0.4

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


def yz_pct(o, h, l, c, win=20, pct_w=PCT_W):
    k = 0.34 / (1.34 + (win + 1) / (win - 1))
    o_ret, c_ret = np.log(o / c.shift(1)), np.log(c / o)
    rs = np.log(h / c) * np.log(h / o) + np.log(l / c) * np.log(l / o)
    var = (o_ret.rolling(win).var(ddof=1) + k * c_ret.rolling(win).var(ddof=1) + (1 - k) * rs.rolling(win).mean())
    yv = np.sqrt(var * 240).values
    pct = np.full(len(yv), np.nan)
    for t in range(len(yv)):
        if np.isnan(yv[t]):
            continue
        w = yv[max(0, t - pct_w + 1):t + 1]
        w = w[~np.isnan(w)]
        if len(w) < pct_w:
            continue
        pct[t] = ((w < yv[t]).sum() + 0.5 * (w == yv[t]).sum()) / len(w) * 100.0
    return pd.Series(pct, index=o.index)


ind = {}
for code in codes:
    p = px[code]
    o, h, l, c = p["open"], p["high"], p["low"], p["close"]
    m4 = (o + h + l + c) / 4
    logm = np.log(m4)
    num = logm - logm.shift(MOM - 1)
    den = logm.diff().abs().rolling(MOM - 1).sum()
    ind[code] = {"c": c, "score": num.abs() / den * num, "pct": yz_pct(o, h, l, c)}

align_start = max(ind[c]["pct"].index.get_loc(ind[c]["pct"].first_valid_index()) for c in codes)


def throttle_w(p):
    if np.isnan(p):
        return 1.0
    return float(np.clip(1 - (1 - FLOOR) * (p - PCT0) / (100 - PCT0), FLOOR, 1.0))


def run(throttle):
    ret = np.zeros(n)
    w_arr = np.ones(n)
    pos_arr = np.array([None] * n, dtype=object)
    pos, w_prev = None, 0.0
    for t in range(start_idx + 1 if False else align_start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        vals = {c: ind[c]["score"].iloc[t] for c in codes}
        vals = {k: v for k, v in vals.items() if not np.isnan(v)}
        if not vals:
            continue
        tgt = max(vals, key=vals.get)
        w_new = throttle_w(ind[tgt]["pct"].iloc[t]) if throttle else 1.0
        if tgt != pos:
            ret[t] -= COST * (w_prev + w_new)
            pos = tgt
        else:
            ret[t] -= COST * abs(w_new - w_prev)
        w_prev = w_new
        w_arr[t] = w_new
        pos_arr[t] = pos
    return pd.Series(ret, index=dates), w_arr, pos_arr


ret_b, w_b, pos_b = run(False)
ret_t, w_t, pos_t = run(True)
seg = slice(align_start + 1, None)
r_b, r_t = ret_b.iloc[seg], ret_t.iloc[seg]
w_ts = pd.Series(w_t, index=dates).iloc[seg]
pos_ts = pd.Series(pos_t, index=dates).iloc[seg]
eq_b, eq_t = (1 + r_b).cumprod(), (1 + r_t).cumprod()


def yr_stats(r, eq):
    out = {}
    for y in sorted(set(r.index.str[:4])):
        m = r.index.str.startswith(y)
        rr = r[m]
        ee = eq[m]
        out[y] = {"ret": round((float((1 + rr).prod()) - 1) * 100, 1),
                  "maxdd": round(float((ee / ee.cummax() - 1).min()) * 100, 1)}
    return out


yb, yt = yr_stats(r_b, eq_b), yr_stats(r_t, eq_t)
w_year = {y: round(float(w_ts[w_ts.index.str.startswith(y)].mean()) * 100, 1) for y in yb}

# --- 崩盘时段：基线回撤最深的 4 段（间隔 >= 60 天） ---
dd_b = eq_b / eq_b.cummax() - 1
dd_t = eq_t / eq_t.cummax() - 1
episodes = []
cand = dd_b.sort_values().index.tolist()
picked = []
for d in cand:
    if dd_b[d] > -0.12:
        break
    if all(abs((pd.Timestamp(d) - pd.Timestamp(p)).days) > 60 for p in picked):
        picked.append(d)
    if len(picked) >= 4:
        break
for trough in picked:
    i_s = r_b.index.get_loc(trough)          # 切片内位置
    peak = eq_b.iloc[:i_s + 1].idxmax()
    j_s = r_b.index.get_loc(peak)
    held = str(pos_b[dates.get_loc(trough)])
    ep = {
        "peak": peak, "trough": trough,
        "asset": held,
        "asset_name": CODES.get(held, held),
        "dd_base": round(float(dd_b[trough]) * 100, 1),
        "dd_throt": round(float(dd_t[trough]) * 100, 1),
        "avg_w": round(float(w_ts.iloc[j_s:i_s + 1].mean()) * 100, 1),
        "pct_at_trough": round(float(ind[held]["pct"].loc[trough]), 1) if held in ind else None,
    }
    episodes.append(ep)
episodes.sort(key=lambda e: e["peak"])

# --- 总量分解：降仓日子里的收益让渡 vs 回撤躲过 ---
throttled = w_ts < 0.999
forgone = float(((1 - w_ts[throttled]) * r_b[throttled]).sum() * 100)   # 少赚的收益
held_ret_on_throttled = float((r_b[throttled]).sum() * 100)               # 这些日子满仓的原收益
summary = {
    "throttled_days": int(throttled.sum()),
    "throttled_pct": round(float(throttled.mean()) * 100, 1),
    "avg_w_throttled": round(float(w_ts[throttled].mean()) * 100, 1),
    "forgone_ret": round(forgone, 1),
    "held_ret_on_throttled": round(held_ret_on_throttled, 1),
    "dd_base": round(float(dd_b.min()) * 100, 1),
    "dd_throt": round(float(dd_t.min()) * 100, 1),
    "ann_base": round(float(eq_b.iloc[-1] ** (240 / len(r_b)) - 1) * 100, 2),
    "ann_throt": round(float(eq_t.iloc[-1] ** (240 / len(r_t)) - 1) * 100, 2),
}

payload = {
    "meta": {"pct0": PCT0, "floor": FLOOR, "start": r_b.index[0], "end": r_b.index[-1]},
    "yearly": [{"year": y, "base_ret": yb[y]["ret"], "throt_ret": yt[y]["ret"],
                "base_dd": yb[y]["maxdd"], "throt_dd": yt[y]["maxdd"], "avg_w": w_year[y]} for y in yb],
    "episodes": episodes,
    "summary": summary,
}
(HERE / "throttle_analysis.json").write_text(json.dumps(payload, ensure_ascii=False))

print(f"窗口 {summary and r_b.index[0]} ~ {r_b.index[-1]}  参数 {int(PCT0)}/{int(FLOOR*100)}")
print(f"\n{'年份':<6}{'基线%':>8}{'节流%':>8}{'基线DD%':>9}{'节流DD%':>9}{'平均仓位%':>10}")
for row in payload["yearly"]:
    print(f"{row['year']:<6}{row['base_ret']:>8}{row['throt_ret']:>8}{row['base_dd']:>9}{row['throt_dd']:>9}{row['avg_w']:>10}")
print("\n基线最深回撤时段复盘:")
for e in episodes:
    print(f"  {e['peak']} → {e['trough']}  持仓 {e['asset_name']:<6} 基线 {e['dd_base']}%  节流 {e['dd_throt']}%  期间均仓 {e['avg_w']}%  谷底分位 {e['pct_at_trough']}")
print("\n总量分解:")
print(f"  降仓天数 {summary['throttled_days']}（{summary['throttled_pct']}%），降仓日平均仓位 {summary['avg_w_throttled']}%")
print(f"  降仓日满仓原收益合计 {summary['held_ret_on_throttled']}%，让渡收益 {summary['forgone_ret']}%")
print(f"  年化：基线 {summary['ann_base']}% → 节流 {summary['ann_throt']}%；最大回撤 {summary['dd_base']}% → {summary['dd_throt']}%")
