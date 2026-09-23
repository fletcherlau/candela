import { useCallback, useEffect, useRef, useState } from "react";
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

type Metric = "price" | "score" | "rank" | "volatility" | "quantile" | "weight";
type DailyCard = {
  code: string;
  name: string;
  reasons: Partial<Record<Metric, string>>;
} & Record<Metric, number | null>;
type DailyResult = {
  tradeDate: string;
  basis: "close";
  publishedAt: string;
  source: string;
  quantileWindow: number;
  cards: DailyCard[];
};
type DailyView = {
  tradeDate: string;
  status: string;
  message: string;
  available: number;
  close: DailyResult | null;
  referenceStatus: string;
};
const codes = ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"];
const metrics: { key: Metric; label: string; format: (n: number) => string }[] =
  [
    { key: "price", label: "收盘价", format: (n) => n.toFixed(3) },
    { key: "score", label: "20 日动量", format: (n) => n.toFixed(4) },
    { key: "rank", label: "排名", format: (n) => String(n) },
    { key: "volatility", label: "年化波动率", format: pct },
    { key: "quantile", label: "波动率分位", format: (n) => `${n.toFixed(2)}%` },
    { key: "weight", label: "计算仓位", format: pct },
  ];
const states: Record<string, string> = {
  ready: "收盘已发布",
  pending: "收盘待齐",
  updating: "更新中",
  failed: "计算失败",
  unavailable: "暂无数据",
};

function MetricValue({
  card,
  metric,
}: {
  card: DailyCard;
  metric: (typeof metrics)[number];
}) {
  const value = card[metric.key];
  return finite(value) ? (
    <span className="research-number">{metric.format(value)}</span>
  ) : (
    <span className="rotation-daily-unknown">
      <span aria-label="不可用">—</span>
      <small>{card.reasons?.[metric.key] || "暂无计算依据"}</small>
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
      if (response.status === 401)
        throw new Error("登录已过期，请重新登录后读取每日数据。");
      if (!response.ok)
        throw new Error("暂时无法读取每日数据，请稍后重新读取。");
      const data: DailyView = await response.json();
      if (
        !data ||
        typeof data.status !== "string" ||
        (data.close &&
          (data.close.basis !== "close" ||
            !Array.isArray(data.close.cards) ||
            data.close.cards.length !== 4 ||
            !codes.every((c, i) => data.close!.cards[i]?.code === c)))
      )
        throw new Error("每日数据返回不完整，请稍后重新读取。");
      if (!controller.signal.aborted) {
        setView(data);
        setError("");
      }
    } catch (e) {
      if (
        request.current === controller &&
        (!controller.signal.aborted ||
          controller.signal.reason?.message === "timeout")
      ) {
        setError(
          controller.signal.aborted
            ? "每日数据读取超时，请稍后重新读取。"
            : e instanceof Error
              ? e.message
              : "每日数据读取失败。",
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (request.current === controller) setBusy(false);
    }
  }, []);
  useEffect(() => {
    void load();
    const refreshVisible = () => {
      if (!document.hidden) void load();
    };
    const interval = window.setInterval(refreshVisible, 30000);
    document.addEventListener("visibilitychange", refreshVisible);
    return () => {
      request.current?.abort();
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refreshVisible);
    };
  }, [load]);
  const close = view?.close;
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
                : (states[view?.status ?? ""] ?? "状态未知")}
          </Badge>
          <Button
            variant="outline"
            aria-disabled={busy}
            onClick={() => {
              if (!busy) void load();
            }}
            aria-label="重新读取每日数据"
          >
            <RefreshCw data-icon="inline-start" />
            重新读取
          </Button>
        </div>
      </div>
      <p className="rotation-daily-context">
        {close
          ? `${fmt(close.tradeDate)} · 收盘`
          : view?.tradeDate
            ? `${fmt(view.tradeDate)} · 等待收盘结果`
            : "等待已发布交易日数据"}
        。四标的计算依据，非实际账户持仓。
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>每日数据读取失败</AlertTitle>
          <AlertDescription>
            {error}
            {close && ` 当前保留 ${fmt(close.tradeDate)} 已发布结果。`}
          </AlertDescription>
        </Alert>
      )}
      <div aria-live="polite">
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
      {view && !close && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>暂无完整每日结果</EmptyTitle>
            <EmptyDescription>
              四标的原始数据完整并计算完成后，统一展示收盘指标。未知值不会填零。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {close && (
        <>
          <div className="rotation-daily-desktop">
            <Table>
              <TableCaption>
                {fmt(close.tradeDate)} 四标的收盘计算数据
              </TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead>标的</TableHead>
                  {metrics.map((m) => (
                    <TableHead key={m.key}>{m.label}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {close.cards.map((card) => (
                  <TableRow key={card.code}>
                    <TableCell>
                      <span>{card.name}</span>
                      <small className="rotation-daily-code">{card.code}</small>
                    </TableCell>
                    {metrics.map((m) => (
                      <TableCell key={m.key}>
                        <MetricValue card={card} metric={m} />
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="rotation-daily-mobile">
            {close.cards.map((card) => (
              <Card key={card.code}>
                <CardHeader>
                  <CardTitle>{card.name}</CardTitle>
                  <CardDescription>
                    {card.code} · {fmt(close.tradeDate)} 收盘
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <dl className="rotation-daily-values">
                    {metrics.map((m) => (
                      <div key={m.key}>
                        <dt>{m.label}</dt>
                        <dd>
                          <MetricValue card={card} metric={m} />
                        </dd>
                      </div>
                    ))}
                  </dl>
                </CardContent>
              </Card>
            ))}
          </div>
          <p className="rotation-daily-message">
            14:45 参考未留存，对应价格差异不可用。
          </p>
          <p className="rotation-daily-message">
            20 点 ER 动量；YZ 20 日波动率，按 240 日年化；分位窗口{" "}
            {close.quantileWindow}{" "}
            个有效值。计算仓位为每个标的独立指标，不代表组合配置。
          </p>
          <p className="rotation-daily-source">
            来源：{close.source}。发布于 {publishedAt(close.publishedAt)}。
          </p>
        </>
      )}
    </section>
  );
}
