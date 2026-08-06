"""研究：YZ 波动率的 1200 日滚动分位数 与 "过去20日收益 vs 未来1日收益" 条件相关性的关系。

检验假设：低波动分位 → 动量（正相关），高波动分位 → 反转（负相关）。
显著性用 Newey-West HAC（lag=20，匹配 r20 的 19 日重叠）。
输出 study.json 供网页展示。
"""
import json
from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).parent
d = json.loads((HERE / "518880_vol.json").read_text())

PCT_WINDOW = 1200
MOM_WINDOW = 20
NW_LAG = 20

dates = pd.Series(d["dates"])
close = pd.Series([r[3] for r in d["ohlc"]], dtype=float)
yz = pd.Series([np.nan if v is None else v / 100.0 for v in d["vol_yz"]])

# --- 1. YZ 的 1200 日滚动分位（含当日，并列取半） ---
yzv = yz.values
n = len(yzv)
pct = np.full(n, np.nan)
for t in range(n):
    if np.isnan(yzv[t]):
        continue
    lo = max(0, t - PCT_WINDOW + 1)
    win = yzv[lo : t + 1]
    win = win[~np.isnan(win)]
    if len(win) < PCT_WINDOW:  # 严格满 1200 日窗口才给分位
        continue
    x = yzv[t]
    pct[t] = ((win < x).sum() + 0.5 * (win == x).sum()) / len(win) * 100.0

# --- 2. 动量信号与未来收益 ---
r20 = close.pct_change(MOM_WINDOW)
fwd1 = close.pct_change().shift(-1)

df = pd.DataFrame({"date": dates, "pct": pct, "r20": r20, "fwd1": fwd1}).dropna().reset_index(drop=True)


def nw_ols(X: np.ndarray, y: np.ndarray, lag: int = NW_LAG):
    """OLS + Newey-West HAC，返回 beta 与 t 值"""
    T, k = X.shape
    XtX_inv = np.linalg.inv(X.T @ X)
    beta = XtX_inv @ X.T @ y
    e = y - X @ beta
    xe = X * e[:, None]
    S = xe.T @ xe
    for l in range(1, lag + 1):
        w = 1.0 - l / (lag + 1.0)
        G = xe[l:].T @ xe[: T - l]
        S += w * (G + G.T)
    V = XtX_inv @ S @ XtX_inv
    se = np.sqrt(np.maximum(np.diag(V), 0))
    t = np.where(se > 0, beta / se, np.nan)
    return beta, t


# --- 3. 十分桶条件相关性 ---
deciles = []
for b in range(10):
    lo, hi = b * 10.0, (b + 1) * 10.0
    m = (df["pct"] >= lo) & (df["pct"] < hi if b < 9 else df["pct"] <= hi)
    sub = df[m]
    x, y = sub["r20"].values, sub["fwd1"].values
    corr = float(np.corrcoef(x, y)[0, 1])
    X = np.column_stack([np.ones(len(sub)), x])
    beta, tstat = nw_ols(X, y)
    pos = sub[sub["r20"] > 0]["fwd1"].mean() * 1e4  # bps：信号为正时做多
    neg = sub[sub["r20"] < 0]["fwd1"].mean() * 1e4
    deciles.append({
        "bucket": f"{int(lo)}-{int(hi)}",
        "n": int(len(sub)),
        "corr": round(corr, 4),
        "beta": round(float(beta[1]), 4),
        "t": round(float(tstat[1]), 2),
        "bps_if_r20_pos": round(float(pos), 1),
        "bps_if_r20_neg": round(float(neg), 1),
    })

# --- 4. 全样本交互项回归 ---
z = (df["pct"] - 50.0) / 50.0
x, y = df["r20"].values, df["fwd1"].values
X = np.column_stack([np.ones(len(df)), x, x * z])
beta, tstat = nw_ols(X, y)
interaction = {
    "n": int(len(df)),
    "beta_mom": round(float(beta[1]), 4),
    "t_mom": round(float(tstat[1]), 2),
    "gamma_interaction": round(float(beta[2]), 4),
    "t_interaction": round(float(tstat[2]), 2),
}

low = [q for q in deciles[:2]]
high = [q for q in deciles[-2:]]
summary = {
    "low20_corr": round(float(np.mean([q["corr"] for q in low])), 4),
    "high20_corr": round(float(np.mean([q["corr"] for q in high])), 4),
}

payload = {
    "meta": {
        "pct_window": PCT_WINDOW,
        "mom_window": MOM_WINDOW,
        "nw_lag": NW_LAG,
        "n": int(len(df)),
        "start": df["date"].iloc[0],
        "end": df["date"].iloc[-1],
    },
    "interaction": interaction,
    "summary": summary,
    "deciles": deciles,
    "pct_series": {
        "dates": dates.tolist(),
        "pct": [None if np.isnan(v) else round(float(v), 1) for v in pct],
    },
}
(HERE / "study.json").write_text(json.dumps(payload, ensure_ascii=False))

# --- 控制台摘要 ---
print(f"样本: {interaction['n']} 天  {df['date'].iloc[0]} ~ {df['date'].iloc[-1]}")
print(f"交互项回归: β_mom={interaction['beta_mom']} (t={interaction['t_mom']})  "
      f"γ_交互={interaction['gamma_interaction']} (t={interaction['t_interaction']})")
print(f"{'分位桶':>8} {'n':>5} {'corr':>8} {'t(NW)':>7} {'bps|r20>0':>10} {'bps|r20<0':>10}")
for q in deciles:
    print(f"{q['bucket']:>8} {q['n']:>5} {q['corr']:>8} {q['t']:>7} {q['bps_if_r20_pos']:>10} {q['bps_if_r20_neg']:>10}")
