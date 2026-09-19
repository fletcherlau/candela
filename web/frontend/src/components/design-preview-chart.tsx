import { useEffect, useRef } from "react";
import * as echarts from "echarts/core";
import { LineChart, HeatmapChart } from "echarts/charts";
import {
  GridComponent,
  TooltipComponent,
  VisualMapComponent,
  AriaComponent,
} from "echarts/components";
import { SVGRenderer } from "echarts/renderers";
import type { EChartsCoreOption } from "echarts/core";
echarts.use([
  LineChart,
  HeatmapChart,
  GridComponent,
  TooltipComponent,
  VisualMapComponent,
  AriaComponent,
  SVGRenderer,
]);
export function DesignPreviewChart({
  option,
  label,
  kind = "line",
}: {
  option: EChartsCoreOption;
  label: string;
  kind?: "line" | "heatmap";
}) {
  const element = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = element.current!;
    const chart = echarts.init(node, undefined, { renderer: "svg" });
    chart.setOption(option);
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(node);
    let disposed = false;
    void document.fonts.ready.then(() => {
      if (!disposed) chart.resize();
    });
    return () => {
      disposed = true;
      observer.disconnect();
      chart.dispose();
    };
  }, [option]);
  return (
    <div
      ref={element}
      className={"research-chart research-chart-" + kind}
      role="img"
      aria-label={label}
    />
  );
}
