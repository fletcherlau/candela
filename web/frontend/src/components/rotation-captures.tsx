import { useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { ResearchTheme } from "@/components/research-theme";
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
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@/components/ui/table";

type CaptureItem = {
  code: string;
  name: string;
  state: string;
  reason: string;
  input?: {
    quote: { source: string; sourceTime: string; Latest: number };
    requestedAt: string;
    capturedAt: string;
  };
};
type CaptureRun = {
  tradeDate: string;
  targetAt: string;
  state: string;
  stage: string;
  available: number;
  message: string;
  updatedAt: string;
  recoveries: number;
  items?: CaptureItem[];
};
const states: Record<string, string> = {
  queued: "等待采集",
  running: "采集中",
  captured: "原始数据采集完成／参考待计算",
  partial: "部分原始数据缺失",
  missing: "原始数据缺失",
};
const stages: Record<string, string> = {
  queued: "等待执行",
  recovering: "等待恢复原目标",
  capturing: "取得并保存原始数据",
  awaiting_calculation: "参考待计算",
  reference_published: "参考已发布",
  reference_failed: "参考计算失败",
  inputs_incomplete: "整组参考不可发布",
  inputs_missing: "当前接入无法补取",
};
const stateLabel = (run: CaptureRun) =>
  run.stage === "reference_published"
    ? "原始数据采集完成／参考已发布"
    : run.stage === "reference_failed"
      ? "原始数据采集完成／参考计算失败"
      : states[run.state];
const codes = ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"];
const date = (value: string) =>
  `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6)}`;
const time = (value: string) =>
  new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).format(new Date(value));
const isTime = (value: unknown): value is string =>
  typeof value === "string" && Number.isFinite(Date.parse(value));
function isRun(value: unknown): value is CaptureRun {
  if (!value || typeof value !== "object") return false;
  const run = value as Record<string, unknown>;
  return (
    typeof run.tradeDate === "string" &&
    /^\d{8}$/.test(run.tradeDate) &&
    typeof run.state === "string" &&
    Object.hasOwn(states, run.state) &&
    typeof run.stage === "string" &&
    typeof run.message === "string" &&
    isTime(run.targetAt) &&
    isTime(run.updatedAt) &&
    Number.isInteger(run.available) &&
    Number(run.available) >= 0 &&
    Number(run.available) <= 4 &&
    Number.isInteger(run.recoveries) &&
    Number(run.recoveries) >= 0
  );
}
function isDetail(
  value: unknown,
): value is CaptureRun & { items: CaptureItem[] } {
  if (!isRun(value) || !Array.isArray(value.items) || value.items.length !== 4)
    return false;
  return value.items.every(
    (item, index) =>
      item &&
      item.code === codes[index] &&
      typeof item.name === "string" &&
      ["pending", "captured", "missing"].includes(item.state) &&
      typeof item.reason === "string" &&
      (item.state !== "captured"
        ? item.input === undefined
        : item.input &&
          typeof item.input.quote?.source === "string" &&
          isTime(item.input.quote.sourceTime) &&
          Number.isFinite(item.input.quote.Latest) &&
          item.input.quote.Latest > 0 &&
          isTime(item.input.requestedAt) &&
          isTime(item.input.capturedAt)),
  );
}
async function read(url: string, signal: AbortSignal) {
  const response = await fetch(url, {
    signal,
    credentials: "same-origin",
    cache: "no-store",
  });
  if (!response.ok)
    throw new Error(
      response.status === 401 || response.status === 403
        ? "登录已过期或无权访问，请重新通过 Access 登录。"
        : response.status === 404
          ? "该交易日尚无采集记录。"
          : "采集记录读取失败，请稍后重新读取。",
    );
  return response.json();
}

export function RotationCaptures() {
  const [runs, setRuns] = useState<CaptureRun[] | null>(null);
  const [selected, setSelected] = useState("");
  const [detail, setDetail] = useState<CaptureRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const update = () => {
      if (document.visibilityState === "visible")
        setRefresh((value) => value + 1);
    };
    const timer = setInterval(update, 30000);
    document.addEventListener("visibilitychange", update);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    const deadline = setTimeout(() => controller.abort(), 15000);
    setLoading(true);
    setError("");
    async function load() {
      try {
        const data = await read(
          "/api/rotation/reference-captures",
          controller.signal,
        );
        if (!Array.isArray(data.runs) || !data.runs.every(isRun))
          throw new Error("采集记录格式异常，请稍后重试。");
        if (controller.signal.aborted) return;
        setRuns(data.runs);
        const chosen = selected || data.runs[0]?.tradeDate;
        if (!chosen) return;
        if (!selected) {
          setSelected(chosen);
          return;
        }
        const result = await read(
          `/api/rotation/reference-captures/${chosen}`,
          controller.signal,
        );
        if (!isDetail(result.run) || result.run.tradeDate !== chosen)
          throw new Error("采集详情日期或内容异常，请稍后重试。");
        if (!controller.signal.aborted) setDetail(result.run);
      } catch (err) {
        if (active)
          setError(
            controller.signal.aborted
              ? "采集记录读取超时，请重新读取。"
              : err instanceof Error
                ? err.message
                : "采集记录读取失败。",
          );
      } finally {
        clearTimeout(deadline);
        if (active) setLoading(false);
      }
    }
    void load();
    return () => {
      active = false;
      clearTimeout(deadline);
      controller.abort();
    };
  }, [selected, refresh]);
  return (
    <ResearchTheme className="min-w-0">
      <section aria-label="14:45 参考采集">
        <Card>
          <CardHeader>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="flex min-w-0 flex-col gap-2">
                <CardTitle>
                  <h2>14:45 参考采集</h2>
                </CardTitle>
                <CardDescription>
                  查看最近 50
                  个交易日的采集记录。时间均为北京时间，刷新只读取已保存数据。
                </CardDescription>
              </div>
              <Button
                variant="outline"
                aria-disabled={loading}
                aria-busy={loading}
                onClick={() => {
                  if (!loading) setRefresh((value) => value + 1);
                }}
              >
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
                重新读取采集记录
              </Button>
            </div>
          </CardHeader>
          <CardContent className="flex min-w-0 flex-col gap-5">
            {error && (
              <Alert variant="destructive">
                <AlertTitle>记录未更新</AlertTitle>
                <AlertDescription>
                  {error}
                  {detail &&
                    ` 当前保留 ${date(detail.tradeDate)} 已读取记录，状态可能已变化。`}
                </AlertDescription>
              </Alert>
            )}
            {loading && (
              <p role="status" className="text-sm text-muted-foreground">
                正在读取采集记录…
              </p>
            )}
            {!runs && loading && (
              <Skeleton className="h-28 w-full" aria-label="正在读取采集记录" />
            )}
            {runs?.length === 0 && (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>暂无固定参考采集记录</EmptyTitle>
                  <EmptyDescription>
                    历史日线和旧盘中信号不等于已保存的 14:45 参考档案。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
            {runs && runs.length > 0 && (
              <Table aria-label="采集任务列表">
                <TableHeader>
                  <TableRow>
                    <TableHead>交易日</TableHead>
                    <TableHead>原始数据</TableHead>
                    <TableHead>采集阶段</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map((run) => (
                    <TableRow
                      key={run.tradeDate}
                      data-state={
                        selected === run.tradeDate ? "selected" : undefined
                      }
                    >
                      <TableCell>
                        <Button
                          variant="ghost"
                          aria-label={`查看 ${date(run.tradeDate)} 采集`}
                          aria-current={
                            selected === run.tradeDate ? "date" : undefined
                          }
                          onClick={() => {
                            if (selected !== run.tradeDate) {
                              setDetail(null);
                              setSelected(run.tradeDate);
                            }
                          }}
                        >
                          {date(run.tradeDate)}
                        </Button>
                      </TableCell>
                      <TableCell className="research-number">
                        {run.available}/4
                      </TableCell>
                      <TableCell className="whitespace-normal min-w-28">
                        {stateLabel(run)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {detail && (
              <div
                className="flex min-w-0 flex-col gap-4"
                aria-label="采集详情"
              >
                <div className="flex flex-col gap-2">
                  <h3 className="text-lg">{date(detail.tradeDate)} 采集详情</h3>
                  <p className="text-sm">
                    计划时点：{time(detail.targetAt)} · 原始数据到齐{" "}
                    {detail.available}/4
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="secondary" className="whitespace-normal">
                      {stateLabel(detail)}
                    </Badge>
                    <Badge variant="outline">
                      {stages[detail.stage] || "处理中"}
                    </Badge>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    {detail.message}
                    。目标时点不是全部标的的成交时间；来源时间与实际采集时间分别列出。
                  </p>
                </div>
                <div className="grid min-w-0 gap-4 md:grid-cols-2">
                  {detail.items?.map((item) => (
                    <Card
                      key={item.code}
                      className="min-w-0"
                      data-capture-code={item.code}
                    >
                      <CardHeader>
                        <CardTitle>
                          <h4>{item.name}</h4>
                        </CardTitle>
                        <CardDescription>
                          {item.code} ·{" "}
                          {item.state === "captured"
                            ? "原始数据已保存"
                            : item.state === "missing"
                              ? "未取得有效原始数据"
                              : "等待采集"}
                        </CardDescription>
                      </CardHeader>
                      <CardContent>
                        {item.input ? (
                          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
                            <dt>时点价格</dt>
                            <dd className="research-number">
                              {item.input.quote.Latest.toLocaleString("zh-CN", {
                                minimumFractionDigits: 3,
                                maximumFractionDigits: 6,
                              })}
                            </dd>
                            <dt>行情来源</dt>
                            <dd className="break-words">
                              {item.input.quote.source}
                            </dd>
                            <dt>来源时间</dt>
                            <dd>{time(item.input.quote.sourceTime)}</dd>
                            <dt>请求时间</dt>
                            <dd>{time(item.input.requestedAt)}</dd>
                            <dt>采集时间</dt>
                            <dd>{time(item.input.capturedAt)}</dd>
                          </dl>
                        ) : (
                          <p className="text-sm text-muted-foreground">
                            {item.reason || "尚未取得原时点数据。"}
                          </p>
                        )}
                      </CardContent>
                    </Card>
                  ))}
                </div>
                <p className="text-sm text-muted-foreground">
                  记录更新：{time(detail.updatedAt)}
                  {detail.recoveries > 0 &&
                    ` · 已恢复 ${detail.recoveries} 次，沿用原目标`}
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      </section>
    </ResearchTheme>
  );
}
