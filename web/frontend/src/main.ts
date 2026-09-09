import "./style.css";

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
const app = document.querySelector<HTMLDivElement>("#app")!;
const icons: Record<string, string> = {
  market: '<path d="M3 17V7m6 13V3m6 13V8m6 10V5M1 10h4m2 4h4m2-3h4m2 3h4"/>',
  data: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0"/>',
  research:
    '<path d="M4 4h6c2 0 2 2 2 2s0-2 2-2h6v15h-6c-2 0-2 2-2 2s0-2-2-2H4zM12 6v15"/>',
  search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/>',
  arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
  shield: '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6zM8 12l3 3 5-6"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/>',
  refresh:
    '<path d="M20 7v5h-5M4 17v-5h5M19 12a7 7 0 0 0-12-5L4 10m1 2a7 7 0 0 0 12 5l3-3"/>',
};
const icon = (name: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] ?? icons.data}</svg>`;
const esc = (s: unknown) =>
  String(s ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ]!,
  );
const num = (n: number | null) =>
  n === null ? "—" : n.toLocaleString("zh-CN");
const date = (v: string) =>
  /^\d{8}$/.test(v) ? `${v.slice(0, 4)}.${v.slice(4, 6)}.${v.slice(6)}` : "—";
let catalog: Catalog | null = null;
let active = "all";
let query = "";
let loading = false;
const page =
  location.pathname === "/data"
    ? "data"
    : location.pathname === "/research"
      ? "research"
      : "market";
const titles = { market: "市场总览", data: "数据管理", research: "研究档案" };
app.innerHTML = `<a class="skip" href="#main">跳转到主要内容</a><aside class="sidebar"><a class="brand" href="/market"><span class="brand-mark"><i></i><i></i><i></i></span>Candela<span class="brand-dot">.</span></a><p class="nav-caption">工作空间</p><nav aria-label="主要导航">${(["market", "data", "research"] as const).map((p) => `<a href="/${p}" ${page === p ? 'aria-current="page"' : ""}>${icon(p)}<span>${titles[p]}</span>${page === p ? '<b class="nav-dot"></b>' : ""}</a>`).join("")}</nav><div class="sidebar-bottom"><div class="access-icon">${icon("shield")}</div><div><strong>个人工作空间</strong><span>Cloudflare Access 保护</span></div></div></aside><div class="workspace"><header class="topbar"><span class="breadcrumb">工作空间 <span>/</span> <strong>${titles[page]}</strong></span><span class="version"><i></i> 基础版 · 只读</span></header><main id="main" tabindex="-1"></main><footer><span><b>Candela</b> · 让数据照亮决策</span><span>官方数据来源 Tushare · 仅供研究参考</span></footer></div>`;
const main = document.querySelector<HTMLElement>("#main")!;
const heading = (
  eyebrow: string,
  title: string,
  subtitle: string,
  action = "",
) =>
  `<div class="page-heading"><div><p class="eyebrow">${eyebrow}</p><h1>${title}</h1><p class="subtitle">${subtitle}</p></div>${action}</div>`;
if (page === "market") {
  main.innerHTML =
    heading(
      "MARKET OVERVIEW",
      "从全市场，观察下一步",
      "在这里查看市场环境。行情接入完成后，将呈现中证全指的长期走势。",
    ) +
    `<section class="market-card"><div class="card-heading"><div class="index-title"><span class="index-symbol">全</span><div><h2>中证全指 <span>000985.CSI</span></h2><p>沪深 A 股整体市场表现</p></div></div><span class="badge neutral">尚未接入</span></div><div class="market-empty"><div class="empty-graphic">${icon("market")}<span class="empty-dot"></span></div><p class="eyebrow">YOUR MARKET, IN PERSPECTIVE</p><h2>市场视图，正在起步</h2><p>中证全指的历史行情尚未接入。<br>当前不展示价格、MACD 或牛熊判断，避免将示意数据当成真实结果。</p><a class="button primary" href="/data">查看数据覆盖 ${icon("arrow")}</a></div><div class="market-footnote">${icon("clock")} 后续提供日 / 周 / 月 K 线、MACD，以及正式与暂算市场状态。</div></section><div class="info-grid"><article class="info-card">${icon("market")}<h3>观察与维护，各有空间</h3><p>市场总览专注行情与市场环境；同步与数据维护统一放在数据管理。</p></article><article class="info-card">${icon("shield")}<h3>数据完整，判断才有依据</h3><p>正式状态和当月暂算将分别展示。数据不足时明确提示，不用旧结果代替当前判断。</p></article><a class="info-card link-card" href="/research">${icon("research")}<h3>延续已有研究 ${icon("arrow")}</h3><p>历史研究页面保留原样，可从研究档案继续访问。</p></a></div>`;
} else if (page === "research") {
  main.innerHTML =
    heading(
      "RESEARCH ARCHIVE",
      "留住每一次探索",
      "已有研究页面保留原样。这里的历史研究结果不代表当前市场状态。",
    ) +
    `<section class="research-grid"><a class="research-card" href="/research/index.html"><span class="research-tag">ETF ROTATION</span><div class="research-art">${icon("research")}</div><h2>ETF 轮动研究</h2><p>历史策略与定投数据看板，包含安全阀对照。</p><span class="research-open">打开原研究页面 ${icon("arrow")}</span></a><a class="research-card" href="/research/no-valve.html"><span class="research-tag">COMPARISON</span><div class="research-art alternate">${icon("market")}</div><h2>无安全阀对照</h2><p>保留原有对照入口，继续检查历史研究结果。</p><span class="research-open">打开对照页面 ${icon("arrow")}</span></a></section>`;
} else {
  main.innerHTML =
    heading(
      "DATA MANAGEMENT",
      "先看清数据，再做研究",
      "集中查看数据来源、已有覆盖与同步设置。当前版本只读，不会触发数据同步。",
      `<button class="button" id="reload">${icon("refresh")} 刷新状态</button>`,
    ) +
    `<div id="summary" class="summary-grid" aria-label="数据概览"></div><section class="catalog-card"><div class="catalog-toolbar"><div class="tabs" role="group" aria-label="数据分类"><button data-group="all" class="active" aria-pressed="true">全部数据</button><button data-group="csi" aria-pressed="false">中证全指</button><button data-group="etf" aria-pressed="false">ETF</button><button data-group="sw" aria-pressed="false">申万行业</button></div><label class="search">${icon("search")}<input id="search" type="search" aria-label="搜索代码或名称" placeholder="搜索代码或名称"></label></div><div id="catalog" aria-live="polite"></div><div class="catalog-bottom"><span>${icon("shield")} 只读查询 · 不修改原始数据</span><span id="queried-at">尚未查询</span></div></section><p class="coverage-note">“已有数据”仅表示库内有记录，不代表已同步至最新交易日。覆盖日期按各数据集独立计算；参考数据不适用行情日期。</p>`;
  document
    .querySelector("#reload")!
    .addEventListener("click", () => void load());
  document.querySelector("#search")!.addEventListener("input", (e) => {
    query = (e.target as HTMLInputElement).value.trim().toLowerCase();
    renderRows();
  });
  document
    .querySelectorAll<HTMLButtonElement>("[data-group]")
    .forEach((button) =>
      button.addEventListener("click", () => {
        active = button.dataset.group!;
        document
          .querySelectorAll<HTMLButtonElement>("[data-group]")
          .forEach((b) => {
            b.classList.toggle("active", b === button);
            b.setAttribute("aria-pressed", String(b === button));
          });
        renderRows();
      }),
    );
  void load();
}
function renderRows() {
  if (!catalog) return;
  const groups = catalog.groups.filter(
    (g) => active === "all" || g.id === active,
  );
  const rows = groups.flatMap((group) => {
    if (group.status === "error")
      return [
        `<tr><td colspan="6"><div class="inline-error" role="alert"><strong>${esc(group.name)} · 读取失败</strong><span>${esc(group.message || "请稍后刷新重试。")}</span></div></td></tr>`,
      ];
    const items = group.items.filter((i) =>
      `${i.name} ${i.code} ${i.kind}`.toLowerCase().includes(query),
    );
    if (!items.length && !query)
      return [
        `<tr><td colspan="6"><div class="group-empty">${esc(group.name)} · 暂无数据集记录</div></td></tr>`,
      ];
    return items.map((item) => {
      const labels: Record<string, string> = {
        available: "已有数据",
        no_data: "暂无数据",
        not_connected: "尚未接入",
        error: "读取失败",
      };
      return `<tr><td><div class="dataset-name"><span class="dataset-icon ${group.id}">${icon(group.id === "csi" ? "market" : "data")}</span><div><strong>${esc(item.name || item.code)}</strong><small>${esc(item.code || group.name)}</small></div></div></td><td><span class="type-label">${esc(item.kind)}</span></td><td><span class="badge ${item.status === "available" ? "green" : "neutral"}"><i></i>${labels[item.status] || "未知状态"}</span></td><td class="dates">${item.startDate ? `${date(item.startDate)} <span>→</span> ${date(item.endDate)}` : '<span class="muted">—</span>'}${item.note ? `<small>${esc(item.note)}</small>` : ""}</td><td class="numeric">${num(item.rows)}</td><td><span class="sync-state ${item.syncEnabled ? "enabled" : ""}">${item.syncEnabled === undefined ? "—" : item.syncEnabled ? "● 已启用" : "○ 已停用"}</span></td></tr>`;
    });
  });
  document.querySelector("#catalog")!.innerHTML = rows.length
    ? `<div class="table-scroll"><table><thead><tr><th>数据集</th><th>数据类型</th><th>覆盖状态</th><th>覆盖日期</th><th class="numeric">记录数</th><th>同步设置</th></tr></thead><tbody>${rows.join("")}</tbody></table></div>`
    : `<div class="blank-state">没有匹配的数据集。试试其他代码或名称。</div>`;
}
async function load() {
  if (loading) return;
  loading = true;
  const button = document.querySelector<HTMLButtonElement>("#reload")!;
  button.disabled = true;
  document.querySelector("#catalog")!.innerHTML =
    '<div class="blank-state loading" role="status">正在读取真实数据覆盖…</div>';
  document.querySelector("#summary")!.innerHTML = [
    "数据分类",
    "已有数据集",
    "已存记录",
  ]
    .map(
      (label) =>
        `<article class="summary-card"><span>${label}</span><strong>—</strong><small>等待查询结果</small></article>`,
    )
    .join("");
  document.querySelector("#queried-at")!.textContent = "正在查询";
  try {
    const response = await fetch("/api/catalog", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (!response.ok)
      throw new Error(
        response.status === 401
          ? "登录已过期，请刷新页面重新通过 Access 登录。"
          : "数据服务暂时不可用，请稍后刷新重试。",
      );
    const data = (await response.json()) as Catalog;
    if (
      !Array.isArray(data.groups) ||
      !data.groups.every((g) => Array.isArray(g.items))
    )
      throw new Error("数据服务返回异常，请稍后重试。");
    catalog = data;
    const items = data.groups.flatMap((g) => g.items);
    const partial = data.groups.some((g) => g.status === "error");
    const available = items.filter((i) => i.status === "available").length;
    const count = items.reduce((s, i) => s + (i.rows ?? 0), 0);
    document.querySelector("#summary")!.innerHTML =
      `<article class="summary-card"><span>数据分类 ${icon("data")}</span><strong>${data.groups.length}<em>类</em></strong><small>中证全指 / ETF / 申万行业</small></article><article class="summary-card"><span>已有数据集 ${icon("shield")}</span><strong>${available}<em>/ ${items.length}</em></strong><small>${partial ? "部分分类读取失败，仅统计已读取部分" : "日线、复权因子与参考数据分别统计"}</small></article><article class="summary-card"><span>已存记录 ${icon("clock")}</span><strong>${num(count)}</strong><small>${partial ? "部分分类读取失败，仅统计已读取部分" : "已有数据记录合计，不代表同步进度"}</small></article>`;
    renderRows();
    document.querySelector("#queried-at")!.textContent =
      "查询于 " +
      new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      }).format(new Date()) +
      " · 北京时间";
  } catch (err) {
    catalog = null;
    document.querySelector("#catalog")!.innerHTML =
      `<div class="blank-state error" role="alert"><strong>暂时无法读取数据</strong><p>${esc(err instanceof Error ? err.message : "请稍后重试。")}</p></div>`;
    document.querySelector("#queried-at")!.textContent = "查询失败";
  } finally {
    loading = false;
    button.disabled = false;
  }
}
