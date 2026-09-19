# Candela 研究样板 · v1.0

样板已固定为 [DESIGN.md v1.0](../DESIGN.md) 的正式基线：中文细宋、英文与数字 Source Serif 4、紧凑研究排版、净白纸面、胭脂湖蓝数据色及细线控件。

- [打开研究总览](http://127.0.0.1:4173/design-preview#overview)
- [打开研究工具](http://127.0.0.1:4173/design-preview#components)
- [实际验收记录](../../../web/frontend/design-qa.md)

页面直接进入研究报告，不再展示字体、纸面、排版或配色候选。旧风格参数会移除，旧候选锚点回到总览；观察周期、收益/回撤、图例、筛选、排序等功能保持可操作。

## 启动与访问

在 `web/frontend` 运行：

```sh
npm run dev -- --host 127.0.0.1 --port 4173 --strictPort
```

在 Codex 远程工作区也使用上述服务端地址发起预览。应用浏览器可能显示临时转发端口，重新打开或分享预览入口时仍使用 `127.0.0.1:4173`，不要沿用旧标签页的临时端口。加载失败时先检查：

```sh
curl -I http://127.0.0.1:4173/design-preview
```

## 体验范围

研究报告包含区间指标、收益与回撤曲线、三年月度热力图和同期比较表。研究工具包含搜索、周期、排序、勾选、详情、真实 CSV 下载和金额校验。页面底部折叠的“加载、空数据与失败状态示例”只用于检查控件反馈，不切换设计风格。

数据来自固定模拟样本，不接入实际行情。实现沿用 shadcn/ui（Radix）、ECharts 和 Lucide，已选字体本地加载并保留原始许可。

## 定稿验收

- [研究总览截图](finalized-overview.png)
- [研究工具截图](finalized-components.png)
- [手机截图](finalized-mobile.png)
- [风格固定、旧链接与响应式检查](finalized-design-checks.json)
- [研究工具交互检查](finalized-component-checks.json)

## 历史存档

[探索过程记录](../history/preview-v0.20.md)和[旧规范](../history/DESIGN-v0.20.md)仅用于追溯选择过程。当前目录仅保留上方三张定稿截图；参考图、旧方案与过程截图已移除。文字观察与 JSON 检查记录保留，历史链接中的参数不会再开启候选样式。
