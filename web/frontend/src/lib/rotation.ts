// Published historical results only. Nullable fields remain unknown, never zero-filled.
export type Day = {
  date: string;
  nav: number | null;
  holding: string | null;
  weight: number | null;
  cashWeight: number | null;
  cost: number | null;
  turnover: number | null;
  benchmarks: (number | null)[];
  suspended?: string[];
};
export type Result = {
  version: string;
  start: string;
  end: string;
  days: Day[];
  codes: string[];
  names: string[];
  costBps: number | null;
};
export type View = {
  status: string;
  message: string;
  updatedAt: string;
  result: Result | null;
};
export const finite = (n: unknown): n is number =>
  typeof n === "number" && Number.isFinite(n);
export const fmt = (s?: string | null) =>
  s && /^\d{8}$/.test(s)
    ? `${s.slice(0, 4)}-${s.slice(4, 6)}-${s.slice(6, 8)}`
    : "—";
export const pct = (n: unknown) =>
  finite(n) ? `${(Math.abs(n) < 0.00005 ? 0 : n * 100).toFixed(2)}%` : "—";
export const number = (n: unknown) => (finite(n) ? n.toFixed(4) : "—");
export const publishedAt = (s?: string) =>
  s && Number.isFinite(Date.parse(s))
    ? new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      }).format(new Date(s)) + "（北京时间）"
    : "—";
export function holdingName(day: Day | undefined, result: Result) {
  if (!day || day.holding == null) return "—";
  if (day.holding === "")
    return day.weight === 0 && day.cashWeight === 1 ? "现金" : "—";
  const index = result.codes.indexOf(day.holding);
  return index >= 0 ? result.names[index] || day.holding : day.holding;
}
export function holdingChange(days: Day[], result: Result) {
  const current = days.at(-1),
    previous = days.at(-2);
  if (
    !current ||
    !previous ||
    [current, previous].some(
      (d) => d.holding == null || !finite(d.weight) || !finite(d.cashWeight),
    )
  )
    return "前后交易日字段不足，无法比较";
  if (current.holding !== previous.holding)
    return `${holdingName(previous, result)} → ${holdingName(current, result)}`;
  const delta = current.weight! - previous.weight!;
  if (
    Math.abs(delta) < 1e-10 &&
    Math.abs(current.cashWeight! - previous.cashWeight!) < 1e-10
  )
    return "标的与收盘权重相同";
  return `标的相同；权重变化 ${delta > 0 ? "+" : ""}${(delta * 100).toFixed(2)} 个百分点`;
}
export function rangeSeries(days: Day[], lo: number, count: number) {
  if (!days.length)
    return { returns: [], dd: [], comparisons: [], gain: null, maxdd: null };
  // Preserve initial purchase fees over the full history, and the existing slice convention.
  const base = lo === 0 ? 1 : days[0].nav;
  const returns = days.map((d) =>
    finite(base) && base > 0 && finite(d.nav) && d.nav > 0
      ? d.nav / base - 1
      : null,
  );
  let peak = base,
    complete = finite(base) && base > 0;
  const dd = days.map((d) => {
    if (!finite(d.nav) || d.nav <= 0) complete = false;
    if (!complete || !finite(peak) || !finite(d.nav)) return null;
    peak = Math.max(peak, d.nav);
    return d.nav / peak - 1;
  });
  const comparisons = Array.from({ length: count }, (_, j) =>
    days.map((d) => {
      const first = days[0].benchmarks?.[j],
        value = d.benchmarks?.[j];
      return finite(first) && first > 0 && finite(value) && value > 0
        ? value / first - 1
        : null;
    }),
  );
  return {
    returns,
    dd,
    comparisons,
    gain: days.length > 1 ? (returns.at(-1) ?? null) : null,
    maxdd:
      days.length > 1 && complete ? Math.min(0, ...(dd as number[])) : null,
  };
}
