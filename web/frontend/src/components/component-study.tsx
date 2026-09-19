import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from "react";
import {
  ArrowUpRight,
  Check,
  ChevronDown,
  Download,
  Info,
  RotateCcw,
  Search,
} from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  assets,
  metrics,
  observations,
  percent,
  type AssetId,
} from "@/lib/design-preview-data";
import { researchTheme } from "@/lib/research-theme";

type Scenario = "normal" | "loading" | "empty" | "error";
type StudyRecord = (typeof assets)[number] & ReturnType<typeof metrics>;
const scenarios: { value: Scenario; label: string }[] = [
  { value: "normal", label: "正常" },
  { value: "loading", label: "加载中" },
  { value: "empty", label: "无数据" },
  { value: "error", label: "失败" },
];
const initialSelection: AssetId[] = ["strategy", "dividend", "gold"];
const moneyFormat = new Intl.NumberFormat("zh-CN", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

function StudyDetail({
  record,
  range,
  selected,
  onSelect,
  disabled,
}: {
  record: StudyRecord;
  range: string;
  selected: boolean;
  onSelect: () => void;
  disabled: boolean;
}) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`查看${record.name}详情`}
          disabled={disabled}
        >
          详情 <ArrowUpRight data-icon="inline-end" />
        </Button>
      </DialogTrigger>
      <DialogContent
        className="component-detail-dialog"
        showCloseButton={false}
      >
        <DialogHeader>
          <DialogTitle>{record.name}</DialogTitle>
          <DialogDescription>
            近 {range} 个月 · 固定模拟数据 · 截止 2025.12.31
          </DialogDescription>
        </DialogHeader>
        <dl className="component-detail-values">
          <div>
            <dt>累计收益</dt>
            <dd className="research-number">{percent(record.total)}</dd>
          </div>
          <div>
            <dt>年化收益</dt>
            <dd className="research-number">{percent(record.annual)}</dd>
          </div>
          <div>
            <dt>最大回撤</dt>
            <dd className="research-number">{percent(record.drawdown)}</dd>
          </div>
        </dl>
        <p>
          累计收益按区间首尾净值计算，最大回撤基于区间内的采样点。选择状态会同步到观察清单。
        </p>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">返回清单</Button>
          </DialogClose>
          <Button onClick={onSelect} aria-pressed={selected}>
            {selected && <Check data-icon="inline-start" />}
            {selected ? "取消选择" : "加入选择"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ComponentStudy({
  range,
  onRangeChange,
}: {
  range: string;
  onRangeChange: (value: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<AssetId[]>(initialSelection);
  const [descending, setDescending] = useState(true);
  const [scenario, setScenario] = useState<Scenario>("normal");
  const [exporting, setExporting] = useState(false);
  const [notice, setNotice] = useState("");
  const [amount, setAmount] = useState("100000");
  const [amountTouched, setAmountTouched] = useState(false);
  const [calculatedAmount, setCalculatedAmount] = useState<number | null>(null);
  const amountInput = useRef<HTMLInputElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );

  const points = useMemo(() => observations(Number(range)), [range]);
  const records = useMemo(
    () =>
      assets.map((asset) => ({
        ...asset,
        ...metrics(points, asset.id, Number(range)),
      })),
    [points, range],
  );
  const filtered = records
    .filter((record) =>
      `${record.name} ${record.short} ${record.id}`
        .toLowerCase()
        .includes(query.trim().toLowerCase()),
    )
    .sort((a, b) => (descending ? b.total - a.total : a.total - b.total));
  const selectedRecords = filtered.filter((record) =>
    selected.includes(record.id),
  );
  const allSelected =
    filtered.length > 0 && selectedRecords.length === filtered.length;
  const busy = scenario === "loading" || exporting;
  const parsedAmount = Number(amount.replaceAll(",", "").trim());
  const amountValid =
    amount.trim() !== "" &&
    Number.isFinite(parsedAmount) &&
    parsedAmount > 0 &&
    parsedAmount <= 100000000;
  const showAmountError = amountTouched && !amountValid;
  const strategyReturn = records.find(
    (record) => record.id === "strategy",
  )!.total;

  function changeSelection(id: AssetId) {
    setSelected((current) =>
      current.includes(id)
        ? current.filter((value) => value !== id)
        : [...current, id],
    );
    setNotice("");
  }
  function selectAll(checked: boolean) {
    const visibleIds = filtered.map((record) => record.id);
    setSelected((current) =>
      checked
        ? [...new Set([...current, ...visibleIds])]
        : current.filter((id) => !visibleIds.includes(id)),
    );
    setNotice("");
  }
  function changeScenario(value: Scenario) {
    if (timer.current !== null) clearTimeout(timer.current);
    setScenario(value);
    setNotice("");
  }
  function refresh() {
    changeScenario("loading");
    timer.current = setTimeout(() => {
      setScenario("normal");
      setNotice("样本已就绪，筛选和选择已保留。");
      timer.current = null;
    }, 850);
  }
  function reset() {
    changeScenario("normal");
    setQuery("");
    setSelected(initialSelection);
    setDescending(true);
    onRangeChange("12");
    setNotice("已恢复近一年与默认选择。");
  }
  function exportSelected() {
    if (busy || scenario !== "normal" || !selectedRecords.length) return;
    const exportRecords = [...selectedRecords];
    const exportRange = range;
    const rows = [
      [
        "日期",
        ...exportRecords.map((record) => `${record.name}模拟累计收益(%)`),
      ],
      ...points.map((point) => [
        point.date,
        ...exportRecords.map((record) =>
          (point.values[record.id] - 100).toFixed(4),
        ),
      ]),
    ];
    setExporting(true);
    setNotice("正在准备所选记录…");
    timer.current = setTimeout(() => {
      const url = URL.createObjectURL(
        new Blob(["\uFEFF" + rows.map((row) => row.join(",")).join("\n")], {
          type: "text/csv;charset=utf-8",
        }),
      );
      const link = document.createElement("a");
      link.href = url;
      link.download = `candela-demo-${exportRange}months-selected.csv`;
      link.click();
      URL.revokeObjectURL(url);
      setExporting(false);
      setNotice(
        `已导出 ${exportRecords.length} 项，近 ${exportRange} 个月的模拟数据。`,
      );
      timer.current = null;
    }, 650);
  }
  function calculate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAmountTouched(true);
    if (!amountValid) {
      setCalculatedAmount(null);
      amountInput.current?.focus();
      return;
    }
    setCalculatedAmount(parsedAmount);
  }

  return (
    <section
      id="components"
      className="component-section component-study"
      aria-labelledby="component-study-title"
    >
      <div className="component-heading">
        <div>
          <p className="research-eyebrow">04 / 研究工具</p>
          <h2 id="component-study-title">选择、比较，再看细节。</h2>
        </div>
        <p>筛选标的、查看详情，导出所需的数据。</p>
      </div>
      <div className="component-grid component-workspace">
        <Card className="component-watchlist">
          <CardHeader>
            <div className="component-study-header">
              <div>
                <CardTitle>
                  <h3>观察清单</h3>
                </CardTitle>
                <CardDescription>
                  固定模拟数据 · 与报告共用观察周期
                </CardDescription>
              </div>
              <Button variant="outline" onClick={refresh} disabled={busy}>
                {scenario === "loading" ? (
                  <Spinner data-icon="inline-start" aria-hidden="true" />
                ) : (
                  <RotateCcw data-icon="inline-start" />
                )}
                {scenario === "loading" ? "正在加载" : "刷新样本"}
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <FieldGroup className="component-filters">
              <Field data-disabled={busy}>
                <FieldLabel htmlFor="component-search">搜索标的</FieldLabel>
                <Input
                  id="component-search"
                  placeholder="例如：黄金"
                  value={query}
                  disabled={busy}
                  onChange={(event) => {
                    setQuery(event.target.value);
                    setNotice("");
                  }}
                />
              </Field>
              <FieldSet disabled={busy} className="component-period">
                <FieldLegend variant="label" id="component-period-label">
                  观察周期
                </FieldLegend>
                <ToggleGroup
                  type="single"
                  value={range}
                  disabled={busy}
                  aria-labelledby="component-period-label"
                  onValueChange={(value) => {
                    if (value) {
                      onRangeChange(value);
                      setNotice("");
                    }
                  }}
                >
                  <ToggleGroupItem value="6">近半年</ToggleGroupItem>
                  <ToggleGroupItem value="12">近一年</ToggleGroupItem>
                  <ToggleGroupItem value="36">全部</ToggleGroupItem>
                </ToggleGroup>
              </FieldSet>
            </FieldGroup>
            <div
              className="component-table-area"
              aria-busy={scenario === "loading"}
            >
              {scenario === "loading" ? (
                <div className="component-loading">
                  <p role="status">正在整理观察清单…</p>
                  <div aria-hidden="true">
                    {[0, 1, 2, 3, 4].map((index) => (
                      <div className="component-skeleton-row" key={index}>
                        <Skeleton className="h-4 w-2/5" />
                        <Skeleton className="h-4 w-1/4" />
                        <Skeleton className="h-4 w-1/6" />
                      </div>
                    ))}
                  </div>
                </div>
              ) : scenario === "error" ? (
                <div className="component-error">
                  <Alert variant="destructive">
                    <Info />
                    <AlertTitle>暂时无法显示样本</AlertTitle>
                    <AlertDescription>
                      <p>观察周期、筛选条件与选择已保留，可以重新加载。</p>
                    </AlertDescription>
                  </Alert>
                  <Button variant="outline" onClick={refresh}>
                    <RotateCcw data-icon="inline-start" />
                    重新加载
                  </Button>
                </div>
              ) : scenario === "empty" || !filtered.length ? (
                <Empty>
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <Search />
                    </EmptyMedia>
                    <EmptyTitle>
                      {scenario === "empty"
                        ? "这个区间还没有数据"
                        : "没有找到匹配的标的"}
                    </EmptyTitle>
                    <EmptyDescription>
                      {scenario === "empty"
                        ? "恢复模拟样本后，可以继续比较与查看详情。"
                        : `没有名称包含“${query.trim()}”的记录，试试其他关键词。`}
                    </EmptyDescription>
                  </EmptyHeader>
                  <Button
                    variant="outline"
                    onClick={() =>
                      scenario === "empty"
                        ? changeScenario("normal")
                        : setQuery("")
                    }
                  >
                    {scenario === "empty" ? "恢复示例数据" : "清除筛选"}
                  </Button>
                </Empty>
              ) : (
                <Table
                  className="component-table"
                  aria-label="观察清单模拟表现"
                >
                  <TableCaption>近 {range} 个月 · 固定模拟样本</TableCaption>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="component-check-cell">
                        <label
                          className="component-check-target"
                          htmlFor="component-select-all"
                        >
                          <Checkbox
                            id="component-select-all"
                            checked={
                              allSelected
                                ? true
                                : selectedRecords.length
                                  ? "indeterminate"
                                  : false
                            }
                            disabled={busy}
                            onCheckedChange={(value) =>
                              selectAll(value === true)
                            }
                            aria-label="选择当前筛选的全部标的"
                          />
                        </label>
                      </TableHead>
                      <TableHead className="component-name-cell">
                        标的
                      </TableHead>
                      <TableHead
                        aria-sort={descending ? "descending" : "ascending"}
                      >
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy}
                          onClick={() => setDescending((value) => !value)}
                        >
                          累计收益{" "}
                          <ChevronDown
                            data-icon="inline-end"
                            className={descending ? undefined : "rotate-180"}
                          />
                        </Button>
                      </TableHead>
                      <TableHead>最大回撤</TableHead>
                      <TableHead>
                        <span className="sr-only">详情</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filtered.map((record) => (
                      <TableRow
                        key={record.id}
                        data-state={
                          selected.includes(record.id) ? "selected" : undefined
                        }
                      >
                        <TableCell className="component-check-cell">
                          <label
                            className="component-check-target"
                            htmlFor={`component-select-${record.id}`}
                          >
                            <Checkbox
                              id={`component-select-${record.id}`}
                              checked={selected.includes(record.id)}
                              disabled={busy}
                              onCheckedChange={() => changeSelection(record.id)}
                              aria-label={`选择${record.name}`}
                            />
                          </label>
                        </TableCell>
                        <TableCell className="component-name-cell">
                          <span className="table-series">
                            <span
                              className="series-dot"
                              style={{
                                background:
                                  researchTheme.series[
                                    assets.findIndex(
                                      (asset) => asset.id === record.id,
                                    )
                                  ],
                              }}
                            />
                            {record.name}
                          </span>
                        </TableCell>
                        <TableCell className="research-number">
                          {percent(record.total)}
                        </TableCell>
                        <TableCell className="research-number">
                          {percent(record.drawdown)}
                        </TableCell>
                        <TableCell>
                          <StudyDetail
                            record={record}
                            range={range}
                            selected={selected.includes(record.id)}
                            onSelect={() => changeSelection(record.id)}
                            disabled={busy}
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
            {scenario === "normal" && filtered.length > 0 && (
              <p className="component-table-scroll-hint">
                左右滑动表格，查看回撤与详情。
              </p>
            )}
          </CardContent>
          <CardFooter className="component-actions">
            <div className="component-selection-summary" role="status">
              <span>
                {scenario === "normal"
                  ? `当前筛选 ${filtered.length} 项 · 已选 ${selectedRecords.length} 项`
                  : "筛选与选择已保留"}
              </span>
              <span>
                {notice ||
                  (scenario === "normal" && !selectedRecords.length
                    ? "勾选至少一项后即可导出。"
                    : "导出当前筛选中已勾选的记录。")}
              </span>
            </div>
            <div className="component-action-buttons">
              <Button variant="ghost" disabled={exporting} onClick={reset}>
                重置条件
              </Button>
              <Button
                className="component-export-action"
                disabled={
                  busy || scenario !== "normal" || !selectedRecords.length
                }
                onClick={exportSelected}
              >
                {exporting ? (
                  <Spinner data-icon="inline-start" aria-hidden="true" />
                ) : (
                  <Download data-icon="inline-start" />
                )}
                {exporting ? "正在准备…" : "导出所选 CSV"}
              </Button>
            </div>
          </CardFooter>
        </Card>
        <div className="component-side">
          <Card className="component-calculator">
            <CardHeader>
              <CardTitle>
                <h3>金额换算</h3>
              </CardTitle>
              <CardDescription>
                轮动策略 · 近 {range} 个月 · 模拟计算
              </CardDescription>
            </CardHeader>
            <CardContent>
              <form noValidate onSubmit={calculate}>
                <FieldGroup>
                  <Field data-invalid={showAmountError}>
                    <FieldLabel htmlFor="sample-amount">
                      假设初始金额 / 元
                    </FieldLabel>
                    <Input
                      ref={amountInput}
                      id="sample-amount"
                      inputMode="decimal"
                      autoComplete="off"
                      value={amount}
                      onChange={(event) => {
                        setAmount(event.target.value);
                        setCalculatedAmount(null);
                      }}
                      onBlur={() => setAmountTouched(true)}
                      aria-invalid={showAmountError}
                      aria-describedby={
                        showAmountError
                          ? "component-amount-error"
                          : "component-amount-help"
                      }
                    />
                    <div className="component-amount-feedback">
                      {showAmountError ? (
                        <FieldError id="component-amount-error">
                          金额须大于 0 且不超过 1 亿元，例如 100,000。
                        </FieldError>
                      ) : (
                        <FieldDescription id="component-amount-help">
                          输入大于 0、不超过 1 亿元的金额。
                        </FieldDescription>
                      )}
                    </div>
                  </Field>
                  <Button type="submit" variant="outline">
                    计算示例 <ArrowUpRight data-icon="inline-end" />
                  </Button>
                </FieldGroup>
              </form>
              <div className="calculation-result" role="status">
                <span>模拟期末金额</span>
                <strong>
                  {calculatedAmount === null
                    ? "—"
                    : moneyFormat.format(
                        calculatedAmount * (1 + strategyReturn / 100),
                      )}
                  <small>元</small>
                </strong>
                <span>
                  {calculatedAmount === null
                    ? "填写金额后点击计算。"
                    : `初始 ${moneyFormat.format(calculatedAmount)} 元 · 区间 ${percent(strategyReturn)}`}
                </span>
              </div>
            </CardContent>
            <CardFooter>本金与区间收益的简单换算，不含额外现金流。</CardFooter>
          </Card>
          <aside className="component-reading-note">
            <p className="research-eyebrow">使用说明</p>
            <p>
              导出当前筛选中已勾选的标的。金额换算采用当前观察周期；打开详情后，可按
              Esc 返回。
            </p>
            <a href="#performance">
              回到收益与回撤图表 <ArrowUpRight aria-hidden="true" />
            </a>
          </aside>
        </div>
      </div>
      <details className="component-state-examples">
        <summary>查看加载、空数据与失败状态示例</summary>
        <div className="component-state-switcher">
          <Field orientation="horizontal" data-disabled={exporting}>
            <FieldTitle id="component-state-label">示例状态</FieldTitle>
            <ToggleGroup
              type="single"
              value={scenario}
              disabled={exporting}
              aria-labelledby="component-state-label"
              onValueChange={(value) => {
                if (scenarios.some((item) => item.value === value))
                  changeScenario(value as Scenario);
              }}
            >
              {scenarios.map((item) => (
                <ToggleGroupItem value={item.value} key={item.value}>
                  {item.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
          <p>同一份清单，查看不同状态下的反馈。</p>
        </div>
      </details>
    </section>
  );
}
