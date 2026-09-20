import { useEffect, useRef } from "react";
import * as echarts from "echarts/core";
import { LineChart, BarChart } from "echarts/charts";
import {
  GridComponent,
  AxisPointerComponent,
  AriaComponent,
} from "echarts/components";
import { SVGRenderer } from "echarts/renderers";
import type { EChartsCoreOption } from "echarts/core";
echarts.use([
  LineChart,
  BarChart,
  GridComponent,
  AxisPointerComponent,
  AriaComponent,
  SVGRenderer,
]);

// Chart geometry and the shared date cursor use the same trading-day index.
// React controls the visible range, so metrics and all three charts change together.
export function RotationChart({
  option,
  label,
  count,
  active,
  onInspect,
  onRange,
  compact = false,
}: {
  option: EChartsCoreOption;
  label: string;
  count: number;
  active: number;
  onInspect: (index: number) => void;
  onRange: (start: number, end: number) => void;
  compact?: boolean;
}) {
  const node = useRef<HTMLDivElement>(null);
  const instance = useRef<echarts.ECharts | null>(null);
  const drag = useRef<number | null>(null);
  useEffect(() => {
    const chart = echarts.init(node.current!, undefined, { renderer: "svg" });
    instance.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(node.current!);
    let disposed = false;
    void document.fonts.ready.then(() => {
      if (!disposed) chart.resize();
    });
    return () => {
      disposed = true;
      observer.disconnect();
      chart.dispose();
      instance.current = null;
    };
  }, []);
  useEffect(() => {
    instance.current?.setOption(option, true);
  }, [option]);
  useEffect(() => {
    const chart = instance.current;
    if (!chart) return;
    chart.dispatchAction({
      type: "updateAxisPointer",
      currTrigger: "mousemove",
      x: chart.convertToPixel({ xAxisIndex: 0 }, active),
      y: 24,
      axesInfo: [{ axisDim: "x", axisIndex: 0, value: active }],
    });
  }, [active, option]);
  function index(e: React.PointerEvent<HTMLDivElement>) {
    const box = e.currentTarget.getBoundingClientRect();
    return Math.max(
      0,
      Math.min(
        count - 1,
        Math.round(
          ((e.clientX - box.left - 52) / Math.max(1, box.width - 68)) *
            (count - 1),
        ),
      ),
    );
  }
  return (
    <div
      ref={node}
      role="img"
      aria-label={label}
      className={
        compact ? "rotation-plot rotation-plot-small" : "rotation-plot"
      }
      onPointerMove={(e) => {
        if (e.pointerType === "mouse" || drag.current !== null)
          onInspect(index(e));
      }}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        onInspect(index(e));
        if (e.pointerType !== "mouse") return; // Preserve vertical touch scrolling; date controls provide touch zoom.
        drag.current = index(e);
        e.currentTarget.setPointerCapture(e.pointerId);
      }}
      onPointerCancel={() => {
        drag.current = null;
      }}
      onPointerUp={(e) => {
        const start = drag.current;
        drag.current = null;
        if (start !== null && Math.abs(index(e) - start) > 2)
          onRange(Math.min(start, index(e)), Math.max(start, index(e)));
      }}
    />
  );
}
