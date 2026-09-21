import { finite } from "@/lib/rotation";
export const dailyCodes = ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"];
export const dailyNames = ["红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"];
export type Metric =
  "price" | "score" | "rank" | "volatility" | "quantile" | "weight";
export type DailyCard = {
  code: string;
  name: string;
  reasons: Partial<Record<Metric, string>>;
  sourceTime?: string;
  capturedAt?: string;
} & Record<Metric, number | null>;
export type DailyResult = {
  tradeDate: string;
  basis: "close" | "reference_1445";
  publishedAt: string;
  source: string;
  version: string;
  quantileWindow: number;
  cards: DailyCard[];
};
export type DailyStage = {
  status: string;
  available: number;
  message: string;
  updatedAt: string;
  missing: { code: string; reason: string }[];
};
export type DailyView = {
  requestedDate: string;
  currentDate: string;
  currentTradingDate: string;
  tradeDate: string;
  selectionMode: string;
  fallback: boolean;
  fallbackReason: string;
  calendarStatus: string;
  status: string;
  message: string;
  close: DailyResult | null;
  reference: DailyResult | null;
  referenceState: DailyStage;
  closeState: DailyStage;
  pending?: { tradeDate: string; reference: DailyStage; close: DailyStage };
  priceSlippage: { code: string; bps: number | null; reason: string }[];
};
function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object";
}
const date = (value: unknown) =>
  typeof value === "string" && (value === "" || /^\d{8}$/.test(value));
const time = (value: unknown) =>
  typeof value === "string" && Number.isFinite(Date.parse(value));
function stage(value: unknown): value is DailyStage {
  return (
    record(value) &&
    typeof value.status === "string" &&
    typeof value.message === "string" &&
    typeof value.updatedAt === "string" &&
    Number.isInteger(value.available) &&
    Number(value.available) >= 0 &&
    Number(value.available) <= 4 &&
    Array.isArray(value.missing) &&
    value.missing.every(
      (item) =>
        record(item) &&
        typeof item.code === "string" &&
        typeof item.reason === "string",
    )
  );
}
function result(value: unknown, basis: string, tradeDate: string) {
  return (
    value === null ||
    (record(value) &&
      value.basis === basis &&
      value.tradeDate === tradeDate &&
      time(value.publishedAt) &&
      typeof value.source === "string" &&
      typeof value.version === "string" &&
      Number.isInteger(value.quantileWindow) &&
      Number(value.quantileWindow) > 0 &&
      Array.isArray(value.cards) &&
      value.cards.length === 4 &&
      value.cards.every(
        (card, i) =>
          record(card) &&
          card.code === dailyCodes[i] &&
          typeof card.name === "string" &&
          record(card.reasons) &&
          Object.values(card.reasons).every(
            (reason) => typeof reason === "string",
          ) &&
          ["price", "score", "rank", "volatility", "quantile", "weight"].every(
            (key) => card[key] === null || finite(card[key]),
          ) &&
          (card.sourceTime === undefined || time(card.sourceTime)) &&
          (card.capturedAt === undefined || time(card.capturedAt)),
      ))
  );
}
export function isDailyView(value: unknown): value is DailyView {
  if (
    !record(value) ||
    !["requestedDate", "currentDate", "currentTradingDate", "tradeDate"].every(
      (key) => date(value[key]),
    ) ||
    typeof value.selectionMode !== "string" ||
    typeof value.fallback !== "boolean" ||
    typeof value.fallbackReason !== "string" ||
    typeof value.calendarStatus !== "string" ||
    typeof value.status !== "string" ||
    typeof value.message !== "string" ||
    !stage(value.referenceState) ||
    !stage(value.closeState)
  )
    return false;
  if (
    !result(value.close, "close", String(value.tradeDate)) ||
    !result(value.reference, "reference_1445", String(value.tradeDate))
  )
    return false;
  if (
    value.pending !== undefined &&
    (!record(value.pending) ||
      !date(value.pending.tradeDate) ||
      !stage(value.pending.reference) ||
      !stage(value.pending.close))
  )
    return false;
  return (
    Array.isArray(value.priceSlippage) &&
    value.priceSlippage.length === 4 &&
    value.priceSlippage.every(
      (item, i) =>
        record(item) &&
        item.code === dailyCodes[i] &&
        (item.bps === null || finite(item.bps)) &&
        typeof item.reason === "string",
    )
  );
}
export function bps(value: number) {
  const rounded = Number(value.toFixed(1));
  return `${rounded === 0 ? "0" : `${rounded > 0 ? "+" : "−"}${Math.abs(rounded)}`} bps`;
}
export function sourceTime(value?: string) {
  return value && time(value)
    ? new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23",
      }).format(new Date(value))
    : "—";
}
