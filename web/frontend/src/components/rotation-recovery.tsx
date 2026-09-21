import { useEffect, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

type RecoveryRun = {
  id: string;
  parentId: string;
  originId: string;
  basis: "reference_1445" | "close";
  tradeDate: string;
  targetAt: string;
  state: string;
  stage: string;
  message: string;
  syncBatchId: string;
  updatedAt: string;
  recoveries: number;
};
const labels: Record<string, string> = {
  accepted: "请求已接受",
  recovering: "等待继续原任务",
  syncing: "同步未完成步骤",
  sync_complete: "同步完成",
  computing: "计算中",
  published: "已发布",
  calculation_failed: "计算或发布失败",
  synchronization_failed: "同步未完成",
  inputs_missing: "原始输入缺失",
};
const id = (value: unknown): value is string =>
  typeof value === "string" && /^[a-f0-9]{32}$/.test(value);
function isRun(value: unknown): value is RecoveryRun {
  if (!value || typeof value !== "object") return false;
  const r = value as Record<string, unknown>;
  return (
    id(r.id) &&
    [
      "parentId",
      "originId",
      "tradeDate",
      "targetAt",
      "message",
      "syncBatchId",
      "updatedAt",
    ].every((k) => typeof r[k] === "string") &&
    (r.parentId === "" || id(r.parentId)) &&
    (r.syncBatchId === "" || id(r.syncBatchId)) &&
    ["reference_1445", "close"].includes(String(r.basis)) &&
    ["queued", "running", "succeeded", "failed", "unavailable"].includes(
      String(r.state),
    ) &&
    Object.hasOwn(labels, String(r.stage)) &&
    /^\d{8}$/.test(String(r.tradeDate)) &&
    Number.isFinite(Date.parse(String(r.targetAt))) &&
    Number.isFinite(Date.parse(String(r.updatedAt))) &&
    Number.isSafeInteger(r.recoveries) &&
    Number(r.recoveries) >= 0
  );
}
const date = (value: string) =>
  `${value.slice(0, 4)}-${value.slice(4, 6)}-${value.slice(6)}`;
async function read(path: string, options: RequestInit = {}) {
  const res = await fetch(path, {
    cache: "no-store",
    credentials: "same-origin",
    ...options,
    signal: options.signal
      ? AbortSignal.any([options.signal, AbortSignal.timeout(15000)])
      : AbortSignal.timeout(15000),
  });
  if (!res.ok)
    throw new Error(
      res.status === 401 || res.status === 403
        ? "登录或写入验证已失效，请重新登录后读取记录。"
        : res.status === 409
          ? "原任务仍在执行、已发布或已取消，请重新读取恢复记录。"
          : "恢复服务暂不可用，请重新读取记录。",
    );
  return res.json();
}
const errorText = (error: unknown) =>
  error instanceof Error
    ? error.name === "TimeoutError"
      ? "读取超时，请重新读取恢复记录。"
      : error.message
    : "恢复记录读取失败。";

// This component submits a saved origin/attempt identity only. All financial
// calculations, frozen ranges and recovery eligibility are enforced upstream.
export function RotationRecovery({
  basis,
  origin,
  tradeDate,
  eligible,
  reason,
}: {
  basis: "reference_1445" | "close";
  origin: string;
  tradeDate: string;
  eligible: boolean;
  reason: string;
}) {
  const [runs, setRuns] = useState<RecoveryRun[]>([]);
  const [current, setCurrent] = useState<RecoveryRun | null>(null);
  const [published, setPublished] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [readError, setReadError] = useState("");
  const [operationError, setOperationError] = useState("");
  const [revision, setRevision] = useState(0);
  const reading = useRef<AbortController | null>(null);
  const writing = useRef<AbortController | null>(null);
  const accepted = useRef("");
  useEffect(() => () => writing.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    reading.current = controller;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      let pending = false;
      try {
        const data = await read("/api/rotation/recoveries", {
          signal: controller.signal,
        });
        if (!Array.isArray(data.runs) || !data.runs.every(isRun))
          throw new Error("恢复记录格式异常。");
        const records: RecoveryRun[] = data.runs.filter(
          (run: RecoveryRun) => run.originId === origin && run.basis === basis,
        );
        let latest = records[0] || null;
        // A response accepted before a list refresh remains queryable even if
        // it has moved beyond the bounded recent-record list.
        if (!latest && accepted.current) {
          const result = await read(
            `/api/rotation/recoveries/${accepted.current}`,
            { signal: controller.signal },
          );
          if (
            !isRun(result.run) ||
            result.run.originId !== origin ||
            result.run.basis !== basis
          )
            throw new Error("恢复记录标识不匹配。");
          latest = result.run;
        }
        const daily = await read(`/api/rotation/daily?tradeDate=${tradeDate}`, {
          signal: controller.signal,
        });
        if (
          daily.tradeDate !== tradeDate ||
          typeof daily.closeState?.status !== "string" ||
          typeof daily.referenceState?.status !== "string"
        )
          throw new Error("发布状态日期或内容异常。");
        if (controller.signal.aborted) return;
        setRuns(records);
        setCurrent(latest);
        setPublished(
          basis === "close"
            ? daily.closeState.status === "ready" && !!daily.close
            : !!daily.reference,
        );
        setLoaded(true);
        setReadError("");
        pending = latest?.state === "queued" || latest?.state === "running";
      } catch (error) {
        if (!controller.signal.aborted) {
          setReadError(errorText(error));
          setLoaded(false);
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          timer = setTimeout(() => void poll(), pending ? 1000 : 15000);
        }
      }
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [basis, origin, tradeDate, revision]);
  async function recover() {
    if (
      writing.current ||
      !loaded ||
      !eligible ||
      published ||
      (current && current.state !== "failed")
    )
      return;
    const controller = new AbortController();
    writing.current = controller;
    reading.current?.abort();
    setBusy(true);
    setOperationError("");
    try {
      const session = await read("/api/session", { signal: controller.signal });
      if (
        typeof session.csrfToken !== "string" ||
        session.csrfToken.length < 32
      )
        throw new Error("写入验证未取得，请重新登录。");
      const path = current
        ? `/api/rotation/recoveries/${current.id}/retry`
        : `/api/rotation/${basis === "close" ? "close-syncs" : "reference-captures"}/${origin}/retry`;
      const result = await read(path, {
        method: "POST",
        headers: { "X-CSRF-Token": session.csrfToken },
        signal: controller.signal,
      });
      if (
        !isRun(result.run) ||
        result.run.originId !== origin ||
        result.run.basis !== basis ||
        result.run.tradeDate !== tradeDate
      )
        throw new Error("恢复接受结果不匹配。");
      if (!controller.signal.aborted) {
        accepted.current = result.run.id;
        setCurrent(result.run);
      }
    } catch (error) {
      if (!controller.signal.aborted)
        setOperationError(
          `${errorText(error)} 提交结果不确定时，先重新读取记录；关闭页面不会取消已接受的任务。`,
        );
    } finally {
      writing.current = null;
      if (!controller.signal.aborted) {
        setBusy(false);
        setRevision((v) => v + 1);
      }
    }
  }
  const title = basis === "close" ? "收盘结果恢复" : "14:45 参考恢复";
  const canRetry =
    eligible && !published && (!current || current.state === "failed");
  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-3">
      <h4 className="text-base">{title}</h4>
      <p className="text-sm text-muted-foreground">
        原交易日 {date(tradeDate)} ·{" "}
        {basis === "close"
          ? "仅续传原批次未完成步骤，完成后整组计算和发布。"
          : "使用保存的 14:45 输入与参数恢复计算，不请求当前价格。"}
      </p>
      {loading && !loaded && (
        <Skeleton className="h-16 w-full" aria-label="正在读取恢复记录" />
      )}
      {(readError || operationError) && (
        <Alert variant="destructive">
          <AlertTitle>恢复状态需核对</AlertTitle>
          <AlertDescription>
            {operationError || readError}{" "}
            {current && "下方保留上次读取记录，状态可能已变化。"}
          </AlertDescription>
        </Alert>
      )}
      {!eligible && !published && <p className="text-sm">{reason}</p>}
      {published && !current && (
        <p role="status" className="text-sm">
          {basis === "close"
            ? "四标的收盘数据已发布"
            : "固定参考已发布，原始输入保持不变"}
        </p>
      )}
      {current && (
        <div className="flex min-w-0 flex-col gap-2 text-sm">
          <div>
            <Badge
              variant={current.state === "failed" ? "destructive" : "secondary"}
            >
              {labels[current.stage]}
            </Badge>
          </div>
          <p role="status">{current.message}</p>
          <p>
            原目标时点：
            {new Date(current.targetAt).toLocaleString("zh-CN", {
              timeZone: "Asia/Shanghai",
              hour12: false,
            })}
            （北京时间）
          </p>
          <p className="break-all text-xs text-muted-foreground">
            恢复任务 {current.id}
            {current.parentId && ` · 父恢复任务 ${current.parentId}`}
          </p>
          {current.syncBatchId && (
            <Button asChild variant="link" className="self-start">
              <a href={`/admin/data?etfBatch=${current.syncBatchId}`}>
                查看恢复同步批次
              </a>
            </Button>
          )}
          {current.recoveries > 0 && (
            <p>中断后已继续 {current.recoveries} 次，目标保持不变。</p>
          )}
          {runs.length > 1 && (
            <details>
              <summary className="min-h-11 cursor-pointer py-3">
                查看此前恢复记录
              </summary>
              <ul className="flex flex-col gap-3">
                {runs.slice(1).map((run) => (
                  <li key={run.id} className="break-all">
                    {labels[run.stage]} · {run.message}
                    <br />
                    <span className="text-xs text-muted-foreground">
                      {run.id}
                    </span>
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
      <div className="flex flex-wrap gap-3">
        {canRetry && (
          <Button disabled={busy || !loaded} onClick={() => void recover()}>
            {busy
              ? "正在提交恢复…"
              : basis === "close"
                ? "恢复收盘同步与发布"
                : "恢复参考计算"}
          </Button>
        )}
        <Button
          variant="outline"
          aria-disabled={busy || loading}
          onClick={() => {
            if (!busy && !loading) {
              setLoading(true);
              setOperationError("");
              setRevision((v) => v + 1);
            }
          }}
        >
          重新读取恢复记录
        </Button>
        {published && (
          <Button asChild variant="link">
            <a href={`/strategies/four-etf-rotation?tradeDate=${tradeDate}`}>
              查看该日数据
            </a>
          </Button>
        )}
      </div>
    </section>
  );
}
