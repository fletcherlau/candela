"""四标的 ETF 动量轮动 + 波动率状态机优化回测。

标的池：510880.SH 红利、518880.SH 黄金、159915.SZ 创业板、513100.SH 纳指
价格：fund_daily + adj_factor 后复权
动量信号（复刻用户策略）：
  log OHLC 四点均价 → 20 日窗口 ER = 净位移 / 路径长度 → 动量 = ER × 净位移
基线：每日收盘打分，次日开盘全仓换得分最高者
优化（三态状态机）：
  动量模式：同基线；持仓标的 YZ 分位 >= 80 且收盘 < MA20 → 次日开盘离场转防御
  防御模式：(a) 任一标的 分位>=80 且 20日高点回撤<=-3% 且 RSI(6) 回穿30 + 收盘>前日最高
            → 次日开盘入场反弹模式（多个候选取 RSI 最低者）
            (b) 无反弹机会且动量第一名收盘 > MA20 → 次日开盘重新入场动量模式
  反弹模式：跌破 MA5 / 创10日新低 / 该标的 20 日动量转正 → 次日开盘离场，交还动量模式
成本：单边 6 bps。输出 rotation.json。
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
    sys.exit("TUSHARE_TOKEN env var is required")

HERE = Path(__file__).parent
CODES = {
    "510880.SH": "红利ETF",
    "518880.SH": "黄金ETF",
    "159915.SZ": "创业板ETF",
    "513100.SH": "纳指ETF",
}
COST = 0.0006
MOM = 20            # 动量窗口（20 个点 = 19 个间隔）
PCT_W = 1200
PCT_TH = 80.0
DD_TH = -0.03
RSI_N, RSI_TH = 6, 30.0

pro = ts.pro_api(TOKEN)

# ---------- 1. 取数 + 后复权 ----------
panels = {}
for code in CODES:
    df = pro.fund_daily(ts_code=code, start_date="20100101", end_date="20260805")
    if df is None or df.empty:
        sys.exit(f"fund_daily empty for {code}")
    df = df.sort_values("trade_date").reset_index(drop=True)
    adj = pro.adj_factor(ts_code=code, start_date="20100101", end_date="20260805")
    if adj is not None and not adj.empty:
        adj = adj.sort_values("trade_date")[["trade_date", "adj_factor"]]
        df = df.merge(adj, on="trade_date", how="left")
        df["adj_factor"] = df["adj_factor"].ffill().bfill().fillna(1.0)
    else:
        df["adj_factor"] = 1.0
    df["date"] = pd.to_datetime(df["trade_date"]).dt.strftime("%Y-%m-%d")
    for c in ["open", "high", "low", "close"]:
        df[c] = df[c].astype(float) * df["adj_factor"]  # 后复权
    panels[code] = df.set_index("date")[["open", "high", "low", "close"]]
    print(f"{code} {CODES[code]}: {len(df)} rows, {df['date'].iloc[0]} ~ {df['date'].iloc[-1]}")

# 共同交易日
common = sorted(set.intersection(*[set(p.index) for p in panels.values()]))
px = {code: panels[code].loc[common] for code in CODES}
dates = pd.Index(common)
n = len(dates)
print(f"共同交易日: {n}, {dates[0]} ~ {dates[-1]}")

# ---------- 2. 指标 ----------
def yz_pct(o, h, l, c, win=20, pct_w=PCT_W):
    k = 0.34 / (1.34 + (win + 1) / (win - 1))
    o_ret = np.log(o / c.shift(1))
    c_ret = np.log(c / o)
    rs = np.log(h / c) * np.log(h / o) + np.log(l / c) * np.log(l / o)
    var = (o_ret.rolling(win).var(ddof=1) + k * c_ret.rolling(win).var(ddof=1)
           + (1 - k) * rs.rolling(win).mean())
    yz = np.sqrt(var * 240)
    yv = yz.values
    pct = np.full(len(yv), np.nan)
    for t in range(len(yv)):
        if np.isnan(yv[t]):
            continue
        lo = max(0, t - pct_w + 1)
        w = yv[lo:t + 1]
        w = w[~np.isnan(w)]
        if len(w) < pct_w:
            continue
        x = yv[t]
        pct[t] = ((w < x).sum() + 0.5 * (w == x).sum()) / len(w) * 100.0
    return pd.Series(pct, index=yz.index)

ind = {}
for code in CODES:
    p = px[code]
    o, h, l, c = p["open"], p["high"], p["low"], p["close"]
    logm = np.log((o + h + l + c) / 4)
    num = logm - logm.shift(MOM - 1)                       # 净位移（带符号）
    den = logm.diff().abs().rolling(MOM - 1).sum()         # 路径长度
    er = num.abs() / den
    score = er * num                                       # ER × 净位移
    delta = c.diff()
    gain = delta.clip(lower=0).ewm(alpha=1 / RSI_N, adjust=False).mean()
    loss = (-delta.clip(upper=0)).ewm(alpha=1 / RSI_N, adjust=False).mean()
    rsi = 100 - 100 / (1 + gain / loss)
    ind[code] = {
        "o": o, "h": h, "l": l, "c": c,
        "score": score,
        "pct": yz_pct(o, h, l, c),
        "rsi": rsi,
        "ma5": c.rolling(5).mean(),
        "ma20": c.rolling(20).mean(),
        "dd20": c / h.rolling(20).max() - 1,
        "low10": l.rolling(10).min(),
    }

codes = list(CODES)
start_idx = max(int(ind[code]["pct"].first_valid_index() is not None and
                    ind[code]["pct"].index.get_loc(ind[code]["pct"].first_valid_index())
                    for code in codes) for code in codes) if False else \
    max(ind[code]["pct"].index.get_loc(ind[code]["pct"].first_valid_index()) for code in codes)
print(f"回测起点: {dates[start_idx]} (index {start_idx})")

setup = {code: (ind[code]["pct"] >= PCT_TH) & (ind[code]["dd20"] <= DD_TH) for code in codes}
trigger = {code: (ind[code]["rsi"].shift(1) <= RSI_TH) & (ind[code]["rsi"] > RSI_TH)
           & (ind[code]["c"] > ind[code]["h"].shift(1)) & setup[code] for code in codes}


def top_score(t):
    vals = {code: ind[code]["score"].iloc[t] for code in codes}
    vals = {k: v for k, v in vals.items() if not np.isnan(v)}
    return max(vals, key=vals.get) if vals else None


# ---------- 3. 基线：纯动量轮动 ----------
def run_baseline():
    ret = np.zeros(n)
    pos = None
    switches = 0
    for t in range(start_idx, n - 1):
        j = t + 1
        tgt = top_score(t)
        if tgt is None:
            continue
        if pos is None:
            a = ind[tgt]
            ret[j] += a["c"].iloc[j] / a["o"].iloc[j] - 1 - COST
            pos = tgt
            switches += 1
        elif tgt != pos:
            a, b = ind[pos], ind[tgt]
            ret[j] += a["o"].iloc[j] / a["c"].iloc[t] - 1 - COST
            ret[j] += b["c"].iloc[j] / b["o"].iloc[j] - 1 - COST
            pos = tgt
            switches += 1
        else:
            a = ind[pos]
            ret[j] += a["c"].iloc[j] / a["c"].iloc[t] - 1
    return pd.Series(ret, index=dates), switches


# ---------- 4. 优化：三态状态机 ----------
def run_overlay():
    ret = np.zeros(n)
    pos = None
    mode = "mom"
    reb = None
    switches = 0
    reb_trades = []
    reb_entry_i = None
    def_days = 0
    for t in range(start_idx, n - 1):
        j = t + 1
        if mode == "mom":
            if pos is not None and ind[pos]["pct"].iloc[t] >= PCT_TH and ind[pos]["c"].iloc[t] < ind[pos]["ma20"].iloc[t]:
                a = ind[pos]
                ret[j] += a["o"].iloc[j] / a["c"].iloc[t] - 1 - COST
                pos = None
                mode = "def"
                continue
            tgt = top_score(t)
            if tgt is None:
                continue
            if pos is None:
                a = ind[tgt]
                ret[j] += a["c"].iloc[j] / a["o"].iloc[j] - 1 - COST
                pos = tgt
                switches += 1
            elif tgt != pos:
                a, b = ind[pos], ind[tgt]
                ret[j] += a["o"].iloc[j] / a["c"].iloc[t] - 1 - COST
                ret[j] += b["c"].iloc[j] / b["o"].iloc[j] - 1 - COST
                pos = tgt
                switches += 1
            else:
                a = ind[pos]
                ret[j] += a["c"].iloc[j] / a["c"].iloc[t] - 1
        elif mode == "def":
            def_days += 1
            cands = [code for code in codes if bool(trigger[code].iloc[t])]
            if cands:
                pick = min(cands, key=lambda cd: ind[cd]["rsi"].iloc[t])
                a = ind[pick]
                ret[j] += a["c"].iloc[j] / a["o"].iloc[j] - 1 - COST
                pos = pick
                reb = pick
                reb_entry_i = j
                mode = "reb"
            else:
                tgt = top_score(t)
                if tgt is not None and ind[tgt]["c"].iloc[t] > ind[tgt]["ma20"].iloc[t]:
                    a = ind[tgt]
                    ret[j] += a["c"].iloc[j] / a["o"].iloc[j] - 1 - COST
                    pos = tgt
                    switches += 1
                    mode = "mom"
        else:  # reb
            a = ind[reb]
            exit_cond = (a["c"].iloc[t] < a["ma5"].iloc[t]
                         or a["c"].iloc[t] < a["low10"].iloc[t]
                         or a["score"].iloc[t] > 0)
            if exit_cond:
                ret[j] += a["o"].iloc[j] / a["c"].iloc[t] - 1 - COST
                reb_trades.append({
                    "asset": reb, "entry": dates[reb_entry_i], "exit": dates[j],
                    "ret": float(np.prod(1 + ret[reb_entry_i:j + 1]) - 1), "days": j - reb_entry_i,
                })
                pos = None
                reb = None
                mode = "mom"
            else:
                ret[j] += a["c"].iloc[j] / a["c"].iloc[t] - 1
    return pd.Series(ret, index=dates), {"switches": switches, "reb_trades": reb_trades, "def_days": def_days}


def metrics(ret):
    r = ret.iloc[start_idx:]
    nn = len(r)
    eq = (1 + r).cumprod()
    ann = float(eq.iloc[-1] ** (240 / nn) - 1)
    sharpe = float(r.mean() / r.std() * np.sqrt(240)) if r.std() > 0 else 0.0
    dd = eq / eq.cummax() - 1
    in_mkt = float((r != 0).mean())
    return {
        "ann": round(ann * 100, 2), "sharpe": round(sharpe, 2),
        "maxdd": round(float(dd.min()) * 100, 2),
        "total": round((float(eq.iloc[-1]) - 1) * 100, 1),
        "in_market": round(in_mkt * 100, 1),
    }, eq, dd


ret_base, sw_base = run_baseline()
ret_ol, ol_info = run_overlay()
ret_gold = ind["518880.SH"]["c"].pct_change().fillna(0)

m_base, eq_base, dd_base = metrics(ret_base)
m_ol, eq_ol, dd_ol = metrics(ret_ol)
m_gold, eq_gold, dd_gold = metrics(ret_gold)

out_dates = dates[start_idx:].tolist()
wins = [t for t in ol_info["reb_trades"] if t["ret"] > 0]
payload = {
    "meta": {
        "codes": CODES, "cost_bps_side": COST * 1e4, "mom": MOM,
        "pct_w": PCT_W, "pct_th": PCT_TH, "dd_th": DD_TH,
        "rsi_n": RSI_N, "rsi_th": RSI_TH,
        "start": out_dates[0], "end": out_dates[-1], "n_days": len(out_dates),
    },
    "dates": out_dates,
    "variants": [
        {"key": "base", "name": "纯动量轮动（基线）", "metrics": {**m_base, "switches": sw_base},
         "equity": eq_base.iloc[start_idx:].round(4).tolist(), "dd": dd_base.iloc[start_idx:].round(4).tolist()},
        {"key": "overlay", "name": "动量 + 状态机优化", "metrics": {**m_ol, "switches": ol_info["switches"]},
         "equity": eq_ol.iloc[start_idx:].round(4).tolist(), "dd": dd_ol.iloc[start_idx:].round(4).tolist()},
        {"key": "gold", "name": "黄金ETF 买入持有（参考）", "metrics": {**m_gold, "switches": 0},
         "equity": eq_gold.iloc[start_idx:].round(4).tolist(), "dd": dd_gold.iloc[start_idx:].round(4).tolist()},
    ],
    "rebound": {
        "trades": ol_info["reb_trades"],
        "n": len(ol_info["reb_trades"]),
        "winrate": round(len(wins) / len(ol_info["reb_trades"]) * 100, 1) if ol_info["reb_trades"] else None,
        "avg_ret": round(float(np.mean([t["ret"] for t in ol_info["reb_trades"]])) * 100, 2) if ol_info["reb_trades"] else None,
        "def_days": ol_info["def_days"],
    },
}
(HERE / "rotation.json").write_text(json.dumps(payload, ensure_ascii=False))

print(f"\n{'策略':<24}{'年化%':>8}{'夏普':>7}{'最大回撤%':>10}{'累计%':>9}{'在场%':>7}{'换仓':>6}")
for v in payload["variants"]:
    m = v["metrics"]
    print(f"{v['name']:<24}{m['ann']:>8}{m['sharpe']:>7}{m['maxdd']:>10}{m['total']:>9}{m['in_market']:>7}{m['switches']:>6}")
print(f"\n反弹交易 {payload['rebound']['n']} 笔, 胜率 {payload['rebound']['winrate']}%, 笔均 {payload['rebound']['avg_ret']}%, 防御天数 {payload['rebound']['def_days']}")
for t in ol_info["reb_trades"][:12]:
    print(f"  {CODES[t['asset']]:<6} {t['entry']} → {t['exit']}  {t['ret']*100:+.2f}%  {t['days']}d")
