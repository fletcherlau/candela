import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
} from "@/components/ui/table";
import { finite, fmt, pct, publishedAt } from "@/lib/rotation";
import {
  bps,
  sourceTime,
  dailyCodes,
  dailyNames,
  isDailyView,
  type Metric,
  type DailyCard,
  type DailyStage,
  type DailyView,
} from "@/lib/rotation-daily";

const metrics: { key: Metric; label: string; format: (n: number) => string }[] =
  [
    {
      key: "price",
      label: "价格",
      format: (n) =>
        n.toLocaleString("zh-CN", {
          minimumFractionDigits: 3,
          maximumFractionDigits: 6,
        }),
    },
    { key: "score", label: "20 日动量", format: (n) => n.toFixed(4) },
    { key: "rank", label: "排名", format: String },
    { key: "volatility", label: "年化波动率", format: pct },
    { key: "quantile", label: "波动率分位", format: (n) => `${n.toFixed(2)}%` },
    { key: "weight", label: "计算仓位", format: pct },
  ];
const states: Record<string, string> = {
  ready: "数据已发布",
  pending: "等待生成",
  updating: "更新中",
  failed: "计算失败",
  unavailable: "暂无数据",
  calendar_unavailable: "日历未确认",
};
const stageNames: Record<string, string> = {
  ready: "已发布",
  pending: "生成中",
  updating: "更新中",
  failed: "计算失败",
  unavailable: "未发布",
  missing: "缺失",
  waiting: "等待计划时点",
};
function MetricValue({
  card,
  metric,
}: {
  card?: DailyCard;
  metric: (typeof metrics)[number];
}) {
  const value = card?.[metric.key];
  return finite(value) ? (
    <span className="research-number">{metric.format(value)}</span>
  ) : (
    <span className="rotation-daily-unknown">
      <span aria-label="不可用">—</span>
      <small>
        {card?.reasons[metric.key] || (card ? "暂无计算依据" : "未发布")}
      </small>
    </span>
  );
}
function Stage({ label, state }: { label: string; state: DailyStage }) {
  return (
    <div className="rotation-daily-stage">
      <p>
        <strong>{label}</strong> · {stageNames[state.status] || "状态待确认"} ·
        原始数据 {state.available}/4
      </p>
      <p>{state.message}</p>
      {state.missing.length > 0 && (
        <ul>
          {state.missing.map((item) => (
            <li key={item.code}>
              {item.code}：{item.reason || "等待原始数据"}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
function Slippage({ value }: { value: DailyView["priceSlippage"][number] }) {
  return value.bps !== null ? (
    <span className="research-number">{bps(value.bps)}</span>
  ) : (
    <span className="rotation-daily-unknown">
      <span aria-label="价格滑点不可用">—</span>
      <small>{value.reason}</small>
    </span>
  );
}
export function RotationDaily() {
  const [view, setView] = useState<DailyView | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const timeout = window.setTimeout(
      () => controller.abort(new Error("timeout")),
      10000,
    );
    setBusy(true);
    try {
      const response = await fetch("/api/rotation/daily", {
        signal: controller.signal,
        cache: "no-store",
      });
      if (response.status === 401 || response.status === 403)
        throw new Error("登录已过期或无权访问，请重新登录后读取每日数据。");
      if (!response.ok)
        throw new Error("暂时无法读取每日数据，请稍后重新读取。");
      const data: unknown = await response.json();
      if (!isDailyView(data))
        throw new Error("每日数据返回不完整，请稍后重新读取。");
      if (!controller.signal.aborted && request.current === controller) {
        setView(data);
        setError("");
      }
    } catch (e) {
      if (
        request.current === controller &&
        (!controller.signal.aborted ||
          controller.signal.reason?.message === "timeout")
      )
        setError(
          controller.signal.aborted
            ? "每日数据读取超时，请稍后重新读取。"
            : e instanceof Error
              ? e.message
              : "每日数据读取失败。",
        );
    } finally {
      window.clearTimeout(timeout);
      if (request.current === controller) setBusy(false);
    }
  }, []);
  useEffect(() => {
    void load();
    const refresh = () => {
      if (!document.hidden) void load();
    };
    const interval = window.setInterval(refresh, 30000);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      request.current?.abort();
      request.current = null;
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [load]);
  const published = !!(view?.reference || view?.close);
  return (
    <section
      id="daily-data"
      className="rotation-daily"
      aria-labelledby="daily-title"
    >
      <div className="rotation-section-heading">
        <h2 id="daily-title">每日数据</h2>
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant="outline">
            {error
              ? "读取失败"
              : busy && !view
                ? "正在读取"
                : states[view?.status || ""] || "状态未知"}
          </Badge>
          <Button
            variant="outline"
            aria-disabled={busy}
            aria-busy={busy}
            onClick={() => {
              if (!busy) void load();
            }}
            aria-label="重新读取每日数据"
          >
            <RefreshCw data-icon="inline-start" aria-hidden="true" />
            重新读取
          </Button>
        </div>
      </div>
      <p className="rotation-daily-context">
        {view?.tradeDate
          ? `${fmt(view.tradeDate)} · ${view.reference && view.close ? "14:45／收盘" : view.reference ? "14:45 固定参考" : view.close ? "收盘" : "尚无已发布结果"}`
          : "等待确认可展示的交易日"}
        。四标的计算依据，非实际账户持仓。
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>每日数据读取失败</AlertTitle>
          <AlertDescription>
            {error}
            {published && ` 当前保留 ${fmt(view?.tradeDate)} 已发布结果。`}
          </AlertDescription>
        </Alert>
      )}
      <div aria-live="polite">
        {view?.fallbackReason && (
          <p className="rotation-daily-message">{view.fallbackReason}。</p>
        )}
        {view?.message && (
          <p className="rotation-daily-message">{view.message}</p>
        )}
      </div>
      {!view && busy && (
        <div aria-label="正在读取每日数据" className="flex flex-col gap-3">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      )}
      {view?.pending && (
        <Alert role="status">
          <AlertTitle className="line-clamp-none">
            {fmt(view.pending.tradeDate)} 当日进度
          </AlertTitle>
          <AlertDescription>
            <div className="rotation-daily-stages">
              <Stage label="14:45 参考" state={view.pending.reference} />
              <Stage label="收盘" state={view.pending.close} />
            </div>
          </AlertDescription>
        </Alert>
      )}
      {view && !published && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>暂无完整每日结果</EmptyTitle>
            <EmptyDescription>
              四标的原始数据完整并计算完成后统一发布。未知值保留为空，日期与阶段由后端确认。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {view && view.tradeDate && (
        <div
          className="rotation-daily-stages"
          aria-label={`${fmt(view.tradeDate)} 数据状态`}
        >
          <Stage label="14:45 参考" state={view.referenceState} />
          <Stage label="收盘" state={view.closeState} />
        </div>
      )}
      {view && published && (
        <>
          <div className="rotation-daily-desktop">
            <Table aria-label="每日两时点指标">
              <TableCaption>
                {fmt(view.tradeDate)} 四标的两时点计算数据 · 北京时间
              </TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead>标的</TableHead>
                  <TableHead>时点</TableHead>
                  {metrics.map((m) => (
                    <TableHead key={m.key}>{m.label}</TableHead>
                  ))}
                  <TableHead>价格滑点</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {dailyCodes.map((code, i) => (
                  <Fragment key={code}>
                    {(["reference", "close"] as const).map((basis, j) => (
                      <TableRow key={basis}>
                        <>
                          {j === 0 && (
                            <TableCell rowSpan={2}>
                              <span>{dailyNames[i]}</span>
                              <small className="rotation-daily-code">
                                {code}
                              </small>
                              {view.reference?.cards[i].sourceTime && (
                                <small className="rotation-daily-code">
                                  源{" "}
                                  {sourceTime(
                                    view.reference.cards[i].sourceTime,
                                  )}
                                  <br />
                                  采集{" "}
                                  {sourceTime(
                                    view.reference.cards[i].capturedAt,
                                  )}
                                </small>
                              )}
                            </TableCell>
                          )}
                        </>
                        <TableCell>{j === 0 ? "14:45" : "收盘"}</TableCell>
                        {metrics.map((m) => (
                          <TableCell key={m.key}>
                            <MetricValue
                              card={view[basis]?.cards[i]}
                              metric={m}
                            />
                          </TableCell>
                        ))}
                        {j === 0 && (
                          <TableCell rowSpan={2}>
                            <Slippage value={view.priceSlippage[i]} />
                          </TableCell>
                        )}
                      </TableRow>
                    ))}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="rotation-daily-mobile">
            {dailyCodes.map((code, i) => (
              <Card key={code} data-daily-code={code}>
                <CardHeader>
                  <CardTitle>{dailyNames[i]}</CardTitle>
                  <CardDescription>
                    {code} · {fmt(view.tradeDate)}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Table
                    className="rotation-daily-comparison"
                    aria-label={`${dailyNames[i]}两时点指标`}
                  >
                    <TableHeader>
                      <TableRow>
                        <TableHead>指标</TableHead>
                        <TableHead>14:45</TableHead>
                        <TableHead>收盘</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {metrics.map((m) => (
                        <TableRow key={m.key}>
                          <TableCell>{m.label}</TableCell>
                          <TableCell>
                            <MetricValue
                              card={view.reference?.cards[i]}
                              metric={m}
                            />
                          </TableCell>
                          <TableCell>
                            <MetricValue
                              card={view.close?.cards[i]}
                              metric={m}
                            />
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  <p className="rotation-daily-slippage">
                    价格滑点：
                    <Slippage value={view.priceSlippage[i]} />
                  </p>
                  {view.reference?.cards[i].sourceTime && (
                    <p className="rotation-daily-source">
                      参考源时间{" "}
                      {sourceTime(view.reference.cards[i].sourceTime)} · 采集{" "}
                      {sourceTime(view.reference.cards[i].capturedAt)}
                      （北京时间）
                    </p>
                  )}
                </CardContent>
              </Card>
            ))}
          </div>
          <p className="rotation-daily-message">
            价格滑点为同日收盘价相对 14:45 价格的差异，只显示
            bps；不是实际成交滑点。
          </p>
          <p className="rotation-daily-message">
            20 点 ER 动量；YZ 20 日波动率，按 240
            日年化。计算仓位为每个标的独立的 w(q)，不代表组合配置或操作结论。
          </p>
          {(["reference", "close"] as const).map((basis) => {
            const result = view[basis];
            return (
              result && (
                <p key={basis} className="rotation-daily-source">
                  {basis === "reference" ? "14:45 参考" : "收盘"}来源：
                  {result.source}。分位窗口 {result.quantileWindow}{" "}
                  个有效值。发布于 {publishedAt(result.publishedAt)}。
                </p>
              )
            );
          })}
        </>
      )}
    </section>
  );
}
