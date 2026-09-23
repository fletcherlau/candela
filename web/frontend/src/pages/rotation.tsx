import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  RefreshCw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { EChartsCoreOption } from "echarts/core";
import { ResearchTheme } from "@/components/research-theme";
import { RotationDaily } from "@/components/rotation-daily";
import { RotationChart } from "@/components/rotation-chart";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { researchTheme as theme } from "@/lib/research-theme";
import {
  finite,
  fmt,
  pct,
  number,
  publishedAt,
  holdingName,
  holdingChange,
  rangeSeries,
  type View,
  type Result,
} from "@/lib/rotation";

const statuses: Record<string, string> = {
  ready: "已发布",
  pending: "等待更新",
  computing: "回测计算中",
  syncing: "行情同步中",
  failed: "更新失败",
  stale: "数据滞后",
};
const universe = [
  ["510880.SH", "红利 ETF"],
  ["518880.SH", "黄金 ETF"],
  ["159915.SZ", "创业板 ETF"],
  ["513100.SH", "纳指 ETF"],
];
const color = (code: string) =>
  theme.series[universe.findIndex(([c]) => c === code) + 1] ?? theme.muted;
const lineType = ["solid", "dashed", "dotted", "dashed"] as const;

function Method({ result }: { result?: Result | null }) {
  const known = result?.version === "v1.4-close-5pp-cash0-cost10-v1";
  return (
    <div className="rotation-method">
      {known ? (
        <p>
          ER 动量轮动，20 日窗口；换仓差距缓冲 0.005，波动分位 70/40
          节流。仓位偏差达到 5 个百分点才微调，无安全阀与吊灯止损。
        </p>
      ) : (
        <p>
          轮动模型依据动量与波动率分配仓位。
          {result
            ? "当前发布版本的具体参数尚未在本页核对，请以该版本说明为准。"
            : "完整参数随已发布结果展示。"}
        </p>
      )}
      <p>
        单边交易成本：
        {finite(result?.costBps)
          ? `${result.costBps} bps（${pct(result.costBps / 10000)}）`
          : "—（未提供）"}
        。策略为扣费净收益，对照 ETF 为后复权买入持有毛收益，未扣交易成本。
      </p>
      {known && (
        <>
          <p>
            现金收益按零处理。历史回测采用完整日线信号及当日收盘价近似成交，未模拟纳指溢价过滤；与实际账户执行存在差异。
          </p>
          <p>
            已核实的整日停牌不参与交易或指标采样，按最近有效收盘价估值；其他行情缺口会暂停更新。
          </p>
        </>
      )}
      <p>
        完整历史连续计算，缩放只改变展示范围。完整历史以初始净值 1
        为基准，包含首次买入费用；局部区间按区间首日净值归一化，回撤相对区间内净值峰值。
      </p>
      <p>
        来源：Tushare 基金日线及复权因子，经 Candela 历史回测发布。有效历史{" "}
        {fmt(result?.start)} 至 {fmt(result?.end)}。计算版本：
        <span className="rotation-version">{result?.version || "—"}</span>。
      </p>
      <p>
        历史模拟不代表未来收益；模型持仓不是实际账户仓位、实时交易信号或成交记录。
      </p>
    </div>
  );
}

export function Rotation() {
  const [view, setView] = useState<View | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [range, setRange] = useState<[number, number]>([0, 0]);
  const [period, setPeriod] = useState("12");
  const [selected, setSelected] = useState<string[]>([]);
  const [hover, setHover] = useState<number | null>(null);
  const initialized = useRef("");
  const request = useRef<AbortController | null>(null);
  async function load() {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    try {
      const res = await fetch("/api/rotation/backtest", {
        signal: controller.signal,
      });
      if (!res.ok)
        throw new Error(
          res.status === 401 || res.status === 403
            ? "登录已过期或无访问权限，请重新登录后刷新。"
            : "暂时无法读取回测，请稍后重试。",
        );
      const next: View = await res.json();
      if (
        !next ||
        typeof next.status !== "string" ||
        (next.result &&
          (!Array.isArray(next.result.days) ||
            !Array.isArray(next.result.codes) ||
            !Array.isArray(next.result.names)))
      )
        throw new Error("回测结果格式不可用，请稍后重试。");
      setView(next);
      setError("");
      const key = next.result
        ? `${next.result.start}:${next.result.end}:${next.result.days.length}`
        : "";
      if (next.result?.days.length && key !== initialized.current) {
        const days = next.result.days;
        const end = new Date(`${fmt(days.at(-1)!.date)}T00:00:00Z`);
        end.setUTCFullYear(end.getUTCFullYear() - 1);
        const start = end.toISOString().slice(0, 10).replaceAll("-", "");
        setRange([
          Math.max(
            0,
            days.findIndex((d) => d.date >= start),
          ),
          days.length - 1,
        ]);
        setHover(null);
        setPeriod("12");
      }
      initialized.current = key;
    } catch (e) {
      if (!controller.signal.aborted)
        setError(e instanceof Error ? e.message : "暂时无法读取回测");
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 60000);
    return () => {
      clearInterval(timer);
      request.current?.abort();
    };
  }, []);
  const result = view?.result;
  const days = result?.days ?? [];
  const lo = Math.min(range[0], Math.max(0, days.length - 1)),
    hi = Math.min(range[1], Math.max(0, days.length - 1));
  const visible = useMemo(() => days.slice(lo, hi + 1), [days, lo, hi]);
  const series = useMemo(
    () => rangeSeries(visible, lo, result?.codes.length ?? 0),
    [visible, lo, result?.codes.length],
  );
  const activeIndex = Math.max(
    0,
    Math.min(hover ?? visible.length - 1, visible.length - 1),
  );
  const active = visible[activeIndex];
  const latest = days.at(-1);
  const missing = visible.some(
    (d) =>
      !finite(d.nav) ||
      d.nav <= 0 ||
      !finite(d.weight) ||
      !finite(d.cashWeight) ||
      d.holding == null ||
      result?.codes.some((_, j) => !finite(d.benchmarks?.[j])),
  );
  function change(a: number, b: number) {
    setHover(null);
    setPeriod("");
    setRange([
      Math.max(0, Math.min(a, b)),
      Math.min(days.length - 1, Math.max(a, b)),
    ]);
  }
  function preset(value: string) {
    if (!value || !days.length) return;
    let start = 0;
    if (value !== "all") {
      const end = new Date(`${fmt(days.at(-1)!.date)}T00:00:00Z`);
      end.setUTCMonth(end.getUTCMonth() - Number(value));
      start = Math.max(
        0,
        days.findIndex(
          (d) => d.date >= end.toISOString().slice(0, 10).replaceAll("-", ""),
        ),
      );
    }
    change(start, days.length - 1);
    setPeriod(value);
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
  const options = useMemo(() => {
    const common: EChartsCoreOption = {
      animation: false,
      textStyle: { fontFamily: theme.typography.numbers, color: theme.ink },
      grid: { left: 52, right: 16, top: 24, bottom: 30 },
      axisPointer: {
        show: true,
        triggerTooltip: false,
        triggerOn: "none",
        label: { show: false },
      },
      xAxis: {
        type: "category",
        boundaryGap: false,
        data: visible.map((d) => fmt(d.date)),
        axisLine: { lineStyle: { color: theme.rule } },
        axisTick: { show: false },
        axisLabel: { color: theme.muted, hideOverlap: true, fontSize: 11 },
        axisPointer: {
          show: true,
          triggerTooltip: false,
          lineStyle: { color: theme.muted, width: 1, type: "dashed" },
        },
      },
      yAxis: {
        type: "value",
        axisLabel: {
          color: theme.muted,
          fontSize: 11,
          formatter: (n: number) => `${(n * 100).toFixed(0)}%`,
        },
        splitLine: { lineStyle: { color: theme.rule, width: 0.5 } },
      },
    };
    const line = (
      name: string,
      data: (number | null)[],
      c: string,
      primary = false,
      type: string = "solid",
    ) => ({
      name,
      type: "line",
      data,
      showSymbol: false,
      connectNulls: false,
      silent: true,
      lineStyle: {
        color: c,
        width: primary ? theme.lineWidth.primary : theme.lineWidth.comparison,
        type,
      },
      itemStyle: { color: c },
      emphasis: { disabled: true },
    });
    return {
      returns: {
        ...common,
        series: [
          line("轮动策略", series.returns, theme.series[0], true),
          ...(result?.codes.flatMap((code, j) =>
            selected.includes(code)
              ? [
                  line(
                    result.names[j],
                    series.comparisons[j],
                    color(code),
                    false,
                    lineType[universe.findIndex(([c]) => c === code)] ??
                      "solid",
                  ),
                ]
              : [],
          ) ?? []),
        ],
      },
      drawdown: {
        ...common,
        series: [
          {
            ...line("区间回撤", series.dd, theme.series[0], true),
            areaStyle: { color: theme.series[0], opacity: 0.08 },
          },
        ],
      },
      holdings: {
        ...common,
        yAxis: { ...(common.yAxis as object), min: 0, max: 1, interval: 0.5 },
        series: [
          ...(result?.codes.map((code, j) => ({
            name: result.names[j],
            type: "bar",
            stack: "weight",
            barCategoryGap: "0%",
            barGap: "0%",
            silent: true,
            itemStyle: { color: color(code) },
            emphasis: { disabled: true },
            data: visible.map((d) =>
              d.holding == null || !finite(d.weight)
                ? null
                : d.holding === code
                  ? d.weight
                  : 0,
            ),
          })) ?? []),
          {
            name: "现金",
            type: "bar",
            stack: "weight",
            barCategoryGap: "0%",
            silent: true,
            itemStyle: { color: theme.surface.edge },
            data: visible.map((d) =>
              finite(d.cashWeight) ? d.cashWeight : null,
            ),
          },
        ],
      },
    };
  }, [visible, series, result, selected]);
  const chartProps = {
    count: visible.length,
    active: activeIndex,
    onInspect: setHover,
    onRange: (a: number, b: number) => change(lo + a, lo + b),
  };
  return (
    <ResearchTheme className="rotation-page">
      <a href="#main" className="rotation-skip">
        跳转到主要内容
      </a>
      <div className="rotation-shell">
        <header className="research-nav">
          <a href="/" className="research-brand" aria-label="Candela 首页">
            Candela
          </a>
          <nav aria-label="主要导航">
            <a href="/market">市场状态</a>
            <a href="/strategies/four-etf-rotation" aria-current="page">
              四标的轮动
            </a>
          </nav>
        </header>
        <main id="main" tabIndex={-1}>
          <div className="research-intro">
            <div className="report-cover">
              <p className="research-eyebrow">
                STRATEGY RESEARCH <span>每日数据 · 历史回测</span>
              </p>
              <h1>四标的轮动</h1>
              <p className="research-intro-copy">
                查看红利、黄金、创业板与纳指的每日计算依据，核对数据，并观察历史模型表现。
              </p>
              <div className="report-byline">
                <span>Candela 研究</span>
                <span>四类资产 · 单标的与现金配置</span>
              </div>
              <Dialog>
                <DialogTrigger asChild>
                  <Button variant="outline">阅读策略与回测口径</Button>
                </DialogTrigger>
                <DialogContent className="rotation-method-dialog">
                  <DialogHeader>
                    <DialogTitle>策略与回测口径</DialogTitle>
                    <DialogDescription>
                      当前发布版本、交易成本与历史模拟限制。
                    </DialogDescription>
                  </DialogHeader>
                  <Method result={result} />
                </DialogContent>
              </Dialog>
            </div>
            <nav className="report-contents" aria-label="报告目录">
              <p>本页内容</p>
              <a href="#daily-data">
                <span>01</span>每日数据
              </a>
              <a href="#performance">
                <span>02</span>收益与风险
              </a>
              <a href="#holdings">
                <span>03</span>历史模型持仓
              </a>
              <a href="#method">
                <span>04</span>规则与口径
              </a>
            </nav>
          </div>
          <RotationDaily />
          <section
            id="data-status"
            className="rotation-status"
            aria-labelledby="status-title"
          >
            <div className="rotation-section-heading">
              <h2 id="status-title">数据与模型状态</h2>
              <div className="flex items-center gap-3">
                <Badge variant="outline">
                  {error
                    ? "读取失败"
                    : (statuses[view?.status ?? ""] ??
                      (view ? "状态未知" : "正在读取"))}
                </Badge>
                <Button
                  variant="outline"
                  size="icon"
                  aria-label="刷新回测"
                  disabled={busy}
                  onClick={() => void load()}
                >
                  <RefreshCw />
                </Button>
              </div>
            </div>
            {error && (
              <Alert variant="destructive">
                <AlertTitle>暂时无法更新页面数据</AlertTitle>
                <AlertDescription>
                  {error}
                  {result &&
                    " 当前保留上次读取的历史结果，不能据此判断已更新。"}
                </AlertDescription>
              </Alert>
            )}
            {view && (
              <p role="status" className="rotation-caption">
                {view.message ||
                  (view.status === "ready"
                    ? "当前展示已发布的历史回测结果。"
                    : "更新尚未完成，请以数据截至日为准。")}
                {result && view.status !== "ready"
                  ? " 下方为保留的历史结果。"
                  : ""}
              </p>
            )}
            {!view && !error && (
              <div role="status" className="rotation-loading">
                <span>正在读取回测…</span>
                <Skeleton className="h-8 w-2/3" />
                <Skeleton className="h-8 w-full" />
              </div>
            )}
            {view && (
              <>
                <dl className="rotation-status-grid">
                  <div>
                    <dt>数据截至交易日</dt>
                    <dd className="research-number">{fmt(result?.end)}</dd>
                  </div>
                  <div>
                    <dt>历史模型持仓</dt>
                    <dd>{result ? holdingName(latest, result) : "—"}</dd>
                  </div>
                  <div>
                    <dt>ETF 权重 / 现金</dt>
                    <dd className="research-number">
                      {pct(latest?.weight)} / {pct(latest?.cashWeight)}
                    </dd>
                  </div>
                </dl>
                <p className="rotation-caption">
                  发布更新时间：{publishedAt(view.updatedAt)}
                </p>
                {result && (
                  <p className="rotation-caption">
                    较前一回测交易日：{holdingChange(days, result)}
                    。权重变化可能来自价格波动，不等同于调仓成交。
                  </p>
                )}
              </>
            )}
          </section>
          {view && !days.length && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>
                  <h2>暂无完整回测结果</h2>
                </EmptyTitle>
                <EmptyDescription>
                  所需行情准备完成后，这里会显示收益与持仓历史。缺失数据不表示空仓或零收益。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          {result && visible.length > 0 && (
            <>
              <section id="performance" aria-labelledby="performance-title">
                <div className="report-section-intro">
                  <p className="research-eyebrow">02 / PERFORMANCE</p>
                  <h2 id="performance-title">收益与风险，一起观察</h2>
                  <p>
                    以下指标随观察区间联动。策略扣除交易成本，ETF
                    对照未扣成本；回撤反映所选区间内从净值高点回落的幅度。
                  </p>
                </div>
                <div className="rotation-toolbar">
                  <span>观察区间</span>
                  <ToggleGroup
                    type="single"
                    value={period}
                    onValueChange={preset}
                    aria-label="观察区间"
                  >
                    {[
                      ["3", "近三月"],
                      ["12", "近一年"],
                      ["36", "近三年"],
                      ["all", "全部"],
                    ].map(([v, n]) => (
                      <ToggleGroupItem key={v} value={v}>
                        {n}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </div>
                <dl className="rotation-metrics">
                  <div>
                    <dt>区间收益 · 扣费后</dt>
                    <dd data-testid="rotation-gain">{pct(series.gain)}</dd>
                  </div>
                  <div>
                    <dt>区间最大回撤</dt>
                    <dd data-testid="rotation-maxdd">{pct(series.maxdd)}</dd>
                  </div>
                  <div>
                    <dt>观察区间 · {visible.length} 个交易日</dt>
                    <dd
                      className="rotation-period"
                      data-testid="rotation-range"
                    >
                      {fmt(visible[0].date)}
                      <br />
                      {fmt(visible.at(-1)!.date)}
                    </dd>
                  </div>
                </dl>
                {missing && (
                  <Alert>
                    <AlertTitle>部分历史字段缺失</AlertTitle>
                    <AlertDescription>
                      缺失值以 —
                      和图表断点展示，不补零、不推断持仓。净值缺口后的区间回撤不可确定。
                    </AlertDescription>
                  </Alert>
                )}
                {visible.length === 1 && (
                  <p className="rotation-caption">
                    当前仅一个交易日，区间收益与最大回撤暂不统计。
                  </p>
                )}
                <div className="rotation-figure-grid">
                  <div className="rotation-figures">
                    <Card>
                      <CardHeader>
                        <CardDescription>
                          图 01 · 以观察区间起点为基准
                        </CardDescription>
                        <CardTitle>累计收益与 ETF 对照</CardTitle>
                        <div className="rotation-legend">
                          <span>
                            <i style={{ borderColor: theme.series[0] }} />
                            轮动策略 · 扣费后
                          </span>
                          {result.codes.map((code, j) => (
                            <label key={code}>
                              <Checkbox
                                checked={selected.includes(code)}
                                onCheckedChange={(checked) =>
                                  setSelected((prev) =>
                                    checked === true
                                      ? [...prev, code]
                                      : prev.filter((c) => c !== code),
                                  )
                                }
                              />
                              <i
                                style={{
                                  borderColor: color(code),
                                  borderTopStyle:
                                    lineType[
                                      universe.findIndex(([c]) => c === code)
                                    ],
                                }}
                              />
                              {result.names[j] || code}
                            </label>
                          ))}
                        </div>
                      </CardHeader>
                      <CardContent>
                        <RotationChart
                          {...chartProps}
                          option={options.returns}
                          label="策略与 ETF 累计收益图"
                        />
                      </CardContent>
                      <CardFooter className="flex flex-col items-start gap-3">
                        {active && (
                          <p className="rotation-chart-readout">
                            {fmt(active.date)} · 净值 {number(active.nav)} ·
                            区间收益 {pct(series.returns[activeIndex])} · 回撤{" "}
                            {pct(series.dd[activeIndex])}
                            {result.codes
                              .filter((c) => selected.includes(c))
                              .map((code) => {
                                const j = result.codes.indexOf(code);
                                return (
                                  <span key={code}>
                                    {" "}
                                    · {result.names[j]}{" "}
                                    {pct(series.comparisons[j]?.[activeIndex])}
                                  </span>
                                );
                              })}
                          </p>
                        )}
                        <p>
                          ETF
                          为未扣费买入持有对照。拖动曲线区域可缩放；手机可使用下方日期和范围控件。
                        </p>
                      </CardFooter>
                    </Card>
                    <div className="rotation-controls">
                      <div className="flex flex-wrap gap-2">
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="向前平移"
                          disabled={lo === 0}
                          onClick={() => pan(-1)}
                        >
                          <ArrowLeft />
                        </Button>
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="放大区间"
                          disabled={hi - lo < 2}
                          onClick={() => zoom(0.5)}
                        >
                          <ZoomIn />
                        </Button>
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="缩小区间"
                          disabled={lo === 0 && hi === days.length - 1}
                          onClick={() => zoom(2)}
                        >
                          <ZoomOut />
                        </Button>
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="向后平移"
                          disabled={hi === days.length - 1}
                          onClick={() => pan(1)}
                        >
                          <ArrowRight />
                        </Button>
                      </div>
                      <FieldGroup className="rotation-date-fields">
                        <Field>
                          <FieldLabel htmlFor="rotation-start">
                            开始日期
                          </FieldLabel>
                          <Input
                            id="rotation-start"
                            type="date"
                            value={fmt(days[lo].date)}
                            min={fmt(result.start)}
                            max={fmt(days[hi].date)}
                            onChange={(e) => {
                              const d = e.target.value.replaceAll("-", ""),
                                i = days.findIndex((v) => v.date >= d);
                              if (d && i >= 0) change(Math.min(i, hi), hi);
                            }}
                          />
                        </Field>
                        <Field>
                          <FieldLabel htmlFor="rotation-end">
                            结束日期
                          </FieldLabel>
                          <Input
                            id="rotation-end"
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
                        </Field>
                      </FieldGroup>
                      <FieldGroup className="rotation-sliders">
                        <Field>
                          <FieldLabel htmlFor="rotation-range-start">
                            区间起点
                          </FieldLabel>
                          <input
                            id="rotation-range-start"
                            type="range"
                            min="0"
                            max={days.length - 1}
                            value={lo}
                            aria-valuetext={fmt(days[lo].date)}
                            onChange={(e) =>
                              change(Math.min(Number(e.target.value), hi), hi)
                            }
                          />
                        </Field>
                        <Field>
                          <FieldLabel htmlFor="rotation-range-end">
                            区间终点
                          </FieldLabel>
                          <input
                            id="rotation-range-end"
                            type="range"
                            min="0"
                            max={days.length - 1}
                            value={hi}
                            aria-valuetext={fmt(days[hi].date)}
                            onChange={(e) =>
                              change(lo, Math.max(Number(e.target.value), lo))
                            }
                          />
                        </Field>
                      </FieldGroup>
                    </div>
                    <Card>
                      <CardHeader>
                        <CardDescription>
                          图 02 · 与收益图共用观察区间
                        </CardDescription>
                        <CardTitle>区间回撤</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <RotationChart
                          {...chartProps}
                          compact
                          option={options.drawdown}
                          label="策略区间回撤图"
                        />
                      </CardContent>
                      <CardFooter>
                        相对于所选区间内净值峰值；不会沿用区间开始前的峰值。
                      </CardFooter>
                    </Card>
                    <section id="holdings" aria-label="历史模型持仓">
                      <Card>
                        <CardHeader>
                          <CardDescription>
                            图 03 · 每个回测交易日收盘后
                          </CardDescription>
                          <CardTitle>历史模型持仓</CardTitle>
                          <div className="rotation-legend">
                            {result.codes.map((code, j) => (
                              <span key={code}>
                                <i style={{ borderColor: color(code) }} />
                                {result.names[j]}
                              </span>
                            ))}
                            <span>
                              <i style={{ borderColor: theme.surface.edge }} />
                              现金
                            </span>
                          </div>
                        </CardHeader>
                        <CardContent>
                          <RotationChart
                            {...chartProps}
                            compact
                            option={options.holdings}
                            label="ETF 与现金历史仓位图"
                          />
                        </CardContent>
                        <CardFooter>
                          历史模型结果，不是实际账户仓位或成交记录。缺失字段保留为空白。
                        </CardFooter>
                      </Card>
                    </section>
                  </div>
                  <aside className="rotation-aside">
                    <h3>如何读这些图</h3>
                    <p>
                      三张图共用观察区间与日期光标。移动指针、点击图表，或用下方日期滑块逐日查看。
                    </p>
                    <p>净值按完整历史连续计算；放大区间不会重新运行策略。</p>
                    <h3>四标的范围</h3>
                    {universe.map(([code, name]) => (
                      <p key={code}>
                        {name}
                        <br />
                        <span className="research-number">{code}</span>
                      </p>
                    ))}
                    <h3>数据边界</h3>
                    <p>
                      每日指标以页面上方已发布数据为准。模型持仓变化不能用来推断实际成交或变化原因。
                    </p>
                    <p>历史模型结果与实际账户分别理解，未知字段保持为空。</p>
                  </aside>
                </div>
              </section>
              {active && (
                <section
                  className="rotation-detail"
                  data-testid="rotation-detail"
                  aria-labelledby="detail-title"
                >
                  <div className="rotation-section-heading">
                    <h2 id="detail-title">逐日查看</h2>
                    <span className="research-number">{fmt(active.date)}</span>
                  </div>
                  <Field>
                    <FieldLabel htmlFor="rotation-inspect">
                      查看交易日（方向键逐日移动）
                    </FieldLabel>
                    <input
                      id="rotation-inspect"
                      type="range"
                      min="0"
                      max={visible.length - 1}
                      value={activeIndex}
                      aria-valuetext={fmt(active.date)}
                      onChange={(e) => setHover(Number(e.target.value))}
                    />
                  </Field>
                  <dl className="rotation-detail-values" aria-live="polite">
                    <div>
                      <dt>历史净值</dt>
                      <dd>{number(active.nav)}</dd>
                    </div>
                    <div>
                      <dt>区间收益</dt>
                      <dd>{pct(series.returns[activeIndex])}</dd>
                    </div>
                    <div>
                      <dt>回撤</dt>
                      <dd>{pct(series.dd[activeIndex])}</dd>
                    </div>
                    <div>
                      <dt>{holdingName(active, result)}</dt>
                      <dd>{pct(active.weight)}</dd>
                    </div>
                    <div>
                      <dt>现金</dt>
                      <dd>现金 {pct(active.cashWeight)}</dd>
                    </div>
                    {result.codes
                      .filter((c) => selected.includes(c))
                      .map((code) => {
                        const j = result.codes.indexOf(code);
                        return (
                          <div key={code}>
                            <dt>{result.names[j]} · 区间收益</dt>
                            <dd>{pct(series.comparisons[j]?.[activeIndex])}</dd>
                          </div>
                        );
                      })}
                  </dl>
                  {active.suspended?.length ? (
                    <p className="rotation-caption">
                      当日已确认停牌：{active.suspended.join("、")}
                      ，以最近有效收盘估值，不参与交易。
                    </p>
                  ) : null}
                </section>
              )}
            </>
          )}
          <section id="method" className="rotation-method-section">
            <p className="research-eyebrow">04 / METHODOLOGY</p>
            <details>
              <summary>策略与回测口径</summary>
              <Method result={result} />
            </details>
            <details>
              <summary>尚未提供的数据</summary>
              <div className="rotation-method">
                <p>
                  14:45 固定参考尚未接入；每日收盘结果独立发布，缺失字段及原因见每日数据区。
                </p>
                <p>
                  本页不推测指标变化原因，不提供实际账户数据或定投模拟。
                </p>
              </div>
            </details>
          </section>
        </main>
        <footer className="rotation-footer">
          <span>Candela · 让数据照亮决策</span>
          <span>历史研究 · 非实际账户业绩</span>
        </footer>
      </div>
    </ResearchTheme>
  );
}
