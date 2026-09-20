// Fictional observations for visual review; never use as market data.
export const assets = [
  { id: "strategy", name: "四标的轮动", short: "轮动策略" },
  { id: "dividend", name: "红利 ETF", short: "红利" },
  { id: "gold", name: "黄金 ETF", short: "黄金" },
  { id: "growth", name: "创业板 ETF", short: "创业板" },
  { id: "nasdaq", name: "纳指 ETF", short: "纳指" },
] as const;
export type AssetId = (typeof assets)[number]["id"];
const strategy = [
  2.4, -1.3, 3.2, 1.6, -2.7, 2.1, 3.8, -0.8, 1.4, -1.2, 4.3, 2.2, -1.8, 4.6,
  2.1, 1.2, -2.4, 3.1, 1.7, -0.9, 5.2, 2.8, -1.6, 3.4, 2.1, 1.5, -2.3, -3.1,
  4.8, 2.6, 3.5, -0.7, 4.2, 1.9, -1.2, 3.1,
];
const returns: Record<AssetId, number[]> = {
  strategy,
  dividend: strategy.map(
    (_, i) => 0.7 + 1.5 * Math.sin(i * 1.4) + 0.7 * Math.cos(i * 2.2),
  ),
  gold: strategy.map(
    (_, i) => 1.05 + 2.2 * Math.sin(i * 0.75 + 1) + 0.7 * Math.cos(i * 1.8),
  ),
  growth: strategy.map(
    (_, i) => 0.6 + 4.7 * Math.sin(i * 1.1 + 2) + 1.5 * Math.cos(i * 0.4),
  ),
  nasdaq: strategy.map(
    (_, i) => 1.35 + 3 * Math.sin(i * 0.8 + 0.8) + Math.cos(i * 1.9),
  ),
};
export const monthly = strategy.map((_, i) => ({
  date:
    String(2023 + Math.floor(i / 12)) +
    "-" +
    String((i % 12) + 1).padStart(2, "0"),
  returns: Object.fromEntries(
    assets.map((a) => [a.id, returns[a.id][i]]),
  ) as Record<AssetId, number>,
}));
export function observations(months: number) {
  const selected = monthly.slice(-months);
  const first = new Date(selected[0].date + "-01T00:00:00Z");
  first.setUTCDate(0);
  const nav = Object.fromEntries(assets.map((a) => [a.id, 100])) as Record<
    AssetId,
    number
  >;
  const points = [
    { date: first.toISOString().slice(0, 10), values: { ...nav } },
  ];
  for (const month of selected) {
    const start = { ...nav };
    const end = new Date(month.date + "-01T00:00:00Z");
    end.setUTCMonth(end.getUTCMonth() + 1, 0);
    for (let step = 1; step <= 7; step++) {
      for (const [index, a] of assets.entries()) {
        const t = step / 7;
        const noise =
          Math.sin(t * Math.PI * 4) *
          Math.sin(t * Math.PI) *
          (index ? 0.65 : 0.32);
        nav[a.id] =
          start[a.id] * (1 + (month.returns[a.id] * t) / 100 + noise / 100);
      }
      const day = step === 7 ? end.getUTCDate() : step * 4;
      points.push({
        date: month.date + "-" + String(day).padStart(2, "0"),
        values: { ...nav },
      });
    }
  }
  return points;
}
export function metrics(
  points: ReturnType<typeof observations>,
  id: AssetId,
  months: number,
) {
  let peak = points[0].values[id],
    drawdown = 0;
  for (const p of points) {
    peak = Math.max(peak, p.values[id]);
    drawdown = Math.min(drawdown, (p.values[id] / peak - 1) * 100);
  }
  const total = (points.at(-1)!.values[id] / points[0].values[id] - 1) * 100;
  return {
    total,
    annual: (Math.pow(1 + total / 100, 12 / months) - 1) * 100,
    drawdown,
  };
}
export const percent = (v: number) =>
  (v >= 0 ? "+" : "−") + Math.abs(v).toFixed(2) + "%";
