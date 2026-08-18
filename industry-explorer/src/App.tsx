import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Bar, BarChart, Cell, ComposedChart, XAxis, YAxis } from 'recharts'
import {
  ChartContainer,
  ChartTooltip,
  type ChartConfig,
} from '@/components/ui/chart'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

// [trade_date "YYYY-MM-DD", open, high, low, close, vol]
type Bar = [string, number, number, number, number, number]

interface Manifest {
  l1: { code: string; name: string }[]
  l2: { code: string; name: string; parent_code: string }[]
  generated_at: string
}

interface Candle {
  date: string
  o: number
  h: number
  l: number
  c: number
  vol: number
  up: boolean
}

const RED = '#dc2626' // 涨(红)
const GREEN = '#16a34a' // 跌(绿)

const DEFAULT_L1 = '801080.SI' // 电子
const DEFAULT_L2 = '801081.SI' // 半导体

const RANGES = [
  { key: '1y', label: '近1年', years: 1 },
  { key: '3y', label: '近3年', years: 3 },
  { key: '5y', label: '近5年', years: 5 },
  { key: 'all', label: '全部', years: Infinity },
] as const

type RangeKey = (typeof RANGES)[number]['key']

const chartConfig = {
  c: { label: '收盘', color: 'var(--chart-1)' },
  vol: { label: '成交量', color: 'var(--chart-2)' },
} satisfies ChartConfig

function cutoffDate(latestDate: string, years: number): string {
  if (!isFinite(years)) return '0000-00-00'
  const d = new Date(`${latestDate}T00:00:00`)
  d.setFullYear(d.getFullYear() - years)
  return d.toISOString().slice(0, 10)
}

type Freq = 'd' | 'w' | 'm'

const FREQS = [
  { key: 'd', label: '日线' },
  { key: 'w', label: '周线' },
  { key: 'm', label: '月线' },
] as const

// 以所在周周一的日期作为分组键(本地时区, 避免 toISOString 的 UTC 偏移)
function weekKey(dateStr: string): string {
  const d = new Date(`${dateStr}T00:00:00`)
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7))
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${dd}`
}

// 日线重采样为周线/月线: 开=首日开, 高/低=区间极值, 收=末日收, 量=求和, 日期=周期最后交易日
function resample(daily: Candle[], freq: Freq): Candle[] {
  if (freq === 'd') return daily
  const out: Candle[] = []
  let key = ''
  for (const d of daily) {
    const k = freq === 'm' ? d.date.slice(0, 7) : weekKey(d.date)
    const last = out[out.length - 1]
    if (last && key === k) {
      last.h = Math.max(last.h, d.h)
      last.l = Math.min(last.l, d.l)
      last.c = d.c
      last.vol += d.vol
      last.date = d.date
      last.up = last.c >= last.o
    } else {
      key = k
      out.push({
        date: d.date,
        o: d.o,
        h: d.h,
        l: d.l,
        c: d.c,
        vol: d.vol,
        up: d.c >= d.o,
      })
    }
  }
  return out
}

// (latest close / close of the last bar on or before the cutoff - 1) * 100
function pctChangeSince(bars: Bar[], years: number): number | null {
  if (bars.length < 2) return null
  const cutoff = cutoffDate(bars[bars.length - 1][0], years)
  let base = bars[0]
  for (const b of bars) {
    if (b[0] <= cutoff) base = b
    else break
  }
  if (base[4] === 0) return null
  return (bars[bars.length - 1][4] / base[4] - 1) * 100
}

function PctText({ value }: { value: number | null }) {
  if (value === null) return <span className="text-muted-foreground">—</span>
  const up = value >= 0
  return (
    <span className={cn(up ? 'text-red-600' : 'text-green-600')}>
      {up ? '+' : ''}
      {value.toFixed(2)}%
    </span>
  )
}

function StatItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-lg font-semibold tabular-nums">{children}</span>
    </div>
  )
}

function CandleTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null
  const d = payload[0].payload as Candle
  const chg = d.o ? (d.c / d.o - 1) * 100 : 0
  return (
    <div className="rounded-md border bg-background px-3 py-2 text-xs shadow-md">
      <div className="mb-1 text-muted-foreground">{d.date}</div>
      <div
        className={cn(
          'flex gap-3 tabular-nums',
          d.up ? 'text-red-600' : 'text-green-600',
        )}
      >
        <span>开 {d.o.toFixed(2)}</span>
        <span>高 {d.h.toFixed(2)}</span>
        <span>低 {d.l.toFixed(2)}</span>
        <span>收 {d.c.toFixed(2)}</span>
        <span>
          {chg >= 0 ? '+' : ''}
          {chg.toFixed(2)}%
        </span>
      </div>
      <div className="mt-1 text-muted-foreground">量 {d.vol.toLocaleString()}</div>
    </div>
  )
}

export default function App() {
  const [manifest, setManifest] = useState<Manifest | null>(null)
  const [manifestError, setManifestError] = useState<string | null>(null)
  const [l1Code, setL1Code] = useState(DEFAULT_L1)
  const [indexCode, setIndexCode] = useState(DEFAULT_L2)
  const [bars, setBars] = useState<Bar[] | null>(null)
  const [loading, setLoading] = useState(false)
  // 可视窗口: candles 的下标区间 [i0, i1](含); null = 跟随 lastRange 的默认窗口
  const [view, setView] = useState<[number, number] | null>(null)
  const [freq, setFreq] = useState<Freq>('d')
  const lastRange = useRef<RangeKey>('1y')
  const [dragging, setDragging] = useState(false)
  const cacheRef = useRef<Record<string, Bar[]>>({})

  useEffect(() => {
    fetch('/data/manifest.json')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<Manifest>
      })
      .then(setManifest)
      .catch((e) => setManifestError(String(e)))
  }, [])

  const l1List = manifest?.l1 ?? []
  const l2Children = useMemo(
    () => (manifest?.l2 ?? []).filter((x) => x.parent_code === l1Code),
    [manifest, l1Code],
  )
  const selectedL1 = l1List.find((x) => x.code === l1Code)

  const nameOfL1 = (code: string) =>
    l1List.find((x) => x.code === code)?.name ?? code
  const nameOfIndex = (code: string) =>
    code === l1Code
      ? `${nameOfL1(code)}（一级指数）`
      : (manifest?.l2.find((x) => x.code === code)?.name ?? code)

  function onL1Change(code: string | null) {
    if (!code) return
    setL1Code(code)
    setIndexCode(code) // default to the L1 index itself
  }

  useEffect(() => {
    const cached = cacheRef.current[indexCode]
    if (cached) {
      setBars(cached)
      return
    }
    let cancelled = false
    setLoading(true)
    fetch(`/data/bars/${indexCode}.json`)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<Bar[]>
      })
      .then((data) => {
        cacheRef.current[indexCode] = data
        if (!cancelled) setBars(data)
      })
      .catch(() => {
        if (!cancelled) setBars(null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [indexCode])

  // 切换指数时窗口复位由下方 candles 变化的 effect 处理

  // 上一支/下一支: 全部一级/二级指数按名称拼音排序; 跨界时一级行业下拉跟随
  const zhCollator = useMemo(() => new Intl.Collator('zh-Hans-CN'), [])
  const navList = useMemo(() => {
    if (!manifest) return []
    const items: { code: string; name: string; l1: string; isL1: boolean }[] = []
    for (const x of manifest.l1)
      items.push({ code: x.code, name: x.name, l1: x.code, isL1: true })
    for (const x of manifest.l2)
      items.push({ code: x.code, name: x.name, l1: x.parent_code, isL1: false })
    return items.sort(
      (a, b) =>
        zhCollator.compare(a.name, b.name) ||
        (a.isL1 === b.isL1 ? 0 : a.isL1 ? -1 : 1),
    )
  }, [manifest, zhCollator])
  const navIndex = navList.findIndex((x) => x.code === indexCode)
  const goNav = (delta: number) => {
    const i = navIndex + delta
    if (i < 0 || i >= navList.length) return
    const item = navList[i]
    if (item.l1 !== l1Code) setL1Code(item.l1)
    setIndexCode(item.code)
  }

  // 滚轮缩放的图表容器(原生 wheel 监听以便 preventDefault, 见下方 display 之后的 effect)
  const chartWrapRef = useRef<HTMLDivElement>(null)

  const selectedName =
    indexCode === l1Code
      ? selectedL1?.name
      : (manifest?.l2.find((x) => x.code === indexCode)?.name ?? null)

  const stats = useMemo(() => {
    if (!bars || bars.length === 0) return null
    const latest = bars[bars.length - 1]
    return {
      latestClose: latest[4],
      latestDate: latest[0],
      chg1y: pctChangeSince(bars, 1),
      chg5y: pctChangeSince(bars, 5),
    }
  }, [bars])

  // 全部历史日线; 时间档只设定初始可视窗口, 不限制缩放和拖拽
  const dailyCandles = useMemo<Candle[]>(() => {
    if (!bars) return []
    return bars.map((b) => ({
      date: b[0],
      o: b[1],
      h: b[2],
      l: b[3],
      c: b[4],
      vol: b[5],
      up: b[4] >= b[1],
    }))
  }, [bars])

  // 按日/周/月重采样; 后续的窗口、缩放、平移都基于重采样后的 candles
  const candles = useMemo(() => resample(dailyCandles, freq), [dailyCandles, freq])

  // 时间档对应的默认可视窗口(candles 下标区间)
  const defaultView = (key: RangeKey): [number, number] => {
    const n = candles.length
    if (n === 0) return [0, -1]
    const years = RANGES.find((r) => r.key === key)?.years ?? Infinity
    if (!isFinite(years)) return [0, n - 1]
    const cutoff = cutoffDate(candles[n - 1].date, years)
    const i = candles.findIndex((d) => d.date >= cutoff)
    return [i >= 0 ? i : 0, n - 1]
  }

  const effView: [number, number] = view ?? defaultView(lastRange.current)
  const [vi0, vi1] = effView
  const display = useMemo(
    () => candles.slice(Math.max(0, vi0), vi1 + 1),
    [candles, vi0, vi1],
  )

  // 窗口是否等于某时间档的默认窗口(用于按钮高亮)
  const isDefaultView = (key: RangeKey) => {
    const [a, b] = defaultView(key)
    return a === vi0 && b === vi1
  }

  // 切换指数(candles 更换)时回到最近一次时间档的默认窗口
  useEffect(() => {
    setView(null)
  }, [candles])

  // 滚轮缩放(仿富途): 以鼠标位置为锚点缩放可视窗口
  useEffect(() => {
    const el = chartWrapRef.current
    if (!el || candles.length < 2) return
    const PLOT_LEFT = 64 // margin.left 8 + YAxis width 56
    const PLOT_RIGHT = 8
    const MIN_BARS = 10
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const n = candles.length
      const [i0, i1] = view ?? defaultView(lastRange.current)
      const span = i1 - i0 + 1
      const rect = el.getBoundingClientRect()
      const w = rect.width - PLOT_LEFT - PLOT_RIGHT
      const f =
        w > 0
          ? Math.min(1, Math.max(0, (e.clientX - rect.left - PLOT_LEFT) / w))
          : 0.5
      const zoomIn = e.deltaY < 0
      const step = Math.max(1, Math.round(span * 0.2))
      const newSpan = zoomIn
        ? Math.max(MIN_BARS, span - step)
        : Math.min(n, span + step)
      if (newSpan === n) {
        setView([0, n - 1])
        return
      }
      const anchor = i0 + span * f
      const ni0 = Math.max(
        0,
        Math.min(n - newSpan, Math.round(anchor - newSpan * f)),
      )
      setView([ni0, ni0 + newSpan - 1])
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [candles, view])

  // Y 轴范围跟随展示数据的最高/最低价
  const yDomain = useMemo<[number, number]>(() => {
    if (display.length === 0) return [0, 1]
    let lo = Infinity
    let hi = -Infinity
    for (const d of display) {
      if (d.l < lo) lo = d.l
      if (d.h > hi) hi = d.h
    }
    const pad = (hi - lo) * 0.05 || hi * 0.01 || 1
    return [lo - pad, hi + pad]
  }, [display])

  const rangeHigh = display.length ? Math.max(...display.map((b) => b.h)) : null
  const rangeLow = display.length ? Math.min(...display.map((b) => b.l)) : null

  // 自定义 K 线 shape: 根据 yDomain 把价格映射到像素, 画影线 + 实体
  const [dMin, dMax] = yDomain
  const renderCandle = (props: any) => {
    const { x, width, payload, background } = props
    if (!background || !payload) return <g />
    const span = dMax - dMin || 1
    const Y = (v: number) =>
      background.y + background.height * (1 - (v - dMin) / span)
    const cx = x + width / 2
    const color = payload.up ? RED : GREEN
    const bodyW = Math.max(1, Math.min(width * 0.8, 12))
    const bodyTop = Y(Math.max(payload.o, payload.c))
    const bodyH = Math.max(1, Math.abs(Y(payload.o) - Y(payload.c)))
    return (
      <g>
        <line
          x1={cx}
          x2={cx}
          y1={Y(payload.h)}
          y2={Y(payload.l)}
          stroke={color}
          strokeWidth={1}
        />
        <rect x={cx - bodyW / 2} y={bodyTop} width={bodyW} height={bodyH} fill={color} />
      </g>
    )
  }

  // 左键拖动平移: 内容跟随鼠标(向右拖=看更早的 K 线)
  function onChartMouseDown(e: React.MouseEvent) {
    if (e.button !== 0 || candles.length < 2) return
    const el = chartWrapRef.current
    if (!el) return
    e.preventDefault()
    const plotW = el.getBoundingClientRect().width - 64 - 8
    const [i0, i1] = effView
    const span = i1 - i0 + 1
    const barW = plotW / span || 1
    const startX = e.clientX
    const n = candles.length
    setDragging(true)
    const onMove = (ev: MouseEvent) => {
      const delta = Math.round((ev.clientX - startX) / barW)
      if (delta === 0) return
      const ni0 = Math.max(0, Math.min(n - span, i0 - delta))
      setView([ni0, ni0 + span - 1])
    }
    const onUp = () => {
      setDragging(false)
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  return (
    <div className="mx-auto max-w-6xl p-6">
      <header className="mb-6">
        <h1 className="text-2xl font-bold">申万行业指数浏览</h1>
        {manifest && (
          <p className="mt-1 text-sm text-muted-foreground">
            数据生成于 {new Date(manifest.generated_at).toLocaleString('zh-CN')} ·{' '}
            {manifest.l1.length} 个一级行业 / {manifest.l2.length} 个二级行业
          </p>
        )}
      </header>

      <div className="mb-6 flex flex-wrap gap-4">
        <div className="flex flex-col gap-1.5">
          <span className="text-xs text-muted-foreground">一级行业</span>
          <Select value={l1Code} onValueChange={onL1Change}>
            <SelectTrigger className="w-48">
              <SelectValue placeholder="选择一级行业">
                {(v: string) => nameOfL1(v)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {l1List.map((x) => (
                <SelectItem key={x.code} value={x.code}>
                  {x.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="text-xs text-muted-foreground">二级行业</span>
          <Select value={indexCode} onValueChange={(v) => v && setIndexCode(v)}>
            <SelectTrigger className="w-56">
              <SelectValue placeholder="选择二级行业">
                {(v: string) => nameOfIndex(v)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {selectedL1 && (
                <SelectItem value={selectedL1.code}>
                  {selectedL1.name}（一级指数）
                </SelectItem>
              )}
              {l2Children.map((x) => (
                <SelectItem key={x.code} value={x.code}>
                  {x.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="ml-auto flex items-end gap-1">
          <button
            onClick={() => goNav(-1)}
            disabled={navIndex <= 0}
            title="上一支"
            aria-label="上一支"
            className="rounded-md border p-2 text-muted-foreground transition-colors hover:bg-muted disabled:opacity-40 disabled:hover:bg-transparent"
          >
            <ChevronLeft className="h-4 w-4" />
          </button>
          <button
            onClick={() => goNav(1)}
            disabled={navIndex < 0 || navIndex >= navList.length - 1}
            title="下一支"
            aria-label="下一支"
            className="rounded-md border p-2 text-muted-foreground transition-colors hover:bg-muted disabled:opacity-40 disabled:hover:bg-transparent"
          >
            <ChevronRight className="h-4 w-4" />
          </button>
          <span className="ml-1 self-center text-xs tabular-nums text-muted-foreground">
            {navIndex >= 0 ? `${navIndex + 1} / ${navList.length}` : ''}
          </span>
        </div>
      </div>

      {manifestError && (
        <p className="text-red-600">manifest 加载失败：{manifestError}</p>
      )}

      <Card>
        <CardHeader>
          <CardTitle>{selectedName ?? '—'}</CardTitle>
          <CardDescription>
            K 线走势（申万 2021 版行业分类）· 左键拖动平移 · 滚轮缩放 · 点时间档复位窗口
          </CardDescription>
          <div className="flex items-center gap-2 pt-2">
            {FREQS.map((f) => (
              <button
                key={f.key}
                onClick={() => setFreq(f.key)}
                className={cn(
                  'rounded-md border px-3 py-1 text-sm transition-colors',
                  freq === f.key
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'bg-background text-muted-foreground hover:bg-muted',
                )}
              >
                {f.label}
              </button>
            ))}
            <span className="mx-1 h-5 w-px bg-border" />
            {RANGES.map((r) => (
              <button
                key={r.key}
                onClick={() => {
                  lastRange.current = r.key
                  setView(defaultView(r.key))
                }}
                className={cn(
                  'rounded-md border px-3 py-1 text-sm transition-colors',
                  isDefaultView(r.key)
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'bg-background text-muted-foreground hover:bg-muted',
                )}
              >
                {r.label}
              </button>
            ))}
          </div>
        </CardHeader>
        <CardContent>
          {stats && (
            <div className="mb-6 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
              <StatItem label="最新收盘">{stats.latestClose.toFixed(2)}</StatItem>
              <StatItem label="最新日期">{stats.latestDate}</StatItem>
              <StatItem label="近1年涨跌幅">
                <PctText value={stats.chg1y} />
              </StatItem>
              <StatItem label="近5年涨跌幅">
                <PctText value={stats.chg5y} />
              </StatItem>
              <StatItem label="区间最高">
                {rangeHigh !== null ? rangeHigh.toFixed(2) : '—'}
              </StatItem>
              <StatItem label="区间最低">
                {rangeLow !== null ? rangeLow.toFixed(2) : '—'}
              </StatItem>
            </div>
          )}

          {loading ? (
            <div className="flex h-80 items-center justify-center text-muted-foreground">
              加载中…
            </div>
          ) : display.length === 0 ? (
            <div className="flex h-80 items-center justify-center text-muted-foreground">
              暂无数据
            </div>
          ) : (
            <div
              ref={chartWrapRef}
              onMouseDown={onChartMouseDown}
              className={dragging ? 'cursor-grabbing' : 'cursor-grab'}
            >
              <ChartContainer config={chartConfig} className="h-96 w-full select-none">
                <ComposedChart
                  data={display}
                  margin={{ top: 4, right: 8, bottom: 0, left: 8 }}
                >
                  <XAxis
                    dataKey="date"
                    tickLine={false}
                    axisLine={false}
                    minTickGap={48}
                    tickFormatter={(v: string) => v.slice(0, 7)}
                  />
                  <YAxis
                    domain={yDomain}
                    tickLine={false}
                    axisLine={false}
                    width={56}
                    tickFormatter={(v: number) => v.toFixed(0)}
                  />
                  <ChartTooltip content={<CandleTooltip />} />
                  <Bar dataKey="c" shape={renderCandle} isAnimationActive={false} />
                </ComposedChart>
              </ChartContainer>

              <ChartContainer config={chartConfig} className="mt-2 h-16 w-full select-none">
                <BarChart data={display} margin={{ top: 0, right: 8, bottom: 0, left: 8 }}>
                  <XAxis dataKey="date" hide />
                  <YAxis hide domain={[0, 'dataMax']} />
                  <Bar dataKey="vol" isAnimationActive={false}>
                    {display.map((d) => (
                      <Cell key={d.date} fill={d.up ? RED : GREEN} opacity={0.5} />
                    ))}
                  </Bar>
                </BarChart>
              </ChartContainer>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
