import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { fmt } from "@/lib/rotation";
import { isDailyDates, type DailyDates } from "@/lib/rotation-daily";

const stateNames: Record<string, string> = {
  ready: "已发布",
  pending: "生成中",
  updating: "等待重算",
  computing: "计算中",
  syncing: "数据同步中",
  failed: "失败",
  unavailable: "未发布",
  missing: "缺失",
  waiting: "等待计划时点",
};
export function RotationDailyHistory({
  selectedDate,
  onSelect,
}: {
  selectedDate: string;
  onSelect: (date: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState("");
  const [index, setIndex] = useState<DailyDates | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  const load = useCallback(async (before = "") => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const timeout = window.setTimeout(
      () => controller.abort(new Error("timeout")),
      10000,
    );
    setBusy(true);
    try {
      const response = await fetch(
        `/api/rotation/daily/dates${before ? `?before=${before}` : ""}`,
        {
          signal: controller.signal,
          cache: "no-store",
        },
      );
      if (response.status === 401 || response.status === 403)
        throw new Error("登录已过期或无权访问，请重新登录后读取档案。");
      if (!response.ok) throw new Error("档案列表暂不可用，请重新读取。");
      const data: unknown = await response.json();
      if (
        !isDailyDates(data) ||
        (before && data.dates.some((item) => item.tradeDate >= before))
      )
        throw new Error("档案日期返回异常，请重新读取。");
      if (!controller.signal.aborted && request.current === controller) {
        setIndex((previous) => ({
          ...data,
          dates:
            before && previous
              ? [...previous.dates, ...data.dates]
              : data.dates,
        }));
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
            ? "档案读取超时，请重新读取。"
            : e instanceof Error
              ? e.message
              : "档案读取失败。",
        );
    } finally {
      window.clearTimeout(timeout);
      if (request.current === controller) setBusy(false);
    }
  }, []);
  useEffect(() => {
    void load();
    return () => {
      request.current?.abort();
      request.current = null;
    };
  }, [load]);
  const choose = (date: string) => {
    onSelect(date);
    setOpen(false);
  };
  return (
    <div className="rotation-daily-history">
      <div className="flex flex-wrap items-center gap-3">
        <Dialog
          open={open}
          onOpenChange={(next) => {
            setOpen(next);
            if (next) {
              setInput(selectedDate ? fmt(selectedDate) : "");
              void load();
            }
          }}
        >
          <DialogTrigger asChild>
            <Button variant="outline">选择历史日期</Button>
          </DialogTrigger>
          <DialogContent className="max-h-[85dvh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>每日数据档案</DialogTitle>
              <DialogDescription>
                只列出已留存的日期。14:45
                参考缺失时不会用其他价格补齐；选日不改变下方回测区间。
              </DialogDescription>
            </DialogHeader>
            <form
              className="flex flex-col gap-3"
              onSubmit={(event) => {
                event.preventDefault();
                if (input) choose(input.replaceAll("-", ""));
              }}
            >
              <Field>
                <FieldLabel htmlFor="daily-history-date">指定交易日</FieldLabel>
                <Input
                  id="daily-history-date"
                  type="date"
                  required
                  value={input}
                  max={index?.currentDate ? fmt(index.currentDate) : undefined}
                  onChange={(event) => setInput(event.target.value)}
                />
              </Field>
              <Button type="submit" variant="outline">
                查看此日
              </Button>
            </form>
            <p className="rotation-daily-message">
              {index?.earliestDate
                ? `档案始于 ${fmt(index.earliestDate)}；各日期可能只留存一个时点。`
                : index
                  ? "尚无留存档案。"
                  : "档案起始日尚未确认。"}
            </p>
            {error && (
              <p role="alert">
                {error} {index && "下方保留上次读取的列表。"}
              </p>
            )}
            <Button
              variant="outline"
              aria-disabled={busy}
              onClick={() => {
                if (!busy) void load();
              }}
            >
              重新读取档案
            </Button>
            <div aria-live="polite">
              {busy
                ? "正在读取档案…"
                : index
                  ? `已列出 ${index.dates.length} 个留存日`
                  : ""}
            </div>
            <ul className="rotation-daily-date-list" aria-label="已留存日期">
              {index?.dates.map((item) => (
                <li key={item.tradeDate}>
                  <Button
                    variant="outline"
                    aria-pressed={selectedDate === item.tradeDate}
                    onClick={() => choose(item.tradeDate)}
                  >
                    <span>{fmt(item.tradeDate)}</span>
                    <span className="rotation-daily-date-states">
                      14:45 {stateNames[item.reference.status] || "状态待确认"}{" "}
                      · 收盘 {stateNames[item.close.status] || "状态待确认"}
                    </span>
                  </Button>
                </li>
              ))}
            </ul>
            {index?.nextBefore && (
              <Button
                variant="outline"
                aria-disabled={busy}
                onClick={() => {
                  if (!busy) void load(index.nextBefore);
                }}
              >
                加载更早日期
              </Button>
            )}
          </DialogContent>
        </Dialog>
        {selectedDate && (
          <Button variant="outline" onClick={() => onSelect("")}>
            回到最新
          </Button>
        )}
        <span className="rotation-daily-message">
          {selectedDate ? `已选择 ${fmt(selectedDate)}` : "默认最新可用数据"}
        </span>
      </div>
      <p className="rotation-daily-message">
        {index?.earliestDate
          ? `真实留存始于 ${fmt(index.earliestDate)}`
          : index
            ? "尚无每日留存档案"
            : "档案起始日尚未确认"}
        {error && " · 档案读取失败，可打开历史日期重试"}
      </p>
    </div>
  );
}
