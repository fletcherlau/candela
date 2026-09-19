> 历史记录：v0.20 及之前的探索过程，不是当前实现规范。当前以 [DESIGN.md v1.0](../DESIGN.md) 为准；旧预览参数已停用。

# Candela 策略研究样板

当前版本 v0.20：研究报告式阅读结构、B · 净白留白纸面、A2 · 胭脂湖蓝数据配色、克制的文字与操作。参考 AI 2027，并结合用户补充的 OpenAI 研究页面方向与最初的纸卡参考。

在 `web/frontend` 运行 `npm run dev -- --port 4173 --strictPort`。
打开 [研究样板](http://127.0.0.1:4173/design-preview)。此前的 `theme` 查询参数不再影响当前版本。

在 Codex 远程工作区中，也使用上述服务端地址发起预览。应用浏览器可能显示另一临时转发端口；分享或重新打开时仍使用 `127.0.0.1:4173`，不要沿用旧标签页的临时端口。遇到加载失败，可先用 `curl -I http://127.0.0.1:4173/design-preview` 检查源站，再从上述链接重新打开。

页面采用已有 shadcn/ui（Radix）、ECharts、Lucide 与本地 Noto Serif SC、Noto Sans SC、Cormorant Garamond、Source Serif 4、Alegreya 字体。模拟数据为固定样本；可切换周期、收益/回撤和比较序列，查看数据、筛选排序，进入“组件试用”进行勾选、详情查看、金额换算、状态恢复与所选 CSV 导出。

报告布局基准截图（v0.5，最新字体效果见下方试读）：

- [首屏与主图](../preview/showcase.png)
- [桌面全页](../preview/desktop.png)
- [收益图版与边注](../preview/report-figure.png)
- [月度图版](../preview/detail.png)
- [手机首屏](../preview/mobile-top.png)
- [手机全页](../preview/mobile.png)
- [展开的附录](../preview/components.png)

规范：[DESIGN.md](../DESIGN.md)。验收：[design-qa.md](../../../web/frontend/design-qa.md)、[交互检查](../preview/report-checks.json)。整体视觉方向已获用户认可；数字字体已选定 Source Serif 4。

## 历史探索

以下截图保留为设计过程记录，当前页面已使用新方向，不再提供早期主题切换器：

- [A 晴空蓝](../preview/theme-blue.png)
- [B 松石绿](../preview/theme-pine.png)
- [C 暖陶棕](../preview/theme-clay.png)
- [D 麦穗黄](../preview/theme-wheat.png)
- [早期 A/B/C 比较](../preview/theme-comparison.png)


## 数字字体试读

“数字字体已选定”折叠区保留三张样卡，访问以下锚点链接会展开。样卡并排展示相同指标、金额、表格和符号；选择器可将候选应用于下方完整报告，切换保留周期和金额。

- [A · Inter](http://127.0.0.1:4173/design-preview?numbers=inter#number-fonts)
- [B · IBM Plex Sans](http://127.0.0.1:4173/design-preview?numbers=plex#number-fonts)
- [C · Source Serif 4](http://127.0.0.1:4173/design-preview?numbers=source-serif#number-fonts)
- [并排字样截图](../preview/number-font-comparison.png)
- [手机字样截图](../preview/number-font-mobile.png)
- [字体加载与交互检查](../preview/number-font-checks.json)

三款均使用本地真实字体文件，随站点附带原始许可证；默认使用已选定的 Source Serif 4；A/B 候选仍可临时对照，不改变规范中的决定。研究报告截图保留 v0.5 布局基准，数字字体最新效果以本节截图为准。


## 中文字体：已选定 A · 细宋

“中文字体已选定”折叠区保留 A 细宋（Noto Serif SC 300）、B 常规宋（同款 400）、C 细黑（Noto Sans SC 300）。正文独立切换 400 / 300，图注保持 400；中文样张数字固定 Source Serif 4。下方选择器应用于完整报告，保留周期与金额。

- [A · 细宋](http://127.0.0.1:4173/design-preview?numbers=source-serif&chinese=serif-light#chinese-fonts)
- [B · 常规宋](http://127.0.0.1:4173/design-preview?numbers=source-serif&chinese=serif-regular#chinese-fonts)
- [C · 细黑](http://127.0.0.1:4173/design-preview?numbers=source-serif&chinese=sans-light#chinese-fonts)
- [桌面对照](../preview/chinese-font-comparison.png)、[手机对照](../preview/chinese-font-mobile.png)、[A 版报告标题区](../preview/chinese-serif-light-report.png)
- [实际字体与交互检查](../preview/chinese-font-checks.json)

用户已选定 A · Noto Serif SC 细宋，标题 300；正文 400。B/C 保留为临时对照，无参数或无效值恢复 A。两款中文字体均为本地真实可变字体，随站点保留 SIL OFL 1.1 原始许可。


## 英文字体：已选定 B · Source Serif 4

页尾“英文字体已选定”折叠区保留三款英文；统一标题 300、正文 400。中文保持细宋，数字保持 Source Serif 4。点击选项后可以在完整报告中看英文标题、英文摘要和中英混排。

- [A · Inter](http://127.0.0.1:4173/design-preview?english=inter&chinese=serif-light&numbers=source-serif#english-fonts)
- [B · Source Serif 4](http://127.0.0.1:4173/design-preview?english=source-serif&chinese=serif-light&numbers=source-serif#english-fonts)
- [C · Cormorant Garamond](http://127.0.0.1:4173/design-preview?english=cormorant&chinese=serif-light&numbers=source-serif#english-fonts)
- [桌面对照](../preview/english-font-comparison.png)、[手机对照](../preview/english-font-mobile.png)、[B 版报告混排](../preview/english-source-serif-report.png)
- [真实字体加载与交互检查](../preview/english-font-checks.json)

用户最终选定 B · Source Serif 4：标题 300，正文 400。无参数或无效参数回退 B；A/C 仅作临时对照。三款原始 SIL OFL 1.1 许可证均可从样卡打开。中文与数字历史样张折叠，原锚点链接仍会展开相应对照区。


[查看已选定的完整报告](http://127.0.0.1:4173/design-preview?english=source-serif&chinese=serif-light&numbers=source-serif#overview)：中文细宋 + 英文 Source Serif 4 + 数字 Source Serif 4。最新 [B 版报告截图](../preview/english-source-serif-report.png)、[检查记录](../preview/selected-english-font-checks.json)。


字体搭配已由用户确认定稿：中文 Noto Serif SC 细宋，英文与数字 Source Serif 4；中英文标题 300，正文与数字 400。排版也已选定 B · 紧凑研究，历史样张继续保留作对照。


## 排版已选定 B · 紧凑研究

同一份报告可以直接切换 A/B，字体、配色和内容固定。切换保留周期、图表模式、序列、筛选与金额；字体历史对照已移到页尾。

- [A · 舒展阅读](http://127.0.0.1:4173/design-preview?density=relaxed#layout-density)：较大的正文、较宽松的段落与图文留白。
- [B · 紧凑研究](http://127.0.0.1:4173/design-preview?density=compact#layout-density)：收紧标题、段落与表格间距，便于对照数据。
- 桌面：[A](../preview/density-relaxed-desktop.png) / [B](../preview/density-compact-desktop.png)；全页：[A](../preview/density-relaxed-full.png) / [B](../preview/density-compact-full.png)。
- 图版：[A](../preview/density-relaxed-figure.png) / [B](../preview/density-compact-figure.png)；手机：[A](../preview/density-relaxed-mobile.png) / [B](../preview/density-compact-mobile.png)。
- [实际尺寸与交互检查](../preview/density-checks.json)。用户已选定 B，无参数或无效参数回退 B；A 为临时对照。两版手机正文都保持至少 16px。

[默认值与状态检查](../preview/selected-density-checks.json)。页尾折叠区明确显示“排版已选定：B · 紧凑研究”，历史截图保留原对照状态。


## 当前：纸面已选定 B · 净白留白

用户已选择 B · 净白留白：纯白底色与纸面、浅灰细边、12px 圆角、无卡片阴影。默认和无效参数回退 B；A/C 仍可临时对照，页面明确显示定稿与临时状态。字体、B · 紧凑研究和数据配色保持。

| 方案 | 打开样板 | 图版截图 | 手机截图 |
| --- | --- | --- | --- |
| A · 暖纸书页 | [试读 A](http://127.0.0.1:4173/design-preview?paper=warm#paper-surfaces) | [暖灰与微暖白](../preview/paper-warm-figure.png) | [A 手机](../preview/paper-warm-mobile.png) |
| B · 净白留白（已选定） | [查看 B](http://127.0.0.1:4173/design-preview?paper=white#paper-surfaces) | [纯白与轻细边](../preview/paper-white-figure.png) | [B 手机](../preview/paper-white-mobile.png) |
| C · 轻叠纸页 | [试读 C](http://127.0.0.1:4173/design-preview?paper=layered#paper-surfaces) | [灰衬底与白纸投影](../preview/paper-layered-figure.png) | [C 手机](../preview/paper-layered-mobile.png) |

三版首屏：[A](../preview/paper-warm-desktop.png) / [B](../preview/paper-white-desktop.png) / [C](../preview/paper-layered-desktop.png)。[参考来源](../references/paper/README.md)包括实际 Anthropic / 有知有行截图，以及 ChatGPT 官方展示的界面图；ChatGPT 交互首页触发验证，没有把验证页当作参考。

纸面选择覆盖报告、表格、表单、弹窗和图表提示框，保留现有筛选与输入。`paper` 参数可直接分享；字体和排版历史样张已收进页尾折叠区，旧锚点仍有效。响应式记录：[paper-responsive-checks.json](../preview/paper-responsive-checks.json)；交互与颜色记录：[paper-checks.json](../preview/paper-checks.json)。


[已选 B 的完整报告](http://127.0.0.1:4173/design-preview?paper=white&density=compact#overview)；默认、无效参数、历史对照与响应式验收见 [selected-paper-checks.json](../preview/selected-paper-checks.json)。v0.16 截图保留原比较文案，图版的纸色和层次与已选 B 相同。


## 数据配色已选定 A2 · 胭脂湖蓝（v0.19）

用户明确选定 A2 · 胭脂湖蓝，认为更接近熟悉的股票图表配色。报告曲线、图例及表格标记统一采用胭脂红、湖蓝、赭金、灰紫、松绿；月收益热力图采用湖蓝负值、近白零值、胭脂红正值。定稿参数与历史对照见 DESIGN.md 第 16 节。

样板曲线已减细为主线 1.6px、比较线 1.2px，悬停保持线宽；图例同步，Lucide 图标改为 1.5 线宽。点击区域和已定字体、紧凑排版、B 纸面保持。


A2 定稿截图：[折线图](../preview/selected-palette-line.png) / [热力图](../preview/selected-palette-heatmap.png)；[验证记录](../preview/selected-palette-checks.json)。


## 当前：组件样式与交互试用（v0.20，待确认）

打开[组件试用区](http://127.0.0.1:4173/design-preview#components)。这一版是一套推荐方案：观察清单支持搜索、共享观察周期、排序、选择、详情弹窗和导出；旁边的金额换算可以体验输入校验。上方可直接切换正常、加载、空数据和失败状态，操作后可恢复。

建议体验：搜索“黄金”并查看详情；取消全部选择观察按钮禁用态；输入金额 0 查看错误，再输入 100,000 计算；切到失败状态后重新加载。桌面和手机均可用，颜色与字体沿用已选定方案，组件样式尚待用户确认。


最终截图：[桌面](../preview/component-study-desktop.png) / [手机](../preview/component-study-mobile.png) / [详情弹窗](../preview/component-study-dialog.png) / [错误状态](../preview/component-study-error.png)；[交互与响应式检查](../preview/component-study-checks.json)。
