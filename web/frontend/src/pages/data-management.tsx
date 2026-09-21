import { RotationCaptures } from "@/components/rotation-captures";
import { IndexSync } from "./index-sync";
import { Fragment, useEffect, useRef, useState } from "react";
import { RefreshCw, Search } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

type Item = {
  id: string;
  code: string;
  name: string;
  kind: string;
  status: string;
  syncEnabled?: boolean;
  rows: number | null;
  startDate: string;
  endDate: string;
  note?: string;
};
type Group = {
  id: string;
  name: string;
  status: string;
  items: Item[];
  message?: string;
};
type Catalog = { groups: Group[] };
const formatNumber = (value: number | null) =>
  value === null ? "—" : value.toLocaleString("zh-CN");
const formatDate = (value: string) =>
  /^\d{8}$/.test(value)
    ? `${value.slice(0, 4)}.${value.slice(4, 6)}.${value.slice(6)}`
    : "—";
const statuses: Record<string, string> = {
  available: "已有数据",
  no_data: "暂无数据",
  not_connected: "尚未接入",
  error: "读取失败",
};

export function DataManagement() {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [queriedAt, setQueriedAt] = useState("");
  const [category, setCategory] = useState("all");
  const [search, setSearch] = useState("");
  const [refresh, setRefresh] = useState(0);
  const pending = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    pending.current = true;
    setLoading(true);
    setError("");
    setCatalog(null);
    async function load() {
      try {
        const response = await fetch("/api/catalog", {
          credentials: "same-origin",
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok)
          throw new Error(
            response.status === 401
              ? "登录已过期，请刷新页面重新通过 Access 登录。"
              : "数据服务暂时不可用，请稍后刷新重试。",
          );
        const data = (await response.json()) as Catalog;
        if (
          !data ||
          !Array.isArray(data.groups) ||
          !data.groups.every((group) => group && Array.isArray(group.items))
        )
          throw new Error("数据服务返回异常，请稍后重试。");
        setCatalog(data);
        setQueriedAt(
          new Intl.DateTimeFormat("zh-CN", {
            timeZone: "Asia/Shanghai",
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
            hour12: false,
          }).format(new Date()),
        );
      } catch (err) {
        if (!controller.signal.aborted)
          setError(err instanceof Error ? err.message : "请稍后重试。");
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          pending.current = false;
        }
      }
    }
    void load();
    return () => controller.abort();
  }, [refresh]);

  const items = catalog?.groups.flatMap((group) => group.items) ?? [];
  const partial = catalog?.groups.some((group) => group.status === "error");
  const query = search.trim().toLowerCase();
  const groups =
    catalog?.groups
      .filter((group) => category === "all" || group.id === category)
      .map((group) => ({
        ...group,
        items: group.items.filter((item) =>
          `${item.name} ${item.code} ${item.kind}`
            .toLowerCase()
            .includes(query),
        ),
      })) ?? [];
  const hasRows = groups.some(
    (group) => group.status === "error" || group.items.length || !query,
  );
  const summary = [
    {
      title: "数据分类",
      value: catalog ? `${catalog.groups.length} 类` : "—",
      note: "中证全指 / ETF / 申万行业",
    },
    {
      title: "已有数据集",
      value: catalog
        ? `${items.filter((item) => item.status === "available").length} / ${items.length}`
        : "—",
      note: partial
        ? "部分分类读取失败，仅统计已读取部分"
        : "日线、复权因子与参考数据分别统计",
    },
    {
      title: "已存记录",
      value: catalog
        ? formatNumber(items.reduce((sum, item) => sum + (item.rows ?? 0), 0))
        : "—",
      note: partial
        ? "部分分类读取失败，仅统计已读取部分"
        : "已有数据记录合计，不代表同步进度",
    },
  ];

  return (
    <div className="space-y-7">
      <div className="flex flex-wrap items-start justify-between gap-5">
        <div>
          <h1 className="text-2xl font-semibold">数据管理</h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            查看数据来源、覆盖范围与同步设置。刷新仅重新查询数据状态。
          </p>
        </div>
        <Button
          variant="outline"
          disabled={loading}
          onClick={() => {
            if (!pending.current) {
              pending.current = true;
              setRefresh((value) => value + 1);
            }
          }}
        >
          <RefreshCw
            aria-hidden="true"
            className={loading ? "animate-spin" : ""}
          />
          刷新状态
        </Button>
      </div>
      <IndexSync onCompleted={() => setRefresh((value) => value + 1)} />
      <RotationCaptures />
      <div aria-label="数据概览" className="grid gap-4 sm:grid-cols-3">
        {summary.map((item) => (
          <Card key={item.title} className="gap-3 shadow-none">
            <CardHeader>
              <CardDescription>{item.title}</CardDescription>
              <CardTitle className="text-2xl tabular-nums">
                {item.value}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-xs leading-5 text-muted-foreground">
              {loading ? "等待查询结果" : item.note}
            </CardContent>
          </Card>
        ))}
      </div>
      <Card className="min-w-0 gap-0 overflow-hidden shadow-none">
        <CardHeader className="flex flex-wrap gap-4 pb-6">
          <Tabs value={category} onValueChange={setCategory}>
            <TabsList aria-label="数据分类" className="h-auto flex-wrap">
              <TabsTrigger value="all">全部数据</TabsTrigger>
              <TabsTrigger value="csi">中证全指</TabsTrigger>
              <TabsTrigger value="etf">ETF</TabsTrigger>
              <TabsTrigger value="sw">申万行业</TabsTrigger>
            </TabsList>
          </Tabs>
          <div className="relative w-full sm:max-w-xs">
            <Search
              aria-hidden="true"
              className="absolute top-2.5 left-3 size-4 text-muted-foreground"
            />
            <Input
              type="search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="pl-9"
              aria-label="搜索代码或名称"
              placeholder="搜索代码或名称"
            />
          </div>
        </CardHeader>
        <CardContent className="px-0" aria-live="polite" aria-busy={loading}>
          {loading ? (
            <p
              role="status"
              className="p-10 text-center text-sm text-muted-foreground"
            >
              正在读取真实数据覆盖…
            </p>
          ) : error ? (
            <div className="px-6 pb-6">
              <Alert variant="destructive">
                <AlertTitle>暂时无法读取数据</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            </div>
          ) : !hasRows ? (
            <p className="p-10 text-center text-sm text-muted-foreground">
              没有匹配的数据集。试试其他代码或名称。
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">数据集</TableHead>
                  <TableHead>数据类型</TableHead>
                  <TableHead>覆盖状态</TableHead>
                  <TableHead>覆盖日期</TableHead>
                  <TableHead className="text-right">记录数</TableHead>
                  <TableHead className="pr-6">同步设置</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {groups.map((group) => (
                  <Fragment key={group.id}>
                    {group.status === "error" ? (
                      <TableRow>
                        <TableCell colSpan={6} className="px-6 py-4">
                          <Alert variant="destructive">
                            <AlertTitle>{group.name} · 读取失败</AlertTitle>
                            <AlertDescription>
                              {group.message || "请稍后刷新重试。"}
                            </AlertDescription>
                          </Alert>
                        </TableCell>
                      </TableRow>
                    ) : !group.items.length && !query ? (
                      <TableRow>
                        <TableCell
                          colSpan={6}
                          className="px-6 py-6 text-muted-foreground"
                        >
                          {group.name} · 暂无数据集记录
                        </TableCell>
                      </TableRow>
                    ) : (
                      group.items.map((item) => (
                        <TableRow key={item.id}>
                          <TableCell className="py-4 pl-6">
                            <strong className="font-medium">
                              {item.name || item.code}
                            </strong>
                            <span className="mt-1 block text-xs text-muted-foreground">
                              {item.code || group.name}
                            </span>
                          </TableCell>
                          <TableCell>{item.kind}</TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                item.status === "available"
                                  ? "secondary"
                                  : "outline"
                              }
                            >
                              {statuses[item.status] || "未知状态"}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {item.startDate ? (
                              <>
                                <span>{formatDate(item.startDate)}</span> →{" "}
                                <span>{formatDate(item.endDate)}</span>
                              </>
                            ) : (
                              "—"
                            )}
                            {item.note && (
                              <span className="mt-1 block text-xs text-muted-foreground">
                                {item.note}
                              </span>
                            )}
                          </TableCell>
                          <TableCell className="text-right tabular-nums">
                            {formatNumber(item.rows)}
                          </TableCell>
                          <TableCell className="pr-6">
                            {item.syncEnabled === undefined
                              ? "—"
                              : item.syncEnabled
                                ? "已启用"
                                : "已停用"}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
        <div className="flex flex-wrap justify-between gap-2 border-t px-6 pt-5 text-xs text-muted-foreground">
          <span>只读查询 · 不修改原始数据</span>
          <span>
            {loading
              ? "正在查询"
              : error
                ? "查询失败"
                : `查询于 ${queriedAt} · 北京时间`}
          </span>
        </div>
      </Card>
      <p className="text-xs leading-6 text-muted-foreground">
        “已有数据”不代表已同步至最新交易日。覆盖日期按各数据集独立计算；参考数据不适用行情日期。
      </p>
    </div>
  );
}
