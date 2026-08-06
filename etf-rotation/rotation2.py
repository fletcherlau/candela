"""轮动策略 v2：实盘口径（信号日收盘执行）+ log/比值对比 + 成本敏感性 + Top2 磁滞 + 状态机。

执行：信号用当日收盘计算，当日收盘成交（对应实盘 14:45 算信号、尾盘下单）。
成本：单边 10bps（另跑 5bps 敏感性档）。
变体：
  base_log / base_raw —— 纯动量（log / 比值信号）
  base_log_5          —— 纯动量（log，单边 5bps，敏感性）
  hyster              —— Top2 磁滞：持仓跌出前两名才换到第一名（log）
  overlay             —— 三态状态机（log）
输出 rotation.json（覆盖 v1），控制台额外打印 2013 全窗口对比。
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
MOM, PCT_W, PCT_TH, DD_TH, RSI_N, RSI_TH = 20, 1200, 80.0, -0.03, 6, 30.0
COST = 0.001       # 单边 10bps
COST_LO = 0.0005   # 单边 5bps

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

# 停车资产：511010 国债ETF / 511880 货币ETF（现金仓位替代品，零佣金假设）
BOND_NAMES = {"511010.SH": "国债ETF", "511880.SH": "货币ETF"}
bond_rets = {}
for BOND, bname in BOND_NAMES.items():
    bdf = pro.fund_daily(ts_code=BOND, start_date="20100101", end_date="20260805").sort_values("trade_date")
    badj = pro.adj_factor(ts_code=BOND, start_date="20100101", end_date="20260805")
    badj = badj.sort_values("trade_date")[["trade_date", "adj_factor"]] if badj is not None and not badj.empty else None
    bdf["adj_factor"] = 1.0 if badj is None else bdf.merge(badj, on="trade_date", how="left")["adj_factor"].ffill().bfill().fillna(1.0)
    bdf["date"] = pd.to_datetime(bdf["trade_date"]).dt.strftime("%Y-%m-%d")
    bc = (bdf["close"].astype(float) * bdf["adj_factor"])
    bc.index = bdf["date"]
    bc = bc.reindex(dates).ffill()
    bond_rets[BOND] = bc.pct_change().fillna(0).values
    print(f"停车资产 {BOND} {bname}: 区间年化 {round(float((bc.iloc[-1]/bc.iloc[0])**(240/len(dates))-1)*100,2)}%")

# 511880 货币ETF 无复权因子、定期折算复位价格 → 价格法失真，改用分段货币基金收益率（估计值）
CASH_RATE = {  # 货币基金 7 日年化的大致中枢（估计）
    "2013": 0.035, "2014": 0.035, "2015": 0.033, "2016": 0.026,
    "2017": 0.030, "2018": 0.028, "2019": 0.024, "2020": 0.020,
    "2021": 0.020, "2022": 0.018, "2023": 0.019, "2024": 0.016,
    "2025": 0.015, "2026": 0.015,
}
bond_rets["CASH_SYN"] = np.array([CASH_RATE.get(d[:4], 0.015) / 240 for d in dates])
print(f"停车资产 货币ETF(合成收益): 分段估计 1.5%~3.5%")


def yz_pct(o, h, l, c, win=20, pct_w=PCT_W):
    k = 0.34 / (1.34 + (win + 1) / (win - 1))
    o_ret = np.log(o / c.shift(1))
    c_ret = np.log(c / o)
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
    score_log = num.abs() / den * num
    num_r = m4 / m4.shift(MOM - 1) - 1
    den_r = m4.diff().abs().rolling(MOM - 1).sum() / m4.shift(MOM - 1)
    score_raw = num_r.abs() / den_r * num_r
    delta = c.diff()
    gain = delta.clip(lower=0).ewm(alpha=1 / RSI_N, adjust=False).mean()
    loss = (-delta.clip(upper=0)).ewm(alpha=1 / RSI_N, adjust=False).mean()
    ind[code] = {
        "c": c, "score_log": score_log, "score_raw": score_raw,
        "pct": yz_pct(o, h, l, c),
        "rsi": 100 - 100 / (1 + gain / loss),
        "ma5": c.rolling(5).mean(), "ma20": c.rolling(20).mean(),
        "dd20": c / h.rolling(20).max() - 1, "low10": l.rolling(10).min(), "h": h,
    }

full_start = dates.get_loc("2013-08-26")
align_start = max(ind[c]["pct"].index.get_loc(ind[c]["pct"].first_valid_index()) for c in codes)

setup = {c: (ind[c]["pct"] >= PCT_TH) & (ind[c]["dd20"] <= DD_TH) for c in codes}
trigger = {c: (ind[c]["rsi"].shift(1) <= RSI_TH) & (ind[c]["rsi"] > RSI_TH)
           & (ind[c]["c"] > ind[c]["h"].shift(1)) & setup[c] for c in codes}


def ranks(skey, t):
    vals = {c: ind[c][skey].iloc[t] for c in codes}
    vals = {k: v for k, v in vals.items() if not np.isnan(v)}
    return sorted(vals, key=vals.get, reverse=True)


def run_momentum(skey, cost, start, hysteresis=False):
    ret = np.zeros(n)
    pos_arr = np.array([None] * n, dtype=object)
    pos, sw = None, 0
    for t in range(start + 1, n):
        if pos:
            ret[t] += ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1
        r = ranks(skey, t)
        if not r:
            pos_arr[t] = pos
            continue
        tgt = r[0]
        if hysteresis and pos is not None and pos in r[:2]:
            tgt = pos
        if tgt != pos:
            ret[t] -= (2 if pos else 1) * cost
            pos, sw = tgt, sw + 1
        pos_arr[t] = pos
    return ret, pos_arr, sw


def throttle_w(p, pct0=60.0, floor=0.6):
    """分位 <=pct0 → 满仓；pct0~100 线性降到 floor"""
    if np.isnan(p):
        return 1.0
    return float(np.clip(1 - (1 - floor) * (p - pct0) / (100 - pct0), floor, 1.0))


def run_throttle(skey, cost, start, pct0=60.0, floor=0.6):
    ret = np.zeros(n)
    pos, w_prev, sw = None, 0.0, 0
    pos_arr = np.array([None] * n, dtype=object)
    w_arr = np.zeros(n)
    for t in range(start + 1, n):
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        r = ranks(skey, t)
        if not r:
            pos_arr[t] = pos
            w_arr[t] = w_prev
            continue
        tgt = r[0]
        w_new = throttle_w(ind[tgt]["pct"].iloc[t], pct0, floor)
        if tgt != pos:
            ret[t] -= cost * (w_prev + w_new)   # 卖出旧仓位 + 买入新仓位
            pos, sw = tgt, sw + 1
        else:
            ret[t] -= cost * abs(w_new - w_prev)  # 仓位微调的部分收成本
        w_prev = w_new
        pos_arr[t] = pos
        w_arr[t] = w_new
    return ret, sw, pos_arr, w_arr


def run_throttle_park(skey, cost, start, bond_ret, pct0=70.0, floor=0.4, bond_cost=0.0):
    """节流 + 空仓部分买入停车资产（bond_ret 为停车资产日收益），停车腿成本 bond_cost"""
    ret = np.zeros(n)
    pos, w_prev, sw = None, 0.0, 0
    for t in range(start + 1, n):
        if w_prev > 0:
            bond_w = 1 - w_prev if pos else 1.0
            ret[t] += bond_w * bond_ret[t]
        if pos and w_prev > 0:
            ret[t] += w_prev * (ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1)
        r = ranks(skey, t)
        if not r:
            continue
        tgt = r[0]
        w_new = throttle_w(ind[tgt]["pct"].iloc[t], pct0, floor)
        if tgt != pos:
            ret[t] -= cost * (w_prev + w_new) + bond_cost * ((1 - w_prev) + (1 - w_new))
            pos, sw = tgt, sw + 1
        else:
            ret[t] -= (cost + bond_cost) * abs(w_new - w_prev)
        w_prev = w_new
    return ret, sw


def run_overlay(cost, start):
    ret = np.zeros(n)
    pos, mode, reb, reb_entry, sw = None, "mom", None, None, 0
    reb_trades, def_days = [], 0
    for t in range(start + 1, n):
        if pos:
            ret[t] += ind[pos]["c"].iloc[t] / ind[pos]["c"].iloc[t - 1] - 1
        if mode == "mom":
            if pos and ind[pos]["pct"].iloc[t] >= PCT_TH and ind[pos]["c"].iloc[t] < ind[pos]["ma20"].iloc[t]:
                ret[t] -= COST  # 收盘离场
                pos, mode = None, "def"
                continue
            r = ranks("score_log", t)
            if not r:
                continue
            tgt = r[0]
            if tgt != pos:
                ret[t] -= (2 if pos else 1) * cost
                pos, sw = tgt, sw + 1
        elif mode == "def":
            def_days += 1
            cands = [c for c in codes if bool(trigger[c].iloc[t])]
            if cands:
                pick = min(cands, key=lambda cd: ind[cd]["rsi"].iloc[t])
                ret[t] -= cost
                pos, reb, reb_entry, mode = pick, pick, t, "reb"
            else:
                r = ranks("score_log", t)
                if r and ind[r[0]]["c"].iloc[t] > ind[r[0]]["ma20"].iloc[t]:
                    ret[t] -= cost
                    pos, sw, mode = r[0], sw + 1, "mom"
        else:  # reb
            a = ind[reb]
            if (a["c"].iloc[t] < a["ma5"].iloc[t] or a["c"].iloc[t] < a["low10"].iloc[t] or a["score_log"].iloc[t] > 0):
                ret[t] -= cost
                seg = np.prod(1 + ret[reb_entry + 1:t + 1]) - 1
                reb_trades.append({"asset": reb, "entry": dates[reb_entry], "exit": dates[t],
                                   "ret": float(seg), "days": t - reb_entry})
                pos, reb, mode = None, None, "mom"
    return ret, {"switches": sw, "reb_trades": reb_trades, "def_days": def_days}


def metrics(ret, start):
    r = pd.Series(ret, index=dates).iloc[start + 1:]
    eq = (1 + r).cumprod()
    return {
        "ann": round(float(eq.iloc[-1] ** (240 / len(r)) - 1) * 100, 2),
        "sharpe": round(float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0, 2),
        "maxdd": round(float((eq / eq.cummax() - 1).min()) * 100, 2),
        "total": round((float(eq.iloc[-1]) - 1) * 100, 1),
        "in_market": round(float((r != 0).mean()) * 100, 1),
    }, eq


runs = {}
for key, (skey, cost, hyst) in {
    "base_log": ("score_log", COST, False),
    "base_raw": ("score_raw", COST, False),
    "base_log_5": ("score_log", COST_LO, False),
    "hyster": ("score_log", COST, True),
}.items():
    ret, pos_arr, sw = run_momentum(skey, cost, align_start, hyst)
    m, eq = metrics(ret, align_start)
    runs[key] = {"ret": ret, "pos": pos_arr, "sw": sw, "m": m, "eq": eq}

# log vs 比值：持仓重合度（对齐窗口，同成本同执行）
olap = float(np.mean(runs["base_log"]["pos"][align_start + 1:] == runs["base_raw"]["pos"][align_start + 1:]))
ann_diff = runs["base_log"]["m"]["ann"] - runs["base_raw"]["m"]["ann"]

# 全窗口（2013-08 起）参考值，仅控制台
for skey, tag in [("score_log", "log"), ("score_raw", "比值")]:
    ret_f, _, sw_f = run_momentum(skey, COST, full_start)
    m_f, _ = metrics(ret_f, full_start)
    print(f"全窗口 2013-08 起 · {tag} · 10bps · 收盘执行: 年化 {m_f['ann']}% 夏普 {m_f['sharpe']} 回撤 {m_f['maxdd']}% 换仓 {sw_f}")

ret_th, sw_th, pos_th, w_th = run_throttle("score_log", COST, align_start, 70.0, 0.4)
m_th, eq_th = metrics(ret_th, align_start)

ret_tp, sw_tp = run_throttle_park("score_log", COST, align_start, bond_rets["511010.SH"], 70.0, 0.4)
m_tp, eq_tp = metrics(ret_tp, align_start)
print(f"节流+国债停车: 年化 {m_tp['ann']}% 夏普 {m_tp['sharpe']} 回撤 {m_tp['maxdd']}%（vs 纯节流 {m_th['ann']}% / {m_th['sharpe']} / {m_th['maxdd']}%）")

ret_tm, sw_tm = run_throttle_park("score_log", COST, align_start, bond_rets["CASH_SYN"], 70.0, 0.4)
m_tm, eq_tm = metrics(ret_tm, align_start)
print(f"节流+货币停车(合成): 年化 {m_tm['ann']}% 夏普 {m_tm['sharpe']} 回撤 {m_tm['maxdd']}%")

ret_ol, ol_info = run_overlay(COST, align_start)
m_ol, eq_ol = metrics(ret_ol, align_start)

ret_gold = ind["518880.SH"]["c"].pct_change().fillna(0).values
m_gold, eq_gold = metrics(ret_gold, align_start)

names = {
    "base_log": "纯动量 · log（10bps）",
    "base_raw": "纯动量 · 比值（10bps）",
    "base_log_5": "纯动量 · log（5bps 敏感性）",
    "hyster": "动量 + Top2 磁滞缓冲",
    "throttle": "动量 + 分位仓位节流（70%/40%）",
    "throttle_park": "节流 70/40 + 国债ETF停车",
    "throttle_park880": "节流 70/40 + 货币ETF停车(合成)",
    "overlay": "动量 + 状态机优化",
    "gold": "黄金ETF 买入持有（参考）",
}
equities = {k: runs[k]["eq"] for k in ["base_log", "base_raw", "base_log_5", "hyster"]}
equities["throttle"] = eq_th
equities["throttle_park"] = eq_tp
equities["throttle_park880"] = eq_tm
equities["overlay"] = eq_ol
equities["gold"] = eq_gold
mets = {k: {**runs[k]["m"], "switches": runs[k]["sw"]} for k in runs}
mets["throttle"] = {**m_th, "switches": sw_th}
mets["throttle_park"] = {**m_tp, "switches": sw_tp}
mets["throttle_park880"] = {**m_tm, "switches": sw_tm}
mets["overlay"] = {**m_ol, "switches": ol_info["switches"]}
mets["gold"] = {**m_gold, "switches": 0}

# --- 节流参数网格稳健性 ---
grid = []
for pct0 in [50.0, 60.0, 70.0]:
    for floor in [0.4, 0.5, 0.6]:
        ret_g, _, _, _ = run_throttle("score_log", COST, align_start, pct0, floor)
        m_g, _ = metrics(ret_g, align_start)
        grid.append({"pct0": int(pct0), "floor": floor, "ann": m_g["ann"], "sharpe": m_g["sharpe"], "maxdd": m_g["maxdd"]})

# --- 成本拖累对照（同参数零成本） ---
ret_th0, _, _, _ = run_throttle("score_log", 0.0, align_start, 70.0, 0.4)
m_th0, _ = metrics(ret_th0, align_start)
ret_b0, _, _ = run_momentum("score_log", 0.0, align_start)
m_b0, _ = metrics(ret_b0, align_start)
print(f"\n成本拖累（对齐窗口，年化差）: 基线 {m_b0['ann']}%(零成本) → {runs['base_log']['m']['ann']}%(10bps) = 拖累 {round(m_b0['ann'] - runs['base_log']['m']['ann'], 2)}pp")
print(f"                        节流70/40 {m_th0['ann']}%(零成本) → {m_th['ann']}%(10bps) = 拖累 {round(m_th0['ann'] - m_th['ann'], 2)}pp")

out_dates = dates[align_start + 1:].tolist()
wins = [t for t in ol_info["reb_trades"] if t["ret"] > 0]
payload = {
    "meta": {
        "codes": CODES, "cost_bps_side": COST * 1e4, "exec": "信号日收盘执行（实盘 14:45 信号 + 尾盘下单）",
        "mom": MOM, "pct_w": PCT_W, "pct_th": PCT_TH, "dd_th": DD_TH, "rsi_n": RSI_N, "rsi_th": RSI_TH,
        "start": out_dates[0], "end": out_dates[-1], "n_days": len(out_dates),
    },
    "lograw": {
        "ann_log": runs["base_log"]["m"]["ann"], "ann_raw": runs["base_raw"]["m"]["ann"],
        "diff_pp": round(ann_diff, 2), "overlap_pct": round(olap * 100, 1),
    },
    "dates": out_dates,
    "variants": [
        {"key": k, "name": names[k], "metrics": mets[k],
         "equity": equities[k].round(4).tolist(),
         "dd": (equities[k] / equities[k].cummax() - 1).round(4).tolist()}
        for k in ["base_log", "base_raw", "base_log_5", "hyster", "throttle", "throttle_park", "throttle_park880", "overlay", "gold"]
    ],
    "rebound": {
        "trades": ol_info["reb_trades"], "n": len(ol_info["reb_trades"]),
        "winrate": round(len(wins) / len(ol_info["reb_trades"]) * 100, 1) if ol_info["reb_trades"] else None,
        "avg_ret": round(float(np.mean([t["ret"] for t in ol_info["reb_trades"]])) * 100, 2) if ol_info["reb_trades"] else None,
        "def_days": ol_info["def_days"],
    },
    "grid": grid,
}
(HERE / "rotation.json").write_text(json.dumps(payload, ensure_ascii=False))

# ---------- 5. 当前交易状态导出（70/40 节流版） ----------
s0 = align_start + 1
segments = []
cur, seg_start = None, None
for i in range(s0, n):
    p = pos_th[i]
    if p != cur:
        if cur is not None:
            segments.append({"asset": cur, "start": dates[seg_start], "end": dates[i - 1]})
        cur, seg_start = p, i
if cur is not None:
    segments.append({"asset": cur, "start": dates[seg_start], "end": dates[n - 1]})

last = n - 1
last_scores = {c: ind[c]["score_log"].iloc[last] for c in codes}
order = sorted(codes, key=lambda c: last_scores[c], reverse=True)
assets = [{
    "code": c, "name": CODES[c], "close": round(float(ind[c]["c"].iloc[last]), 3),
    "score": round(float(last_scores[c]), 4), "rank": order.index(c) + 1,
    "pct": round(float(ind[c]["pct"].iloc[last]), 1),
    "rsi": round(float(ind[c]["rsi"].iloc[last]), 1),
    "dd20": round(float(ind[c]["dd20"].iloc[last]) * 100, 2),
    "weight_if_held": round(throttle_w(ind[c]["pct"].iloc[last], 70.0, 0.4) * 100, 1),
} for c in order]
holding = pos_th[last]
state = {
    "meta": {"pct0": 70, "floor": 0.4, "start": dates[s0], "end": dates[last], "cost_bps_side": COST * 1e4},
    "current": {
        "date": dates[last], "holding": holding, "holding_name": CODES.get(holding, str(holding)),
        "weight": round(float(w_th[last]) * 100, 1),
        "pct": round(float(ind[holding]["pct"].iloc[last]), 1) if holding else None,
        "rank": order.index(holding) + 1 if holding else None,
    },
    "assets": assets,
    "dates": dates[s0:].tolist(),
    "weight": [round(float(x) * 100, 1) for x in w_th[s0:]],
    "segments": segments,
}
(HERE / "state.json").write_text(json.dumps(state, ensure_ascii=False))
print(f"\n当前状态 {dates[last]}: 持仓 {CODES.get(holding)} 仓位 {state['current']['weight']}% 分位 {state['current']['pct']}% 排名 #{state['current']['rank']}")

print(f"\n对齐窗口 {out_dates[0]} ~ {out_dates[-1]}")
print(f"{'策略':<26}{'年化%':>8}{'夏普':>7}{'最大回撤%':>10}{'累计%':>9}{'在场%':>7}{'换仓':>6}")
for v in payload["variants"]:
    m = v["metrics"]
    print(f"{v['name']:<26}{m['ann']:>8}{m['sharpe']:>7}{m['maxdd']:>10}{m['total']:>9}{m['in_market']:>7}{m['switches']:>6}")
print(f"\nlog vs 比值（对齐窗口，同成本同执行）: 年化差 {ann_diff:+.2f}pp, 持仓重合度 {olap*100:.1f}%")
print(f"反弹交易 {payload['rebound']['n']} 笔, 胜率 {payload['rebound']['winrate']}%, 笔均 {payload['rebound']['avg_ret']}%")
print("\n节流参数网格（起点 × 下限）:")
print(f"{'起点/下限':>10}{'年化%':>8}{'夏普':>7}{'最大回撤%':>10}")
for g in grid:
    print(f"{g['pct0']:>6}/{int(g['floor']*100):>3}%{g['ann']:>8}{g['sharpe']:>7}{g['maxdd']:>10}")
