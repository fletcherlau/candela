import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  RefreshCw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

type Day = {
  suspended?: string[];
  date: string;
  nav: number;
  holding: string;
  weight: number;
  cashWeight: number;
  cost: number;
  turnover: number;
  benchmarks: number[];
};
type Result = {
  version: string;
  start: string;
  end: string;
  days: Day[];
  codes: string[];
  names: string[];
  costBps: number;
};
type View = {
  status: string;
  message: string;
  updatedAt: string;
  result: Result | null;
};
const colors = ["#c57838", "#bf9c32", "#687cbb", "#ae6487"];
const fmt = (s: string) => `${s.slice(0, 4)}-${s.slice(4, 6)}-${s.slice(6, 8)}`;
const pct = (n: number) => `${(n * 100).toFixed(2)}%`;
const statuses: Record<string, string> = {
  ready: "已更新",
  pending: "等待更新",
  computing: "更新中",
  syncing: "行情同步中",
  failed: "更新未完成",
  stale: "数据待更新",
};

export function Rotation() {
  const [view, setView] = useState<View | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [range, setRange] = useState<[number, number]>([0, 0]);
  const [selected, setSelected] = useState<boolean[]>([
    false,
    false,
    false,
    false,
  ]);
  const [hover, setHover] = useState<number | null>(null);
  const initialized = useRef("");
  const drag = useRef<number | null>(null);
  const chartNode = useRef<SVGSVGElement | null>(null);
  const [chartWidth, setChartWidth] = useState(1000);
  useEffect(() => {
    const node = chartNode.current;
    if (!node) return;
    const observer = new ResizeObserver((entries) =>
      setChartWidth(Math.max(260, entries[0].contentRect.width)),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [view?.result?.end]);
  async function load() {
    setBusy(true);
    try {
      const res = await fetch("/api/rotation/backtest");
      if (!res.ok) throw new Error("暂时无法读取回测，请稍后重试。");
      const next: View = await res.json();
      setView(next);
      setError("");
      if (next.result && next.result.end !== initialized.current) {
        const days = next.result.days;
        const end = new Date(`${fmt(next.result.end)}T00:00:00Z`);
        end.setUTCFullYear(end.getUTCFullYear() - 1);
        const start = end.toISOString().slice(0, 10).replaceAll("-", "");
        setRange([
          Math.max(
            0,
            days.findIndex((d) => d.date >= start),
          ),
          days.length - 1,
        ]);
        initialized.current = next.result.end;
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "暂时无法读取回测");
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 60000);
    return () => clearInterval(timer);
  }, []);
  const result = view?.result;
  const days = result?.days ?? [];
  const [lo, hi] = range;
  const visible = days.slice(lo, hi + 1);
  const series = useMemo(() => {
    if (!visible.length)
      return { returns: [], dd: [], comparisons: [], maxdd: 0, gain: 0 };
    const base = lo === 0 ? 1 : visible[0].nav;
    let peak = base;
    const returns = visible.map((d) => d.nav / base - 1);
    const dd = visible.map((d) => {
      peak = Math.max(peak, d.nav);
      return d.nav / peak - 1;
    });
    const comparisons = [0, 1, 2, 3].map((j) =>
      visible.map((d) => d.benchmarks[j] / visible[0].benchmarks[j] - 1),
    );
    return {
      returns,
      dd,
      comparisons,
      maxdd: Math.min(0, ...dd),
      gain: returns[returns.length - 1],
    };
  }, [result, lo, hi]);
  const active = visible[hover ?? visible.length - 1];
  const activeIndex = hover ?? visible.length - 1;
  function change(a: number, b: number) {
    setHover(null);
    setRange([
      Math.max(0, Math.min(a, b)),
      Math.min(days.length - 1, Math.max(a, b)),
    ]);
  }
  function preset(months: number | null) {
    if (!days.length) return;
    if (months === null) return change(0, days.length - 1);
    const end = new Date(`${fmt(days[days.length - 1].date)}T00:00:00Z`);
    end.setUTCMonth(end.getUTCMonth() - months);
    const start = end.toISOString().slice(0, 10).replaceAll("-", "");
    change(
      Math.max(
        0,
        days.findIndex((d) => d.date >= start),
      ),
      days.length - 1,
    );
  }
  function zoom(scale: number) {
    const mid = (lo + hi) / 2,
      half = Math.max(1, ((hi - lo) * scale) / 2);
    change(Math.floor(mid - half), Math.ceil(mid + half));
  }
  function pan(direction: number) {
    const step = Math.max(1, Math.round((hi - lo + 1) / 3)) * direction;
    const shift = Math.max(-lo, Math.min(days.length - 1 - hi, step));
    change(lo + shift, hi + shift);
  }
  function pointerIndex(e: React.PointerEvent<SVGSVGElement>) {
    const box = e.currentTarget.getBoundingClientRect();
    return Math.max(
      0,
      Math.min(
        visible.length - 1,
        Math.round(
          ((((e.clientX - box.left) / box.width) * chartWidth - 50) /
            (chartWidth - 65)) *
            (visible.length - 1),
        ),
      ),
    );
  }
  const x = (i: number) =>
    50 + (i / Math.max(1, visible.length - 1)) * (chartWidth - 65);
  function plot(
    values: number[][],
    palette: string[],
    height: number,
    label: string,
    filled = false,
  ) {
    const all = values.flat();
    const min = Math.min(0, ...all),
      max = Math.max(0, ...all);
    const span = max - min || 0.01;
    const y = (v: number) => 16 + ((max - v) / span) * (height - 48);
    const path = (v: number[]) =>
      v
        .map((n, i) => `${i ? "L" : "M"}${x(i).toFixed(2)},${y(n).toFixed(2)}`)
        .join(" ");
    return (
      <svg
        ref={label === "策略与 ETF 累计收益图" ? chartNode : undefined}
        role="img"
        aria-label={label}
        viewBox={`0 0 ${chartWidth} ${height}`}
        className="rotation-chart w-full select-none"
        onPointerMove={(e) => setHover(pointerIndex(e))}
        onPointerLeave={() => {
          if (drag.current === null) setHover(null);
        }}
        onPointerDown={(e) => {
          if (e.button !== 0) return;
          drag.current = pointerIndex(e);
          e.currentTarget.setPointerCapture(e.pointerId);
        }}
        onPointerCancel={() => {
          drag.current = null;
        }}
        onPointerUp={(e) => {
          const start = drag.current;
          drag.current = null;
          if (start !== null) {
            const end = pointerIndex(e);
            if (Math.abs(end - start) > 2)
              change(lo + Math.min(start, end), lo + Math.max(start, end));
          }
        }}
      >
        {[0, 1, 2, 3].map((i) => {
          const v = max - (span * i) / 3;
          return (
            <g key={i}>
              <line
                x1="50"
                x2={chartWidth - 15}
                y1={y(v)}
                y2={y(v)}
                stroke="#e9eae5"
              />
              <text
                x="42"
                y={y(v) + 4}
                textAnchor="end"
                fill="#727a70"
                fontSize="11"
              >
                {pct(v)}
              </text>
            </g>
          );
        })}
        {values.map((v, j) => (
          <g key={j}>
            {filled && (
              <path
                d={`${path(v)} L${x(v.length - 1)},${y(0)} L50,${y(0)} Z`}
                fill={palette[j]}
                opacity=".10"
              />
            )}
            <path
              d={path(v)}
              stroke={palette[j]}
              strokeWidth={j === 0 ? 2.5 : 1.7}
              fill="none"
              vectorEffect="non-scaling-stroke"
            />
          </g>
        ))}
        {hover !== null && (
          <line
            x1={x(hover)}
            x2={x(hover)}
            y1="12"
            y2={height - 25}
            stroke="#658164"
            strokeDasharray="4 4"
          />
        )}
        {visible.length > 0 && (
          <>
            <text x="50" y={height - 4} fill="#727a70" fontSize="11">
              {fmt(visible[0].date)}
            </text>
            <text
              x={chartWidth - 15}
              y={height - 4}
              textAnchor="end"
              fill="#727a70"
              fontSize="11"
            >
              {fmt(visible[visible.length - 1].date)}
            </text>
          </>
        )}
      </svg>
    );
  }
  return (
    <div className="space-y-7 py-10 sm:py-14">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="mb-3 text-xs tracking-[.2em] text-primary">
            FOUR ASSET ROTATION
          </p>
          <h1 className="text-3xl font-semibold">四标的轮动</h1>
          <p className="mt-3 text-sm text-muted-foreground">
            红利、黄金、创业板与纳指，观察轮动策略的历史表现。
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Badge variant="secondary">历史回测</Badge>
          <Button
            variant="outline"
            size="icon"
            aria-label="刷新回测"
            disabled={busy}
            onClick={() => void load()}
          >
            <RefreshCw className="size-4" />
          </Button>
        </div>
      </div>
      {error && (
        <div
          role="alert"
          className="rounded-lg border border-destructive/30 p-4 text-sm text-destructive"
        >
          {error}
        </div>
      )}
      {view && (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <Badge variant={view.status === "ready" ? "outline" : "secondary"}>
            {statuses[view.status] ?? "等待结果"}
          </Badge>
          {result && (
            <span className="text-muted-foreground">
              数据截至 {fmt(result.end)}
            </span>
          )}
          {view.message && (
            <span role="status" className="text-muted-foreground">
              {view.message}
            </span>
          )}
        </div>
      )}
      {!view && !error && (
        <p role="status" className="py-24 text-center text-muted-foreground">
          正在读取回测…
        </p>
      )}
      {view && !result && (
        <Card className="py-16 text-center shadow-none">
          <CardContent>
            <h2 className="text-lg font-medium">暂无完整回测结果</h2>
            <p className="mt-3 text-sm text-muted-foreground">
              所需行情准备完成后，这里会显示收益与持仓历史。
            </p>
          </CardContent>
        </Card>
      )}
      {result && visible.length > 0 && (
        <>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <Stat
              title="区间收益"
              value={visible.length > 1 ? pct(series.gain) : "—"}
            />
            <Stat
              title="区间最大回撤"
              value={visible.length > 1 ? pct(series.maxdd) : "—"}
            />
            <div className="col-span-2 sm:col-span-1">
              <Stat
                title="观察区间"
                value={`${fmt(visible[0].date)} — ${fmt(visible[visible.length - 1].date)}`}
                small
              />
            </div>
          </div>
          <Card className="gap-4 shadow-none">
            <CardHeader className="gap-4 border-b">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <CardTitle>累计收益</CardTitle>
                <div className="flex flex-wrap gap-1">
                  {[
                    ["近三月", 3],
                    ["近一年", 12],
                    ["近三年", 36],
                    ["全部", null],
                  ].map(([label, months]) => (
                    <Button
                      key={label}
                      variant="ghost"
                      size="sm"
                      onClick={() => preset(months as number | null)}
                    >
                      {label}
                    </Button>
                  ))}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-x-5 gap-y-3 text-sm">
                <span className="font-medium text-primary">
                  ● 轮动策略 · 扣费后
                </span>
                {result.names.map((name, j) => (
                  <label
                    key={name}
                    className="inline-flex cursor-pointer items-center gap-2"
                  >
                    <input
                      type="checkbox"
                      checked={selected[j]}
                      onChange={(e) =>
                        setSelected(
                          selected.map((v, k) =>
                            k === j ? e.target.checked : v,
                          ),
                        )
                      }
                    />
                    <svg width="8" height="8" aria-hidden="true">
                      <circle cx="4" cy="4" r="4" fill={colors[j]} />
                    </svg>
                    {name}
                  </label>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                ETF
                对照为复权后的买入持有收益，未扣交易成本。拖动图表可放大选定区间。
              </p>
            </CardHeader>
            <CardContent className="px-2 sm:px-6">
              {plot(
                [
                  series.returns,
                  ...series.comparisons.filter((_, j) => selected[j]),
                ],
                ["#3c694c", ...colors.filter((_, j) => selected[j])],
                300,
                "策略与 ETF 累计收益图",
              )}
              <div className="mt-3 flex flex-wrap items-center justify-between gap-3 px-4">
                <div className="flex gap-1">
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="向前平移"
                    onClick={() => pan(-1)}
                  >
                    <ArrowLeft />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="放大区间"
                    onClick={() => zoom(0.5)}
                  >
                    <ZoomIn />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="缩小区间"
                    onClick={() => zoom(2)}
                  >
                    <ZoomOut />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="向后平移"
                    onClick={() => pan(1)}
                  >
                    <ArrowRight />
                  </Button>
                </div>
                <div className="grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 sm:w-auto">
                  <Input
                    className="min-w-0"
                    aria-label="开始日期"
                    type="date"
                    value={fmt(days[lo].date)}
                    min={fmt(result.start)}
                    max={fmt(days[hi].date)}
                    onChange={(e) => {
                      const d = e.target.value.replaceAll("-", "");
                      const i = days.findIndex((v) => v.date >= d);
                      if (d && i >= 0) change(Math.min(i, hi), hi);
                    }}
                  />
                  <span>—</span>
                  <Input
                    className="min-w-0"
                    aria-label="结束日期"
                    type="date"
                    value={fmt(days[hi].date)}
                    min={fmt(days[lo].date)}
                    max={fmt(result.end)}
                    onChange={(e) => {
                      const d = e.target.value.replaceAll("-", "");
                      let i = days.length - 1;
                      while (i >= 0 && days[i].date > d) i--;
                      if (d && i >= 0) change(lo, Math.max(i, lo));
                    }}
                  />
                </div>
              </div>
              <div className="mt-4 grid gap-2 px-4">
                <input
                  aria-label="区间起点"
                  type="range"
                  min="0"
                  max={days.length - 1}
                  value={lo}
                  onChange={(e) =>
                    change(Math.min(Number(e.target.value), hi), hi)
                  }
                />
                <input
                  aria-label="区间终点"
                  type="range"
                  min="0"
                  max={days.length - 1}
                  value={hi}
                  onChange={(e) =>
                    change(lo, Math.max(Number(e.target.value), lo))
                  }
                />
              </div>
            </CardContent>
          </Card>
          <Card className="shadow-none">
            <CardHeader>
              <CardTitle>区间回撤</CardTitle>
              <p className="text-xs text-muted-foreground">
                相对于所选区间内的净值峰值。
              </p>
            </CardHeader>
            <CardContent className="px-2 sm:px-6">
              {plot([series.dd], ["#b57869"], 155, "策略区间回撤图", true)}
            </CardContent>
          </Card>
          <Card className="shadow-none">
            <CardHeader>
              <CardTitle>持仓变化</CardTitle>
              <p className="text-xs text-muted-foreground">
                每日收盘后的 ETF 仓位；剩余为不计息现金。
              </p>
            </CardHeader>
            <CardContent className="px-2 sm:px-6">
              <svg
                role="img"
                aria-label="ETF 与现金历史仓位图"
                viewBox={`0 0 ${chartWidth} 135`}
                className="rotation-chart w-full"
                onPointerMove={(e) => setHover(pointerIndex(e))}
                onPointerLeave={() => setHover(null)}
              >
                <rect
                  x="50"
                  y="8"
                  width={chartWidth - 65}
                  height="90"
                  fill="#f0f0eb"
                />
                {visible.map((d, i) => (
                  <rect
                    key={d.date}
                    x={50 + (i / visible.length) * (chartWidth - 65)}
                    y={8 + (1 - d.weight) * 90}
                    width={(chartWidth - 65) / visible.length + 0.2}
                    height={d.weight * 90}
                    fill={colors[result.codes.indexOf(d.holding)]}
                  />
                ))}
                <text
                  x="42"
                  y="17"
                  textAnchor="end"
                  fontSize="11"
                  fill="#727a70"
                >
                  100%
                </text>
                <text
                  x="42"
                  y="98"
                  textAnchor="end"
                  fontSize="11"
                  fill="#727a70"
                >
                  0%
                </text>
                {hover !== null && (
                  <line
                    x1={x(hover)}
                    x2={x(hover)}
                    y1="8"
                    y2="98"
                    stroke="#223d2c"
                  />
                )}
                <text x="50" y="126" fontSize="11" fill="#727a70">
                  {fmt(visible[0].date)}
                </text>
                <text
                  x={chartWidth - 15}
                  y="126"
                  textAnchor="end"
                  fontSize="11"
                  fill="#727a70"
                >
                  {fmt(visible[visible.length - 1].date)}
                </text>
              </svg>
              <div className="mt-3 flex flex-wrap gap-4 px-4 text-xs text-muted-foreground">
                {[...result.names, "现金"].map((n, i) => (
                  <span key={n} className="inline-flex items-center gap-1">
                    <svg width="8" height="8" aria-hidden="true">
                      <rect
                        width="8"
                        height="8"
                        fill={colors[i] ?? "#deded5"}
                      />
                    </svg>
                    {n}
                  </span>
                ))}
              </div>
            </CardContent>
          </Card>
          {active && (
            <div
              aria-live="polite"
              className="rounded-lg border bg-white p-5 text-sm"
              data-testid="rotation-detail"
            >
              <span className="mr-5 font-medium">{fmt(active.date)}</span>
              <span className="mr-5">
                区间收益 {pct(series.returns[activeIndex])}
              </span>
              <span className="mr-5">回撤 {pct(series.dd[activeIndex])}</span>
              <span className="mr-5">
                {result.names[result.codes.indexOf(active.holding)]}{" "}
                {pct(active.weight)}
              </span>
              <span>现金 {pct(active.cashWeight)}</span>
              {active.suspended?.length ? (
                <p className="mt-2 text-xs text-muted-foreground">
                  当日已确认停牌：{active.suspended.join("、")}
                  ，以最近有效收盘估值，不参与交易。
                </p>
              ) : null}
            </div>
          )}
          <details className="rounded-lg border p-5 text-sm">
            <summary className="cursor-pointer font-medium">
              策略与回测口径
            </summary>
            <div className="mt-4 space-y-3 leading-7 text-muted-foreground">
              <p>
                ER 动量轮动，20 日窗口；换仓差距缓冲 0.005，波动分位 70/40
                节流。仓位偏差达到 5 个百分点才微调，无安全阀与吊灯止损。
              </p>
              <p>
                单边成本 10bps（0.1%），现金收益为
                0。历史回测采用完整日线信号及当日收盘价近似成交，未模拟纳指溢价过滤；与真实账户执行存在差异。
              </p>
              <p>
                已核实的整日停牌不参与交易或指标采样，按最近有效收盘价估值；其他行情缺口会暂停更新。
              </p>
              <p>
                完整历史连续计算，缩放仅改变展示范围。策略扣除交易成本，对照 ETF
                未扣成本；全历史包含首次买入费用。
              </p>
              <p>
                有效历史 {fmt(result.start)} 至 {fmt(result.end)} · 计算版本{" "}
                {result.version}
              </p>
            </div>
          </details>
        </>
      )}
    </div>
  );
}
function Stat({
  title,
  value,
  small,
}: {
  title: string;
  value: string;
  small?: boolean;
}) {
  return (
    <div className="h-full rounded-xl border bg-white px-5 py-5">
      <p className="text-xs text-muted-foreground">{title}</p>
      <p
        className={`${small ? "text-sm" : "text-2xl"} mt-3 font-medium tabular-nums`}
      >
        {value}
      </p>
    </div>
  );
}
