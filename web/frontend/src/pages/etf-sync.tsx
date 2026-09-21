import { RotationRecovery } from "@/components/rotation-recovery";
import { ResearchTheme } from "@/components/research-theme";
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
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";

const states: Record<string, string> = {
  queued: "排队中",
  running: "处理中",
  cancelling: "取消中",
  succeeded: "全部成功",
  failed: "失败",
  partial: "部分失败",
  cancelled: "已取消",
};
const stages: Record<string, string> = {
  queued: "等待执行",
  recovering: "等待恢复",
  daily: "获取日线",
  adj: "获取复权因子",
  finished: "执行结束",
};
const validID = (s: string) => /^[a-f0-9]{32}$/.test(s);
const date = (s: string) =>
  /^\d{8}$/.test(s) ? `${s.slice(0, 4)}.${s.slice(4, 6)}.${s.slice(6)}` : "—";
const terminal = (state: string) =>
  ["succeeded", "failed", "partial", "cancelled"].includes(state);
type Item = {
  id: string;
  code: string;
  state: string;
  stage: string;
  dailyStart: string;
  adjStart: string;
  dailyCheckpoint: string;
  adjCheckpoint: string;
  dailyRows: number;
  adjRows: number;
  completedSegments: number;
  totalSegments: number;
  errorCode: string;
  message: string;
  parentRun: string;
};
type Batch = {
  events?: {
    code: string;
    kind: string;
    at: string;
    checkpoint: string;
    message: string;
  }[];
  id: string;
  parentId: string;
  endDate: string;
  createdAt: string;
  state: string;
  total: number;
  success: number;
  failed: number;
  cancelled: number;
  items: Item[] | null;
};
function isBatch(value: unknown, detail = false): value is Batch {
  if (!value || typeof value !== "object") return false;
  const b = value as Record<string, unknown>;
  if (
    typeof b.id !== "string" ||
    !validID(b.id) ||
    typeof b.state !== "string" ||
    !Object.hasOwn(states, b.state)
  )
    return false;
  if (
    !["parentId", "endDate", "createdAt"].every(
      (k) => typeof b[k] === "string",
    ) ||
    !["total", "success", "failed", "cancelled"].every(
      (k) => Number.isSafeInteger(b[k]) && Number(b[k]) >= 0,
    )
  )
    return false;
  if (
    b.events !== undefined &&
    (!Array.isArray(b.events) ||
      !b.events.every(
        (event: Record<string, unknown>) =>
          event &&
          typeof event === "object" &&
          ["code", "kind", "at", "checkpoint", "message"].every(
            (k) => typeof event[k] === "string",
          ),
      ))
  )
    return false;
  return (
    !detail ||
    (Array.isArray(b.items) &&
      b.items.length === b.total &&
      b.items.every(
        (item: Record<string, unknown>) =>
          item &&
          typeof item === "object" &&
          [
            "id",
            "code",
            "state",
            "stage",
            "dailyStart",
            "adjStart",
            "dailyCheckpoint",
            "adjCheckpoint",
            "errorCode",
            "message",
            "parentRun",
          ].every((k) => typeof item[k] === "string") &&
          validID(String(item.id)) &&
          Object.hasOwn(states, String(item.state)) &&
          ["dailyRows", "adjRows", "completedSegments", "totalSegments"].every(
            (k) => Number.isSafeInteger(item[k]) && Number(item[k]) >= 0,
          ),
      ))
  );
}
async function read(path: string, options: RequestInit = {}) {
  const response = await fetch(path, {
    cache: "no-store",
    ...options,
    signal: options.signal
      ? AbortSignal.any([options.signal, AbortSignal.timeout(15000)])
      : AbortSignal.timeout(15000),
  });
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? "登录已过期或无权访问，请刷新页面重新登录。"
        : response.status === 409
          ? "批次尚未结束或没有可重试的失败对象，请刷新批次。"
          : response.status === 404
            ? "批次不存在。"
            : "批次服务暂时不可用，请重新读取。",
    );
  return response.json();
}
const message = (err: unknown) =>
  err instanceof Error
    ? err.name === "TimeoutError"
      ? "读取超时，请重新读取。"
      : err.message
    : "批次查询失败。";

export function ETFSync({ onCompleted }: { onCompleted: () => void }) {
  const [batches, setBatches] = useState<Batch[]>([]);
  const [selected, setSelected] = useState(() => {
    const id = new URLSearchParams(location.search).get("etfBatch") || "";
    return validID(id) ? id : "";
  });
  const [detail, setDetail] = useState<Batch | null>(null);
  const [codes, setCodes] = useState("");
  const [codesError, setCodesError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [operationError, setOperationError] = useState("");
  const [notice, setNotice] = useState("");
  const [refresh, setRefresh] = useState(0);
  const writing = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const callback = useRef(onCompleted);
  callback.current = onCompleted;
  const completed = useRef(new Set<string>());
  function select(id: string) {
    controller.current?.abort();
    setSelected(id);
    setRefresh((v) => v + 1);
    setDetail(null);
    setLoading(true);
    setError("");
    const url = new URL(location.href);
    url.searchParams.set("etfBatch", id);
    history.replaceState(null, "", url);
  }
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const abort = new AbortController();
    controller.current = abort;
    const poll = async () => {
      try {
        const result = await read("/api/etf-syncs", { signal: abort.signal });
        if (
          !Array.isArray(result.batches) ||
          !result.batches.every((b: unknown) => isBatch(b))
        )
          throw new Error("批次服务返回异常。");
        if (!alive || abort.signal.aborted) return;
        setBatches(result.batches);
        if (!selected && result.batches.length) {
          select(result.batches[0].id);
          return;
        }
        if (selected) {
          const response = await read(`/api/etf-syncs/${selected}`, {
            signal: abort.signal,
          });
          if (!isBatch(response.batch, true) || response.batch.id !== selected)
            throw new Error("批次详情返回异常。");
          if (!alive || abort.signal.aborted) return;
          setDetail(response.batch);
          if (
            terminal(response.batch.state) &&
            !completed.current.has(selected)
          ) {
            completed.current.add(selected);
            callback.current();
          }
        }
        setError("");
      } catch (err) {
        if (alive && !abort.signal.aborted) setError(message(err));
      } finally {
        if (alive && !abort.signal.aborted) {
          setLoading(false);
          timer = setTimeout(poll, 3000);
        }
      }
    };
    void poll();
    return () => {
      alive = false;
      abort.abort();
      clearTimeout(timer);
    };
  }, [selected, refresh]);
  async function act(intent: "submit" | "selected" | "retry" | "cancel") {
    if (writing.current) return;
    if ((intent === "retry" || intent === "cancel") && !detail) return;
    const input =
      intent === "selected"
        ? [
            ...new Set(
              codes
                .trim()
                .toUpperCase()
                .split(/[\s,，]+/)
                .filter(Boolean),
            ),
          ]
        : [];
    if (
      intent === "selected" &&
      (!input.length ||
        input.length > 500 ||
        input.some((code) => !/^\d{6}\.(SH|SZ)$/.test(code)))
    ) {
      setCodesError(
        "请输入 1 至 500 个 ETF 代码，例如 510880.SH；多个代码用逗号或空格分隔。",
      );
      return;
    }
    writing.current = true;
    setBusy(true);
    setNotice("");
    setOperationError("");
    setError("");
    controller.current?.abort();
    try {
      const session = await read("/api/session");
      if (typeof session.csrfToken !== "string")
        throw new Error("会话验证失败，请刷新页面。");
      const creating = intent === "submit" || intent === "selected";
      const response = await read(
        creating ? "/api/etf-syncs" : `/api/etf-syncs/${detail!.id}/${intent}`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": session.csrfToken,
          },
          ...(creating ? { body: JSON.stringify({ codes: input }) } : {}),
        },
      );
      if (!isBatch(response.batch, true)) throw new Error("批次响应异常。");
      select(response.batch.id);
      setDetail(response.batch);
      setNotice(
        intent === "cancel"
          ? "取消请求已保存。已成功结果保留，请以批次状态为准。"
          : response.deduplicated
            ? "已打开已有批次，未重复提交。"
            : "批次已保存，后台继续执行；离开或刷新页面不会取消任务。",
      );
    } catch (err) {
      setOperationError(
        `${message(err)} 操作可能已被接受，请先刷新批次列表确认。`,
      );
    } finally {
      writing.current = false;
      setBusy(false);
      setRefresh((v) => v + 1);
    }
  }
  return (
    <ResearchTheme className="min-w-0">
      <Card role="region" aria-label="ETF 批量同步" className="min-w-0">
        <CardHeader>
          <CardTitle>
            <h2>ETF 批量同步</h2>
          </CardTitle>
          <CardDescription>
            按启用名单或指定代码追补日线与复权因子，逐只查看结果并恢复失败步骤。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex min-w-0 flex-col gap-5">
          <div className="flex flex-wrap gap-3">
            <Button disabled={busy} onClick={() => void act("submit")}>
              同步全部启用 ETF
            </Button>
            <Button
              variant="outline"
              aria-disabled={loading || busy}
              onClick={() => {
                if (!busy && !loading) {
                  setLoading(true);
                  setRefresh((v) => v + 1);
                }
              }}
            >
              刷新 ETF 批次
            </Button>
          </div>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void act("selected");
            }}
          >
            <FieldGroup>
              <Field data-invalid={!!codesError}>
                <FieldLabel htmlFor="etf-sync-codes">指定 ETF 代码</FieldLabel>
                <Input
                  id="etf-sync-codes"
                  value={codes}
                  onChange={(event) => {
                    setCodes(event.target.value);
                    setCodesError("");
                  }}
                  placeholder="510880.SH, 518880.SH"
                  autoComplete="off"
                  disabled={busy}
                  aria-invalid={!!codesError}
                  aria-describedby={
                    codesError
                      ? "etf-code-help etf-code-error"
                      : "etf-code-help"
                  }
                />
                <FieldDescription id="etf-code-help">
                  多个代码用逗号或空格分隔。指定代码仅用于本次同步，不修改启用名单。
                </FieldDescription>
                {codesError && (
                  <FieldError id="etf-code-error">{codesError}</FieldError>
                )}
              </Field>
              <Field>
                <Button
                  type="submit"
                  variant="outline"
                  disabled={busy || !codes.trim()}
                >
                  同步指定 ETF
                </Button>
              </Field>
            </FieldGroup>
          </form>
          <p className="text-xs leading-6 text-muted-foreground">
            对象与截止日由后端在提交时固定，截止日为提交当日（北京时间）。日线与因子分别续传，相同未结束批次自动合并。
          </p>
          {notice && (
            <p role="status" className="text-sm">
              {notice}
            </p>
          )}
          {(error || operationError) && (
            <Alert variant="destructive">
              <AlertTitle>ETF 批次读取或操作失败</AlertTitle>
              <AlertDescription>
                {operationError || error}{" "}
                {detail
                  ? `下方保留批次 ${detail.id.slice(0, 8)} 上次读取结果。`
                  : ""}
              </AlertDescription>
            </Alert>
          )}
          {loading && !detail && (
            <Skeleton className="h-20 w-full" aria-label="正在读取 ETF 批次" />
          )}
          {!loading && !batches.length && !error && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>暂无 ETF 同步批次</EmptyTitle>
              </EmptyHeader>
            </Empty>
          )}
          <div className="grid min-w-0 gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
            <div
              className="flex max-h-96 flex-col gap-2 overflow-y-auto"
              aria-label="最近 ETF 批次"
            >
              {batches.map((batch) => (
                <Button
                  key={batch.id}
                  variant={selected === batch.id ? "secondary" : "outline"}
                  className="h-auto min-h-11 flex-col items-start gap-1 whitespace-normal p-3 text-left"
                  aria-pressed={selected === batch.id}
                  aria-label={`查看 ETF 批次 ${batch.id.slice(0, 8)}`}
                  disabled={busy}
                  onClick={() => select(batch.id)}
                >
                  <span>
                    {states[batch.state]} · {batch.total} 只
                  </span>
                  <span>截止 {date(batch.endDate)}</span>
                  <span className="break-all">
                    {batch.id.slice(0, 8)}
                    {batch.parentId ? " · 失败重试" : ""}
                  </span>
                </Button>
              ))}
            </div>
            {detail && (
              <section
                aria-label="ETF 批次详情"
                className="flex min-w-0 flex-col gap-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h3 className="font-medium">批次详情</h3>
                  <Badge variant={detail.failed ? "destructive" : "secondary"}>
                    {states[detail.state]}
                  </Badge>
                </div>
                <p className="break-all text-xs text-muted-foreground">
                  批次 {detail.id}
                </p>
                <p className="text-sm">
                  截止 {date(detail.endDate)} · 成功 {detail.success} /{" "}
                  {detail.total} · 失败 {detail.failed} · 取消{" "}
                  {detail.cancelled}
                </p>
                <p className="text-xs text-muted-foreground">
                  提交于{" "}
                  {new Date(detail.createdAt).toLocaleString("zh-CN", {
                    timeZone: "Asia/Shanghai",
                    hour12: false,
                  })}{" "}
                  · 北京时间
                </p>
                <div className="flex flex-wrap gap-3">
                  {detail.parentId && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => select(detail.parentId)}
                    >
                      查看原批次
                    </Button>
                  )}
                  {terminal(detail.state) && detail.failed > 0 && (
                    <Button disabled={busy} onClick={() => void act("retry")}>
                      仅重试失败 {detail.failed} 只
                    </Button>
                  )}
                  {!terminal(detail.state) && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => void act("cancel")}
                    >
                      取消未完成对象
                    </Button>
                  )}
                </div>
                {detail.items?.some((item) =>
                  ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"].includes(
                    item.code,
                  ),
                ) && (
                  <RotationRecovery
                    key={detail.id}
                    basis="close"
                    origin={detail.id}
                    tradeDate={detail.endDate}
                    eligible={["failed", "partial", "succeeded"].includes(
                      detail.state,
                    )}
                    reason={
                      detail.state === "cancelled"
                        ? "原批次已取消，不参与失败恢复。"
                        : "原批次仍在执行，请等待同步结果。"
                    }
                  />
                )}
                <div
                  className="grid max-h-[36rem] min-w-0 gap-3 overflow-y-auto md:grid-cols-2"
                  aria-label="ETF 对象结果"
                  tabIndex={0}
                >
                  {detail.items!.map((item) => (
                    <article
                      key={item.id}
                      data-etf-code={item.code}
                      className="flex min-w-0 flex-col gap-2 rounded-lg border p-3"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <h4 className="font-medium">{item.code}</h4>
                        <Badge
                          variant={
                            item.state === "failed"
                              ? "destructive"
                              : "secondary"
                          }
                        >
                          {item.state === "succeeded"
                            ? "已完成"
                            : states[item.state]}
                        </Badge>
                      </div>
                      <p className="text-xs">
                        {item.dailyStart > detail.endDate &&
                        item.adjStart > detail.endDate ? (
                          "已有数据覆盖截止日，无需取数"
                        ) : (
                          <>
                            {stages[item.stage] || item.stage} · 已完成{" "}
                            {item.completedSegments} / {item.totalSegments} 段
                          </>
                        )}
                      </p>
                      <dl className="flex flex-col gap-2 text-xs">
                        <div>
                          <dt>日线</dt>
                          <dd>
                            {item.dailyStart > detail.endDate ? (
                              "已覆盖截止日，无需取数"
                            ) : (
                              <>
                                起点 {date(item.dailyStart)} · 已处理至{" "}
                                {date(item.dailyCheckpoint)} · {item.dailyRows}{" "}
                                条
                              </>
                            )}
                          </dd>
                        </div>
                        <div>
                          <dt>复权因子</dt>
                          <dd>
                            {item.adjStart > detail.endDate ? (
                              "已覆盖截止日，无需取数"
                            ) : (
                              <>
                                起点 {date(item.adjStart)} · 已处理至{" "}
                                {date(item.adjCheckpoint)} · {item.adjRows} 条
                              </>
                            )}
                          </dd>
                        </div>
                      </dl>
                      {item.message && (
                        <p className="break-words text-xs">
                          {item.message}
                          {item.errorCode ? `（${item.errorCode}）` : ""}
                        </p>
                      )}
                      {item.parentRun && (
                        <p className="break-all text-xs text-muted-foreground">
                          恢复自 {item.parentRun}
                        </p>
                      )}
                    </article>
                  ))}
                </div>
                {!!detail.events?.length && (
                  <details>
                    <summary className="cursor-pointer text-sm">
                      执行记录（最近 200 条）
                    </summary>
                    <ol
                      className="mt-3 flex max-h-64 flex-col gap-3 overflow-y-auto"
                      aria-label="ETF 执行记录"
                    >
                      {detail.events.map((event, index) => (
                        <li key={index} className="text-xs">
                          <p>
                            {event.code} ·{" "}
                            <time dateTime={event.at}>
                              {new Date(event.at).toLocaleString("zh-CN", {
                                timeZone: "Asia/Shanghai",
                                hour12: false,
                              })}{" "}
                              · 北京时间
                            </time>
                          </p>
                          <p>{event.message}</p>
                        </li>
                      ))}
                    </ol>
                  </details>
                )}
              </section>
            )}
          </div>
          <p className="text-xs leading-6 text-muted-foreground">
            显示最近 50
            个批次。已处理至表示该范围已完成源端查询，不代表每日均有行情；记录数包含幂等写入。同步完成后，轮动结果仍需整组计算和发布。
          </p>
        </CardContent>
      </Card>
    </ResearchTheme>
  );
}
