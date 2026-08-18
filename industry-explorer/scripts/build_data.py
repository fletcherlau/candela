#!/usr/bin/env python
"""Build JSON data for the industry-explorer frontend.

Reads SW industry index daily bars (L1/L2 CSVs) and the SW2021 classification
from smallcap-rotation/data/, then emits into public/data/:
  - manifest.json: {"l1": [{"code", "name"}], "l2": [{"code", "name", "parent_code"}], "generated_at": ...}
    Only indices actually present in the CSVs are included. L2 parent_code is the
    parent L1's index_code (801xxx.SI) so the frontend can group directly.
  - bars/<code>.json: [[trade_date "YYYY-MM-DD", open, high, low, close, vol], ...]
    sorted ascending; prices rounded to 2 decimals, vol to integer.

Run with the repo venv: ../../.venv/bin/python scripts/build_data.py
"""

import json
from datetime import datetime, timezone
from pathlib import Path

import pandas as pd

REPO_ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = REPO_ROOT / "smallcap-rotation" / "data"
OUT_DIR = REPO_ROOT / "industry-explorer" / "public" / "data"
BARS_DIR = OUT_DIR / "bars"

L1_CSV = DATA_DIR / "sw_daily_l1_20060101_20251231.csv"
L2_CSV = DATA_DIR / "sw_daily_l2_20060101_20251231.csv"
CLASS_JSON = DATA_DIR / "sw_industry_sw2021.json"


def load_bars(csv_path: Path) -> dict[str, list]:
    """Return {ts_code: [[date, o, h, l, c, vol], ...]} sorted ascending by date."""
    df = pd.read_csv(
        csv_path,
        usecols=["ts_code", "trade_date", "open", "high", "low", "close", "vol"],
        dtype={"trade_date": str},
    )
    df["date"] = df["trade_date"].str.slice(0, 4) + "-" + df["trade_date"].str.slice(4, 6) + "-" + df["trade_date"].str.slice(6, 8)
    df = df.sort_values(["ts_code", "trade_date"])
    bars = {}
    for code, g in df.groupby("ts_code", sort=True):
        rows = [
            [d, round(o, 2), round(h, 2), round(lo, 2), round(c, 2), int(round(v))]
            for d, o, h, lo, c, v in zip(
                g["date"], g["open"], g["high"], g["low"], g["close"], g["vol"]
            )
        ]
        bars[code] = rows
    return bars


def main() -> None:
    classification = json.loads(CLASS_JSON.read_text(encoding="utf-8"))
    l1_meta = {x["index_code"]: x for x in classification["l1"]}
    l2_meta = {x["index_code"]: x for x in classification["l2"]}
    # numeric L1 industry_code -> L1 index_code
    ind_code_to_l1 = {x["industry_code"]: x["index_code"] for x in classification["l1"]}

    l1_bars = load_bars(L1_CSV)
    l2_bars = load_bars(L2_CSV)

    manifest_l1 = [
        {"code": code, "name": l1_meta[code]["industry_name"]}
        for code in sorted(l1_bars)
        if code in l1_meta
    ]
    manifest_l2 = []
    for code in sorted(l2_bars):
        meta = l2_meta.get(code)
        if meta is None:
            continue
        parent_index_code = ind_code_to_l1.get(meta["parent_code"])
        if parent_index_code is None or parent_index_code not in l1_bars:
            continue
        manifest_l2.append(
            {"code": code, "name": meta["industry_name"], "parent_code": parent_index_code}
        )

    BARS_DIR.mkdir(parents=True, exist_ok=True)
    all_bars = {**l1_bars, **l2_bars}
    for code, rows in all_bars.items():
        (BARS_DIR / f"{code}.json").write_text(
            json.dumps(rows, separators=(",", ":")), encoding="utf-8"
        )

    manifest = {
        "l1": manifest_l1,
        "l2": manifest_l2,
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }
    (OUT_DIR / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, separators=(",", ":")), encoding="utf-8"
    )

    print(f"L1 indices with data: {len(manifest_l1)}")
    print(f"L2 indices with data: {len(manifest_l2)}")
    print(f"bar files written:    {len(all_bars)} -> {BARS_DIR}")
    print(f"manifest written:     {OUT_DIR / 'manifest.json'}")


if __name__ == "__main__":
    main()
