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
export type RangeWindow = {
  start: string;
  end: string;
  startIndex: number;
  endIndex: number;
  returns: (number | null)[];
  dd: (number | null)[];
  comparisons: (number | null)[][];
  gain: number | null;
  maxdd: number | null;
  missing: boolean;
};
export type View = {
  range: RangeWindow | null;
  holdingChange: string;
  revision: number;
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
// Date validation is a control constraint, not a return/portfolio calculation.
export function validRange(start: string, end: string) {
  const a = new Date(`${fmt(start)}T00:00:00Z`),
    b = new Date(`${fmt(end)}T00:00:00Z`);
  if (
    !Number.isFinite(+a) ||
    !Number.isFinite(+b) ||
    a.toISOString().slice(0, 10).replaceAll("-", "") !== start ||
    b.toISOString().slice(0, 10).replaceAll("-", "") !== end
  )
    return false;
  const today = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  })
    .format(new Date())
    .replaceAll("-", "");
  const limit = new Date(a);
  limit.setUTCFullYear(limit.getUTCFullYear() + 10);
  return a <= b && b <= limit && end <= today;
}
