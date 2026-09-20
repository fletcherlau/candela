# Frontend design

创建或调整页面、组件和数据可视化时，先阅读 [DESIGN.md](../../docs/design/DESIGN.md)，采用其中的 v1.0 定稿基线。历史探索记录不作为当前样式依据，也不重新加入字体、纸面或配色选择器。

复用现有 shadcn/ui、固定主题和图表组件；根据当前任务逐页实施。视觉变更需在浏览器中检查实际字体、响应式布局和相关交互，参考 [样板入口与验收记录](../../docs/design/preview/README.md)。

参数维护遵循 Google DESIGN.md 格式：先改规范 YAML，运行 `npm run design:generate` 和 `npm run design:check`；不要手改生成的参数文件。新页面通过 ResearchTheme 接入共享样式，参见 [组件接入说明](../../docs/design/components.md)。
