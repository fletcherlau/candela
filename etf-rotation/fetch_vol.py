"""拉取 518880（华安黄金ETF）日线数据，计算 Yang-Zhang / Close-to-Close / Parkinson 波动率，导出 JSON。

token 从环境变量 TUSHARE_TOKEN 读取，不写入任何文件。
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

OUT = Path(__file__).parent / "518880_vol.json"

WINDOW = 20
PERIODS = 240  # A 股年化约定整数（518880 实测年均 243.2，取整差异 <1%）

pro = ts.pro_api(TOKEN)
df = pro.fund_daily(ts_code="518880.SH", start_date="20130101", end_date="20260805")
if df is None or df.empty:
    sys.exit("fund_daily returned no data")

df = df.sort_values("trade_date").reset_index(drop=True)
df["date"] = pd.to_datetime(df["trade_date"]).dt.strftime("%Y-%m-%d")
for c in ["open", "high", "low", "close"]:
    df[c] = df[c].astype(float)

n = WINDOW
k = 0.34 / (1.34 + (n + 1) / (n - 1))

# --- Yang-Zhang ---
o_ret = np.log(df["open"] / df["close"].shift(1))          # 隔夜
c_ret = np.log(df["close"] / df["open"])                   # 盘中
rs = (np.log(df["high"] / df["close"]) * np.log(df["high"] / df["open"])
      + np.log(df["low"] / df["close"]) * np.log(df["low"] / df["open"]))
var_o = o_ret.rolling(n).var(ddof=1)
var_c = c_ret.rolling(n).var(ddof=1)
var_rs = rs.rolling(n).mean()
df["vol_yz"] = np.sqrt((var_o + k * var_c + (1 - k) * var_rs) * PERIODS)

# --- Close-to-Close ---
cc = np.log(df["close"] / df["close"].shift(1))
df["vol_cc"] = cc.rolling(n).std(ddof=1) * np.sqrt(PERIODS)

# --- Parkinson ---
df["vol_park"] = np.sqrt(
    (np.log(df["high"] / df["low"]) ** 2).rolling(n).mean() / (4 * np.log(2)) * PERIODS
)

latest = df.iloc[-1]
payload = {
    "meta": {
        "ts_code": "518880.SH",
        "name": "华安黄金ETF",
        "window": WINDOW,
        "k": round(k, 6),
        "periods": PERIODS,
        "start": df["date"].iloc[0],
        "end": df["date"].iloc[-1],
        "count": int(len(df)),
        "latest": {
            "date": latest["date"],
            "close": round(latest["close"], 3),
            "vol_yz": round(float(latest["vol_yz"]) * 100, 2),
            "vol_cc": round(float(latest["vol_cc"]) * 100, 2),
            "vol_park": round(float(latest["vol_park"]) * 100, 2),
        },
        "stats": {
            "yz_mean": round(float(df["vol_yz"].mean()) * 100, 2),
            "yz_max": round(float(df["vol_yz"].max()) * 100, 2),
            "yz_min": round(float(df["vol_yz"].min()) * 100, 2),
        },
    },
    "dates": df["date"].tolist(),
    "ohlc": df[["open", "high", "low", "close"]].round(3).values.tolist(),
    "vol_yz": [None if pd.isna(x) else round(x * 100, 2) for x in df["vol_yz"]],
    "vol_cc": [None if pd.isna(x) else round(x * 100, 2) for x in df["vol_cc"]],
    "vol_park": [None if pd.isna(x) else round(x * 100, 2) for x in df["vol_park"]],
}

OUT.write_text(json.dumps(payload, ensure_ascii=False))
print(f"rows={len(df)}  range={payload['meta']['start']}..{payload['meta']['end']}")
print("latest:", json.dumps(payload["meta"]["latest"], ensure_ascii=False))
print("stats :", json.dumps(payload["meta"]["stats"], ensure_ascii=False))
