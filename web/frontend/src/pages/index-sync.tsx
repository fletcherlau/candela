import { useEffect, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

type RunEvent = {
  kind: string;
  at: string;
  checkpoint: string;
  message: string;
};
type Run = {
  events?: RunEvent[];
  id: string;
  mode: "backfill" | "incremental";
  state: string;
  stage: string;
  startDate: string;
  endDate: string;
  effectiveStart: string;
  checkpoint: string;
  processedRows: number;
  completedSegments: number;
  totalSegments: number;
  historyEvidence: string;
  errorCode: string;
  message: string;
  createdAt: string;
};
const states: Record<string, string> = {
  queued: "排队中",
  running: "执行中",
  cancelling: "取消中",
  cancelled: "已取消",
  succeeded: "已完成",
  failed: "失败",
};
const stages: Record<string, string> = {
  queued: "等待执行",
  recovering: "等待恢复原任务",
  discovering: "确认历史覆盖范围",
  fetching: "获取并保存日线",
  finished: "执行结束",
};
const date = (s: string) =>
  /^\d{8}$/.test(s) ? `${s.slice(0, 4)}.${s.slice(4, 6)}.${s.slice(6)}` : "—";
const modeName = (mode: string) =>
  mode === "backfill" ? "历史回填" : "增量同步";
async function read(response: Response) {
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? "登录已过期，请刷新页面重新登录。"
        : "任务服务暂时不可用，请刷新任务列表后重试。",
    );
  return response.json();
}
function isRun(value: unknown): value is Run {
  if (!value || typeof value !== "object") return false;
  const run = value as Record<string, unknown>;
  return (
    typeof run.id === "string" &&
    /^[a-f0-9]{32}$/.test(run.id) &&
    (run.mode === "backfill" || run.mode === "incremental") &&
    typeof run.state === "string" &&
    Object.hasOwn(states, run.state) &&
    (run.events === undefined ||
      (Array.isArray(run.events) &&
        run.events.every(
          (event) =>
            event &&
            typeof event === "object" &&
            ["kind", "at", "checkpoint", "message"].every(
              (key) => typeof event[key] === "string",
            ),
        ))) &&
    [
      "stage",
      "startDate",
      "endDate",
      "effectiveStart",
      "checkpoint",
      "historyEvidence",
      "errorCode",
      "message",
      "createdAt",
    ].every((key) => typeof run[key] === "string") &&
    ["processedRows", "completedSegments", "totalSegments"].every(
      (key) =>
        typeof run[key] === "number" &&
        Number.isSafeInteger(run[key]) &&
        Number(run[key]) >= 0,
    )
  );
}

export function IndexSync({ onCompleted }: { onCompleted: () => void }) {
  const [runs, setRuns] = useState<Run[]>([]);
  const [selected, setSelected] = useState(
    () => new URLSearchParams(location.search).get("run") || "",
  );
  const [detail, setDetail] = useState<Run | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const submitLock = useRef(false);
  const callback = useRef(onCompleted);
  callback.current = onCompleted;
  const completed = useRef(new Set<string>());

  function select(id: string) {
    setSelected(id);
    setDetail(null);
    const url = new URL(location.href);
    url.searchParams.set("run", id);
    history.replaceState(null, "", url);
  }
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const data = await read(
          await fetch("/api/sync-runs", {
            signal: controller.signal,
            cache: "no-store",
          }),
        );
        if (!Array.isArray(data.runs) || !data.runs.every(isRun))
          throw new Error("任务列表格式异常，请稍后重试。");
        if (controller.signal.aborted) return;
        setRuns(data.runs);
        const id = selected || data.runs[0]?.id;
        if (id) {
          if (!/^[a-f0-9]{32}$/.test(id))
            throw new Error("任务编号无效，请从列表重新选择。");
          const result = await read(
            await fetch(`/api/sync-runs/${id}`, {
              signal: controller.signal,
              cache: "no-store",
            }),
          );
          if (!isRun(result.run))
            throw new Error("任务详情格式异常，请稍后重试。");
          if (controller.signal.aborted) return;
          setDetail(result.run);
          if (result.run.state === "succeeded" && !completed.current.has(id)) {
            completed.current.add(id);
            callback.current();
          }
        } else setDetail(null);
        setError("");
      } catch (err) {
        if (!controller.signal.aborted)
          setError(err instanceof Error ? err.message : "任务查询失败。");
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          timer = setTimeout(load, 3000);
        }
      }
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [selected, refresh]);

  async function submit(mode: "backfill" | "incremental") {
    if (submitLock.current) return;
    submitLock.current = true;
    setSubmitting(true);
    setError("");
    setNotice("");
    try {
      const session = await read(
        await fetch("/api/session", { cache: "no-store" }),
      );
      const data = await read(
        await fetch("/api/sync-runs", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": session.csrfToken,
          },
          body: JSON.stringify({ mode }),
        }),
      );
      if (!isRun(data.run))
        throw new Error("提交结果不确定，请先刷新任务列表。");
      select(data.run.id);
      setDetail(data.run);
      setNotice(
        data.deduplicated
          ? "已有相同任务，已为你打开原任务。"
          : "任务已保存，后台将按顺序执行。可以离开或刷新页面。",
      );
    } catch (err) {
      setError(
        err instanceof Error
          ? `${err.message} 提交可能已被接受，请先查看列表。`
          : "提交结果不确定，请先查看列表。",
      );
    } finally {
      submitLock.current = false;
      setSubmitting(false);
      setRefresh((v) => v + 1);
    }
  }

  async function cancelRun() {
    if (
      !detail ||
      !["queued", "running"].includes(detail.state) ||
      submitLock.current
    )
      return;
    const id = detail.id;
    submitLock.current = true;
    setSubmitting(true);
    setError("");
    setNotice("");
    try {
      const session = await read(
        await fetch("/api/session", { cache: "no-store" }),
      );
      const data = await read(
        await fetch(`/api/sync-runs/${id}/cancel`, {
          method: "POST",
          headers: { "X-CSRF-Token": session.csrfToken },
        }),
      );
      if (!isRun(data.run))
        throw new Error("取消结果不确定，请刷新任务列表确认。");
      select(id);
      setDetail(data.run);
      setNotice(
        data.run.state === "cancelling"
          ? "取消请求已保存，正在确认停止。已提交数据保留。"
          : data.run.state === "cancelled"
            ? "任务已取消，已提交数据保留。"
            : "任务已结束，请以当前任务状态为准。",
      );
    } catch (err) {
      setError(
        err instanceof Error
          ? `${err.message} 取消可能已被接受，请先刷新任务列表确认。`
          : "取消结果不确定，请刷新任务列表确认。",
      );
    } finally {
      submitLock.current = false;
      setSubmitting(false);
      setRefresh((v) => v + 1);
    }
  }

  return (
    <Card className="min-w-0 shadow-none" aria-label="中证全指同步">
      <CardHeader>
        <CardTitle>中证全指同步</CardTitle>
        <CardDescription>
          000985.CSI ·
          历史回填获取源端全部可用日线；增量同步从最后一个已存交易日开始。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap gap-3">
          <Button disabled={submitting} onClick={() => void submit("backfill")}>
            历史回填
          </Button>
          <Button
            variant="outline"
            disabled={submitting}
            onClick={() => void submit("incremental")}
          >
            增量同步
          </Button>
          <Button
            variant="ghost"
            disabled={loading}
            onClick={() => {
              setLoading(true);
              setRefresh((v) => v + 1);
            }}
          >
            刷新任务列表
          </Button>
        </div>
        <p className="text-xs leading-6 text-muted-foreground">
          截止日在提交时固定：北京时间 18:00
          前取前一日，之后取当日。相同任务自动合并，其他任务排队执行。
        </p>
        {notice && (
          <p role="status" className="text-sm">
            {notice}
          </p>
        )}
        {error && (
          <Alert variant="destructive">
            <AlertTitle>任务查询或提交失败</AlertTitle>
            <AlertDescription>
              {error} 下方可能为上次查询结果。
            </AlertDescription>
          </Alert>
        )}
        <div className="grid min-w-0 gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
          <div
            className="max-h-96 space-y-2 overflow-y-auto"
            aria-label="最近同步任务"
          >
            {!runs.length && (
              <p className="text-sm text-muted-foreground">
                {loading ? "正在查询任务…" : "暂无同步任务"}
              </p>
            )}
            {runs.map((run) => (
              <button
                key={run.id}
                onClick={() => select(run.id)}
                aria-pressed={detail?.id === run.id}
                className={`w-full rounded-lg border p-3 text-left text-sm focus-visible:outline-2 focus-visible:outline-offset-2 ${detail?.id === run.id ? "border-primary bg-secondary" : "hover:bg-muted"}`}
              >
                <span className="flex justify-between gap-2">
                  <span>{modeName(run.mode)}</span>
                  <span>{states[run.state] || run.state}</span>
                </span>
                <span className="mt-2 block text-xs text-muted-foreground">
                  截止 {date(run.endDate)} · {run.id.slice(0, 8)}
                </span>
              </button>
            ))}
          </div>
          {detail && (
            <section
              className="min-w-0 space-y-4 rounded-lg border p-5"
              aria-label="同步任务详情"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="font-medium">{modeName(detail.mode)}</h2>
                <Badge
                  variant={
                    detail.state === "failed" ? "destructive" : "secondary"
                  }
                >
                  {states[detail.state] || detail.state}
                </Badge>
                <Button
                  variant="outline"
                  aria-disabled={
                    submitting || !["queued", "running"].includes(detail.state)
                  }
                  aria-busy={submitting || detail.state === "cancelling"}
                  onClick={() => void cancelRun()}
                >
                  取消任务
                </Button>
              </div>
              <p className="break-all text-xs text-muted-foreground">
                任务编号 {detail.id}
              </p>
              <p className="text-xs text-muted-foreground">
                提交于{" "}
                {new Date(detail.createdAt).toLocaleString("zh-CN", {
                  timeZone: "Asia/Shanghai",
                  hour12: false,
                })}{" "}
                · 北京时间
              </p>
              <p className="text-sm">{stages[detail.stage] || detail.stage}</p>
              {detail.totalSegments > 0 && (
                <>
                  <progress
                    className="sync-progress h-2 w-full"
                    aria-label="已完成分段"
                    value={detail.completedSegments}
                    max={detail.totalSegments}
                  />
                  <p className="text-xs text-muted-foreground">
                    已完成 {detail.completedSegments} / {detail.totalSegments}{" "}
                    个分段
                  </p>
                </>
              )}
              <dl className="grid gap-4 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">固定范围</dt>
                  <dd className="mt-1">
                    {detail.startDate === "00010101"
                      ? "全部可用历史"
                      : date(detail.startDate)}{" "}
                    → {date(detail.endDate)}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">实际起点</dt>
                  <dd className="mt-1">{date(detail.effectiveStart)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">已处理记录</dt>
                  <dd className="mt-1 tabular-nums">
                    {detail.processedRows.toLocaleString("zh-CN")} 条
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">已提交至</dt>
                  <dd className="mt-1">{date(detail.checkpoint)}</dd>
                </div>
              </dl>
              <p className="text-xs leading-6 text-muted-foreground">
                已处理记录含重复写入的记录，不代表新增或变化的行数。
              </p>
              {detail.historyEvidence && (
                <p className="text-xs leading-6 text-muted-foreground">
                  {detail.historyEvidence}
                </p>
              )}
              {!!detail.events?.length && (
                <div className="flex flex-col gap-2">
                  <h3 className="text-sm font-medium">执行记录</h3>
                  <ul aria-label="执行记录" className="flex flex-col gap-3">
                    {detail.events.map((event, index) => (
                      <li key={index} className="text-sm">
                        <time
                          dateTime={event.at}
                          className="block text-xs text-muted-foreground"
                        >
                          {new Date(event.at).toLocaleString("zh-CN", {
                            timeZone: "Asia/Shanghai",
                            hour12: false,
                          })}{" "}
                          · 北京时间
                        </time>
                        <p>{event.message}</p>
                        <p className="text-xs text-muted-foreground">
                          {event.checkpoint
                            ? `当时已提交至 ${date(event.checkpoint)}`
                            : "当时尚未提交分段"}
                        </p>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {detail.message && (
                <p
                  className={`text-sm ${detail.state === "failed" ? "text-destructive" : ""}`}
                >
                  {detail.message}
                  {detail.errorCode ? `（${detail.errorCode}）` : ""}
                </p>
              )}
            </section>
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          显示最近 50 个任务。刷新不影响执行；任务失败后，已提交数据保留。
        </p>
      </CardContent>
    </Card>
  );
}
