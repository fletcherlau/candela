# 设计规范到组件

视觉基线仍是用户确认的 v1.0。此次把 Google DESIGN.md `alpha` 格式接入工程，不改变字体、颜色或内容布局的选择。

## 参数流向

`DESIGN.md` YAML → `npm run design:generate` → `design-tokens.generated.ts` → `research-theme.ts` → `ResearchTheme` / shadcn / ECharts。

YAML 是参数来源；生成文件提交到仓库，浏览器不解析 YAML。正文保存设计意图、交互契约和响应式规则。官方 CLI 与 YAML 解析器只作为开发依赖，版本锁定在 package-lock.json。

在 `web/frontend` 执行：

```sh
npm run design:generate
npm run design:check
npm run build
npm run test:design
```

修改参数后先生成再检查。构建会检查官方格式与生成文件一致性；发现陈旧参数会失败，不会静默覆盖。

## 页面接入

```tsx
import { ResearchTheme } from "@/components/research-theme";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

<ResearchTheme>
  <Button>主要操作</Button>
  <Button variant="outline">次要操作</Button>
  <Input aria-label="搜索标的" />
</ResearchTheme>
```

实际表单使用 Field / FieldLabel 提供持久标签。页面通过 `ResearchTheme` 加载本地字体、纸面、字重与布局变量；对外提供 `researchTheme` 给图表读取数值参数。不要复制样板的内联主题或参数表。

Dialog 仍使用原有 Dialog / DialogTrigger / DialogContent / DialogTitle 组合；位于 ResearchTheme 内时，React 上下文会把主题传入 portal 内容。无需额外设置 className 或 style。范围外的现有页面及弹窗保持原主题。

## 本轮落实范围

| 项目 | 实现与验证 |
| --- | --- |
| 字体、颜色与图表系列 | 从文档参数生成；图表使用共享颜色与线宽；字体由主题入口加载 |
| Button | 墨色主按钮、中性次按钮、错误语义色；Tailwind 通过可继承变量取色；4px 圆角、44px 最小高度；图标按钮保持点击宽度 |
| Input | 纸面、控件细边、16px 字号、44px 高度；圆角由旧 3px 修正为规范的 4px |
| Card | 12px 纸面圆角、无投影、20px 桌面内边距及衬线标题，不要求套在样板专属网格中 |
| Checkbox | 选中墨色、细边与部分选择状态可复用；键盘空格切换验证。业务布局仍须提供 44px 标签点击区域 |
| Table / ToggleGroup | 共用墨色、弱底和细分隔；表格行距与分段选中样式按当前规范；数字单元使用 `research-number` |
| Dialog | 自动跨 portal 继承；12px 白纸、无默认投影、细宋标题、细线图标；关闭按钮点击区至少 44px |
| 隔离 | 独立测试页同时渲染研究主题与旧主题；旧按钮颜色及旧弹窗保持不变 |

这不是对所有 shadcn 组件的全面改造。未接入的业务页面仍走既有主题；新增 Select、Popover、Tooltip 等 portal 组件时，需要按 Dialog 的方式接入主题并验证。页面级内容层级、业务状态与数据口径由具体任务决定。

## 校验说明

使用 `@google/design.md` 0.4.0；官方规范见 [Google DESIGN.md](https://github.com/google-labs-code/design.md/blob/main/docs/spec.md)。当前官方 lint 为 0 errors；存在 13 条 `orphaned-tokens` 警告：画布、边缘、分隔线、控件边界、网格及数据色由 TypeScript/CSS/ECharts 消费，而非 YAML components 的支持字段。没有为了消除警告添加虚构组件或重复参数。

描边按钮背景保持透明，正文记录其叠加在纸面上的实际对比度要求。YAML 不为透明背景指定独立颜色对；浏览器检查实际页面，不将透明度当成黑色进行对比。

自动浏览器测试包含独立组件、portal 继承、焦点恢复、键盘操作以及 1440 / 820 / 390 / 320px 样板检查。过程截图放在临时目录；仓库仍仅保留原来的三张定稿视觉基准。
