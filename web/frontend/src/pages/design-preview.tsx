import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { ArrowRight, ArrowUpRight, ChevronDown } from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
} from "@/components/ui/table";
import { Input } from "@/components/ui/input";
import { Field, FieldLabel } from "@/components/ui/field";
import { ComponentStudy } from "@/components/component-study";
import { Separator } from "@/components/ui/separator";
import { DesignPreviewChart } from "@/components/design-preview-chart";
import {
  assets,
  metrics,
  monthly,
  observations,
  percent,
} from "@/lib/design-preview-data";
import {
  researchTheme,
  previewThemeStyle,
  heatmapLabelColor,
} from "@/lib/design-preview-themes";
import type { EChartsCoreOption } from "echarts/core";
import "@fontsource/source-serif-4/latin-300.css";
import "@fontsource/source-serif-4/latin-400.css";
import "@fontsource/source-serif-4/latin-500.css";
import "@fontsource/alegreya/latin-400.css";
import "@fontsource/alegreya/latin-500.css";
import "@fontsource-variable/noto-sans-sc";
import "@fontsource-variable/noto-serif-sc";

const themeStyle = previewThemeStyle();
const numberFont = researchTheme.typography.numbers;
const { paper, edge: paperEdge } = researchTheme.surface;
const retiredDesignParams = [
  "theme",
  "paper",
  "density",
  "chinese",
  "english",
  "numbers",
  "reading",
];
const retiredDesignAnchors = [
  "#paper-surfaces",
  "#layout-density",
  "#chinese-fonts",
  "#english-fonts",
  "#number-fonts",
];

const lineStyles = ["solid", "solid", "dashed", "dotted", "dashed"] as const;
const lineWidthFor = (id: string) => (id === "strategy" ? 1.6 : 1.2);
const font = '"Noto Sans SC Variable", "PingFang SC", sans-serif';
const ranges = [
  { value: "6", label: "近半年" },
  { value: "12", label: "近一年" },
  { value: "36", label: "全部" },
];

function ResearchTitle({
  number,
  title,
  subtitle,
}: {
  number: string;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="research-section-heading">
      <span className="section-number">{number}</span>
      <div>
        <CardTitle>
          <h2>{title}</h2>
        </CardTitle>
        <CardDescription>{subtitle}</CardDescription>
      </div>
    </div>
  );
}

export function DesignPreview() {
  // Historical comparison URLs always resolve to the finalized design.
  useEffect(() => {
    const normalizeDesignUrl = () => {
      const url = new URL(window.location.href);
      for (const key of retiredDesignParams) url.searchParams.delete(key);
      const retiredAnchor = retiredDesignAnchors.includes(url.hash);
      if (retiredAnchor) url.hash = "overview";
      if (url.href !== window.location.href) {
        window.history.replaceState(window.history.state, "", url);
      }
      if (retiredAnchor) document.getElementById("overview")?.scrollIntoView();
    };
    normalizeDesignUrl();
    window.addEventListener("popstate", normalizeDesignUrl);
    window.addEventListener("hashchange", normalizeDesignUrl);
    return () => {
      window.removeEventListener("popstate", normalizeDesignUrl);
      window.removeEventListener("hashchange", normalizeDesignUrl);
    };
  }, []);
  const colors = researchTheme.series;
  const heatmapColors = researchTheme.heatmap;
  const { ink, muted, rule } = researchTheme;
  const [range, setRange] = useState("12");
  const [visible, setVisible] = useState<string[]>([
    "strategy",
    "dividend",
    "gold",
  ]);
  const [mode, setMode] = useState("return");
  const [query, setQuery] = useState("");
  const [descending, setDescending] = useState(true);
  const points = useMemo(() => observations(Number(range)), [range]);
  const stats = useMemo(
    () => metrics(points, "strategy", Number(range)),
    [points, range],
  );
  const selectedMonths = monthly.slice(-Number(range));
  const upMonths = selectedMonths.filter((m) => m.returns.strategy > 0).length;
  useEffect(() => {
    const title = document.title;
    document.title = "Candela · 策略研究样板";
    return () => {
      document.title = title;
    };
  }, []);

  const lineOption = useMemo<EChartsCoreOption>(
    () => ({
      animation: false,
      textStyle: { fontFamily: font, color: muted },
      grid: { left: 48, right: 18, top: 18, bottom: 32 },
      tooltip: {
        trigger: "axis",
        confine: true,
        backgroundColor: paper,
        borderColor: paperEdge,
        textStyle: { color: ink, fontFamily: numberFont, fontSize: 13 },
        valueFormatter: (v: number) => v.toFixed(2) + "%",
      },
      xAxis: {
        type: "category",
        data: points.map((p) => p.date),
        boundaryGap: false,
        axisLine: { lineStyle: { color: rule } },
        axisTick: { show: false },
        axisLabel: {
          fontFamily: numberFont,
          color: muted,
          fontSize: 12,
          margin: 14,
          hideOverlap: true,
          interval: Math.max(1, Math.floor(points.length / 6)),
          formatter: (v: string) => v.slice(0, 7).replace("-", "."),
        },
      },
      yAxis: {
        type: "value",
        splitNumber: 4,
        axisLabel: {
          fontFamily: numberFont,
          color: muted,
          fontSize: 12,
          formatter: "{value}%",
          margin: 12,
        },
        splitLine: { lineStyle: { color: rule, type: "dashed" } },
      },
      series: assets
        .filter((a) => visible.includes(a.id))
        .map((a) => {
          let peak = 100;
          const values = points.map((p) => {
            peak = Math.max(peak, p.values[a.id]);
            return +(
              mode === "return"
                ? p.values[a.id] - 100
                : (p.values[a.id] / peak - 1) * 100
            ).toFixed(3);
          });
          return {
            name: a.name,
            type: "line",
            data: values,
            symbol: "circle",
            showSymbol: false,
            smooth: false,
            color: colors[assets.indexOf(a)],
            lineStyle: {
              width: lineWidthFor(a.id),
              type: lineStyles[assets.indexOf(a)],
            },
            areaStyle:
              a.id === "strategy"
                ? { color: colors[0], opacity: 0.045 }
                : undefined,
            emphasis: {
              focus: "series",
              lineStyle: { width: lineWidthFor(a.id) },
            },
          };
        }),
    }),
    [
      points,
      visible,
      mode,
      colors,
      ink,
      muted,
      rule,
      paper,
      paperEdge,
      numberFont,
    ],
  );

  const heatmapOption = useMemo<EChartsCoreOption>(
    () => ({
      animation: false,
      textStyle: { fontFamily: numberFont },
      grid: { left: 40, right: 6, top: 30, bottom: 10 },
      tooltip: {
        position: "top",
        confine: true,
        backgroundColor: paper,
        borderColor: paperEdge,
        formatter: (p: { value: number[] }) =>
          2023 +
          p.value[1] +
          " 年 " +
          (p.value[0] + 1) +
          " 月<br/>模拟月收益：" +
          percent(p.value[2]),
      },
      xAxis: {
        type: "category",
        position: "top",
        data: Array.from({ length: 12 }, (_, i) => String(i + 1)),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: muted, fontSize: 12, interval: 0 },
      },
      yAxis: {
        type: "category",
        inverse: true,
        data: ["2023", "2024", "2025"],
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: muted, fontSize: 12 },
      },
      visualMap: {
        show: false,
        min: -6,
        max: 6,
        inRange: {
          color: heatmapColors,
        },
      },
      series: [
        {
          type: "heatmap",
          data: monthly.map((m, i) => ({
            value: [i % 12, Math.floor(i / 12), m.returns.strategy],
            label: { color: heatmapLabelColor(m.returns.strategy) },
          })),
          label: {
            show: true,
            fontSize: 12,
            color: ink,
            formatter: (p: { value: number[] }) =>
              (p.value[2] > 0 ? "+" : "") + p.value[2].toFixed(1),
          },
          itemStyle: {
            borderColor: paper,
            borderWidth: 3,
            borderRadius: 1,
          },
          emphasis: { itemStyle: { borderColor: ink, borderWidth: 2 } },
        },
      ],
    }),
    [heatmapColors, ink, muted, rule, paper, paperEdge, numberFont],
  );
  const comparisons = assets
    .map((a) => ({ ...a, ...metrics(points, a.id, Number(range)) }))
    .filter((a) => a.name.includes(query.trim()))
    .sort((a, b) => (descending ? b.total - a.total : a.total - b.total));

  return (
    <div
      className="design-preview-theme design-preview"
      style={themeStyle}
      data-theme="research"
      data-design-version={researchTheme.version}
      data-paper={researchTheme.surface.id}
      data-density={researchTheme.density}
      data-number-font="source-serif"
      data-chinese-font="serif-light"
      data-english-font="source-serif"
    >
      <a className="research-skip" href="#research-main">
        跳转到主要内容
      </a>
      <div className="research-shell">
        <header className="research-nav">
          <a
            href="#overview"
            className="research-brand"
            aria-label="Candela 研究总览"
          >
            candela
          </a>
          <nav aria-label="样板页导航">
            <a href="#overview">研究总览</a>
            <a href="#performance">收益分析</a>
            <a href="#comparison">标的对比</a>
            <a href="#components">研究工具</a>
          </nav>
          <span className="research-edition">
            研究手记 <span>No. 001</span>
          </span>
        </header>

        <main id="research-main">
          <section className="research-intro" id="overview">
            <div className="report-cover">
              <p className="research-eyebrow">
                CANDELA RESEARCH <span>策略研究 / 001</span>
              </p>
              <h1>
                四标的轮动：
                <br />
                收益与回撤的观察。
              </h1>
              <p className="report-english-title" lang="en">
                Understanding returns.
                <br />
                Living with risk.
              </p>
              <p className="research-intro-copy">
                一项策略的表现，不止于终点的收益。我们将收益{" "}
                <span className="report-english" lang="en">
                  return
                </span>
                、月度变化与回撤{" "}
                <span className="report-english" lang="en">
                  drawdown
                </span>{" "}
                放在同一份观察中，理解数字如何随时间展开。
              </p>
              <p className="report-english-copy" lang="en">
                A strategy is more than its final return. We study its path,
                monthly changes, and drawdowns together, tracing how the
                evidence unfolds over time.
              </p>
              <div className="report-byline">
                <span>Candela 研究手记</span>
                <span>2023 — 2025</span>
                <span>模拟数据 · 设计样板</span>
              </div>
              <Dialog>
                <DialogTrigger asChild>
                  <Button variant="outline">
                    数据与方法 <ArrowUpRight data-icon="inline-end" />
                  </Button>
                </DialogTrigger>
                <DialogContent
                  className="design-preview-theme research-dialog"
                  style={themeStyle}
                >
                  <DialogHeader>
                    <DialogTitle>关于这份研究样板</DialogTitle>
                    <DialogDescription>
                      采用 Candela 定稿的研究报告式视觉与交互规范。
                    </DialogDescription>
                  </DialogHeader>
                  <p>
                    所有曲线和指标来自同一份固定模拟数据，时间为 2023–2025
                    年，不是实际行情、真实回测或投资建议。
                  </p>
                  <p>
                    周期切换会将选中区间的起点归一为
                    100，再计算累计收益、年化收益与采样点最大回撤。热力图始终展示完整三年的月度模拟收益。
                  </p>
                  <p>正式产品仍应使用已确认的策略规则、交易成本和数据口径。</p>
                </DialogContent>
              </Dialog>
            </div>
            <aside className="report-contents" aria-label="本篇内容">
              <p>本篇内容</p>
              <a href="#performance">
                <span>01</span> 收益与路径
              </a>
              <a href="#monthly">
                <span>02</span> 月度分布
              </a>
              <a href="#comparison">
                <span>03</span> 同期比较
              </a>
              <a href="#components">
                <span>04</span> 研究工具
              </a>
              <p className="report-contents-note">
                以时间为线索，
                <br />
                让每一个结论都有据可查。
              </p>
            </aside>
          </section>

          <section className="report-section-intro" id="performance">
            <p className="research-eyebrow">01 / 收益与路径</p>
            <h2>同一个终点，可以有不同的过程。</h2>
            <p>
              累计收益描述结果，回撤揭示途中经历的波动。以下图版将轮动策略与四个标的放在相同的起点；切换观察区间，可以看到路径如何改变。
            </p>
          </section>
          <div className="research-toolbar">
            <div className="strategy-name">
              <span>区间摘要</span>
            </div>
            <span className="research-date">
              {selectedMonths[0].date.replace("-", ".")} — 2025.12{" "}
              <span>· 月度模拟序列</span>
            </span>
          </div>

          <section
            className="metric-grid"
            aria-label="所选周期策略指标"
            aria-live="polite"
          >
            <Card data-emphasis="primary">
              <CardHeader>
                <CardDescription>
                  区间累计收益 <span>CUMULATIVE RETURN</span>
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="metric-number">
                  {percent(stats.total).slice(0, -1)}
                  <span>%</span>
                </div>
              </CardContent>
              <CardFooter>所选 {range} 个月的复合变化。</CardFooter>
            </Card>
            <Card>
              <CardHeader>
                <CardDescription>
                  区间年化收益 <span>ANNUALIZED RETURN</span>
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="metric-number">
                  {percent(stats.annual).slice(0, -1)}
                  <span>%</span>
                </div>
              </CardContent>
              <CardFooter>按所选月份折算，仅描述模拟结果。</CardFooter>
            </Card>
            <Card>
              <CardHeader>
                <CardDescription>
                  区间最大回撤 <span>MAXIMUM DRAWDOWN</span>
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="metric-number">
                  {percent(stats.drawdown).slice(0, -1)}
                  <span>%</span>
                </div>
              </CardContent>
              <CardFooter>相对区间内前期高点的最大降幅。</CardFooter>
            </Card>
          </section>

          <section className="performance-grid" aria-label="收益与回撤图版">
            <Card className="performance-card">
              <CardHeader>
                <div className="chart-heading-row">
                  <ResearchTitle
                    number="图 1"
                    title={mode === "return" ? "收益的轨迹" : "波动的另一面"}
                    subtitle={
                      mode === "return"
                        ? "累计收益 · 区间起点归零 · 单位 %"
                        : "相对区间内历史高点的回撤 · 单位 %"
                    }
                  />
                  <ToggleGroup
                    type="single"
                    value={range}
                    onValueChange={(v) => {
                      if (v) setRange(v);
                    }}
                    aria-label="观察周期"
                  >
                    {ranges.map((r) => (
                      <ToggleGroupItem key={r.value} value={r.value}>
                        {r.label}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </div>
              </CardHeader>
              <CardContent>
                <div className="chart-subtoolbar">
                  <ToggleGroup
                    type="single"
                    value={mode}
                    onValueChange={(v) => {
                      if (v) setMode(v);
                    }}
                    aria-label="图表指标"
                    data-appearance="underline"
                  >
                    <ToggleGroupItem value="return">累计收益</ToggleGroupItem>
                    <ToggleGroupItem value="drawdown">回撤</ToggleGroupItem>
                  </ToggleGroup>
                  <span className="chart-help">移动指针，查看同期表现</span>
                </div>
                <DesignPreviewChart
                  option={lineOption}
                  label={
                    range +
                    "个月" +
                    (mode === "return" ? "累计收益" : "回撤") +
                    "曲线。精确月末数据可通过下方查看数据按钮读取。"
                  }
                />
                <ToggleGroup
                  type="multiple"
                  value={visible}
                  onValueChange={(v) => {
                    if (v.length) setVisible(v);
                  }}
                  aria-label="显示比较序列"
                  className="series-legend"
                >
                  {assets.map((a, i) => (
                    <ToggleGroupItem
                      key={a.id}
                      value={a.id}
                      style={{ "--series-color": colors[i] } as CSSProperties}
                    >
                      <span
                        className="series-line"
                        style={{
                          borderTopStyle: lineStyles[i],
                          borderTopWidth: lineWidthFor(a.id),
                        }}
                      />
                      {a.short}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </CardContent>
              <CardFooter>
                <span>固定模拟数据 / 截止 2025.12.31</span>
                <Dialog>
                  <DialogTrigger asChild>
                    <Button variant="ghost" size="sm">
                      查看数据 <ArrowUpRight data-icon="inline-end" />
                    </Button>
                  </DialogTrigger>
                  <DialogContent
                    className="design-preview-theme research-dialog research-data-dialog"
                    style={themeStyle}
                  >
                    <DialogHeader>
                      <DialogTitle>模拟月末数据</DialogTitle>
                      <DialogDescription>
                        当前 {range} 个月，区间起点归一为
                        100。以下为全部比较序列的累计收益。
                      </DialogDescription>
                    </DialogHeader>
                    <div className="dialog-table-scroll">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>日期</TableHead>
                            {assets.map((a) => (
                              <TableHead key={a.id}>{a.short}</TableHead>
                            ))}
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {points
                            .filter((_, i) => i % 7 === 0)
                            .map((p) => (
                              <TableRow key={p.date}>
                                <TableCell>{p.date}</TableCell>
                                {assets.map((a) => (
                                  <TableCell key={a.id}>
                                    {percent(p.values[a.id] - 100)}
                                  </TableCell>
                                ))}
                              </TableRow>
                            ))}
                        </TableBody>
                      </Table>
                    </div>
                  </DialogContent>
                </Dialog>
              </CardFooter>
            </Card>

            <aside className="research-note">
              <p className="research-eyebrow">阅读图 1</p>
              <h3>把波动放回时间里</h3>
              <p>
                每条曲线从同一起点出发。高低表示累计变化，曲线间的距离表示同一时点的表现差异。
              </p>
              <p>切换到回撤视图，可观察每条曲线距离此前高点有多远。</p>
              <Separator />
              <div className="positive-months">
                <strong>
                  {upMonths}
                  <span> / {range}</span>
                </strong>
                <span>收益为正的月份</span>
              </div>
              <p className="note-caption">所选区间 · 模拟统计</p>
              <Button
                variant="outline"
                onClick={() =>
                  setMode(mode === "return" ? "drawdown" : "return")
                }
              >
                {mode === "return" ? "查看回撤视图" : "查看收益视图"}
                <ArrowRight data-icon="inline-end" />
              </Button>
            </aside>
          </section>

          <section className="report-section-intro" id="monthly">
            <p className="research-eyebrow">02 / 月度分布</p>
            <h2>把长曲线，拆成一个个月。</h2>
            <p>
              总收益无法告诉我们每个月发生了什么。将月度变化并置，连续的回落与分散的增长便有了更直观的形状。
            </p>
          </section>
          <section className="report-figure-grid" aria-label="月度收益图版">
            <Card className="heatmap-card">
              <CardHeader>
                <ResearchTitle
                  number="图 2"
                  title="月度收益的分布"
                  subtitle="月度收益日历 · 完整三年 · 单位 %"
                />
              </CardHeader>
              <CardContent>
                <div className="heatmap-scroll">
                  <DesignPreviewChart
                    option={heatmapOption}
                    kind="heatmap"
                    label={
                      "2023 至 2025 年模拟月收益热力图，湖蓝为负、胭脂红为正，浅纸色为零。每格显示带符号数值，完整数据位于下方月度数据列表。"
                    }
                  />
                </div>
                <div className="heatmap-legend">
                  <span>负收益</span>
                  {heatmapColors.map((color, i) => (
                    <span
                      className="heatmap-swatch"
                      key={color}
                      style={{ background: color }}
                      aria-label={[-6, -3, 0, 3, 6][i] + "%"}
                    />
                  ))}
                  <span>正收益</span>
                  <span className="heatmap-range">−6% 至 +6%</span>
                </div>
              </CardContent>
              <CardFooter>
                <details>
                  <summary>查看月度数据列表</summary>
                  <div className="monthly-values">
                    {monthly.map((m) => (
                      <span key={m.date}>
                        {m.date} <strong>{percent(m.returns.strategy)}</strong>
                      </span>
                    ))}
                  </div>
                </details>
              </CardFooter>
            </Card>
            <aside className="research-note">
              <p className="research-eyebrow">阅读图 2</p>
              <h3>颜色表达变化</h3>
              <p>
                湖蓝代表负收益，胭脂红代表正收益；颜色越深，偏离零点越远。每格中的数字保留了方向与幅度。
              </p>
              <p>
                此处始终展示完整三年，便于观察月度分布，不随上方区间筛选改变。
              </p>
            </aside>
          </section>
          <section className="report-section-intro" id="comparison">
            <p className="research-eyebrow">03 / 同期比较</p>
            <h2>在相同口径下，重新比较。</h2>
            <p>
              将收益和最大回撤并列，比较才不止于排名。这里的数值与图 1
              共用观察区间；点击表头可改变累计收益的排序。
            </p>
          </section>
          <section className="report-figure-grid" aria-label="同期比较数据">
            <Card className="comparison-card">
              <CardHeader>
                <div className="chart-heading-row">
                  <ResearchTitle
                    number="表 1"
                    title="策略与标的表现"
                    subtitle={"相同观察区间 · 近 " + range + " 个月"}
                  />
                </div>
              </CardHeader>
              <CardContent>
                <Field className="comparison-search">
                  <FieldLabel htmlFor="asset-search">筛选标的</FieldLabel>
                  <Input
                    id="asset-search"
                    placeholder="搜索标的名称"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                  />
                </Field>
                <Table>
                  <TableCaption>
                    所有数值为模拟表现，按所选区间计算。
                  </TableCaption>
                  <TableHeader>
                    <TableRow>
                      <TableHead>比较序列</TableHead>
                      <TableHead
                        aria-sort={descending ? "descending" : "ascending"}
                      >
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDescending(!descending)}
                        >
                          累计收益{" "}
                          <ChevronDown
                            data-icon="inline-end"
                            className={descending ? undefined : "rotate-180"}
                          />
                        </Button>
                      </TableHead>
                      <TableHead>最大回撤</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {comparisons.map((a) => (
                      <TableRow key={a.id} data-highlight={a.id === "strategy"}>
                        <TableCell>
                          <span className="table-series">
                            <span
                              className="series-dot"
                              style={{
                                background:
                                  colors[
                                    assets.findIndex((x) => x.id === a.id)
                                  ],
                              }}
                            />
                            {a.name}
                          </span>
                        </TableCell>
                        <TableCell>
                          <span
                            className="research-number"
                            data-sign={a.total >= 0 ? "positive" : "negative"}
                          >
                            {percent(a.total)}
                          </span>
                        </TableCell>
                        <TableCell className="research-number">
                          {percent(a.drawdown)}
                        </TableCell>
                      </TableRow>
                    ))}
                    {!comparisons.length && (
                      <TableRow>
                        <TableCell colSpan={3}>
                          没有找到匹配的标的，请尝试其他名称。
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
            <aside className="research-note">
              <p className="research-eyebrow">口径说明</p>
              <h3>数值来自同一份序列</h3>
              <p>
                累计收益由起点与终点计算，最大回撤基于区间内的采样净值。它们描述这份模拟样本，不代表未来表现。
              </p>
            </aside>
          </section>

          <ComponentStudy
            range={range}
            onRangeChange={setRange}
            themeStyle={themeStyle}
          />
        </main>
        <footer className="research-footer">
          <a href="#overview" className="research-brand">
            candela
          </a>
          <span>观察 · 比较 · 理解</span>
          <span>设计样板 · 全部数据为模拟 · 非投资建议</span>
          <a href="#overview">回到顶部 ↑</a>
        </footer>
      </div>
    </div>
  );
}
