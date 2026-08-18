# industry-explorer

申万行业指数（2021 版）浏览前端：一级/二级行业选择、K 线（日/周/月）、拖动平移、滚轮缩放、全市场指数快速切换。

## 技术栈

Vite + React 19 + TypeScript + Tailwind + shadcn/ui（Base UI 版）+ Recharts。

## 准备数据

行情与行业分类来自 `smallcap-rotation/data/` 下的申万日线 CSV 和分类 JSON，先转成前端静态 JSON（输出到 `public/data/`，已 gitignore）：

```bash
../../.venv/bin/python scripts/build_data.py
```

## 开发

```bash
npm install
npm run dev        # 默认 5173, 可用 -- --port 5199 --strictPort
npm run build      # tsc -b && vite build
```

## 交互说明

- 一级/二级行业下拉；右上 `‹ ›` 按钮按拼音序切换全部 156 个指数（跨界时一级下拉自动跟随）
- 日/周/月线切换；近1年/3年/5年/全部只是预设窗口，不限制缩放和平移
- 图表上左键拖动平移，滚轮以鼠标为锚点缩放；再点时间档按钮复位窗口
