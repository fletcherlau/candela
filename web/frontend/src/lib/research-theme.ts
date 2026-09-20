import type { CSSProperties } from "react";
import { designTokens as tokens } from "./design-tokens.generated";

const c = tokens.colors;
const t = tokens.typography;
const s = tokens.spacing;

// Public theme adapter. Business data and chart semantics remain outside it.
export const researchTheme = {
  version: "1.0",
  density: "compact",
  surface: {
    id: "white", canvas: c.canvas, paper: c.paper, soft: c["paper-soft"],
    rule: c.rule, edge: c["paper-edge"], radius: tokens.rounded.paper, shadow: "none",
  },
  typography: {
    chinese: t["headline-zh"].fontFamily,
    english: t["headline-en"].fontFamily,
    numbers: t.metric.fontFamily,
    interface: t.label.fontFamily,
    titleWeight: t["headline-zh"].fontWeight,
    readingWeight: t["body-zh"].fontWeight,
  },
  ink: c.primary, muted: c.secondary, rule: c.grid,
  controlBorder: c["control-border"],
  series: [c.rouge, c.lake, c.gold, c.violet, c.pine],
  heatmap: [c.lake, c["heatmap-negative-mid"], c["heatmap-zero"], c["heatmap-positive-mid"], c.rouge],
  lineWidth: {
    primary: parseFloat(tokens.components["chart-primary"].width),
    comparison: parseFloat(tokens.components["chart-comparison"].width),
  },
} as const;

export function researchThemeStyle(): CSSProperties {
  return {
    "--display-font": t["headline-zh"].fontFamily,
    "--english-font": t["headline-en"].fontFamily,
    "--number-font": t.metric.fontFamily,
    "--brand-font": t.brand.fontFamily,
    "--body-font": t.label.fontFamily,
    "--report-title-weight": t["headline-zh"].fontWeight,
    "--report-reading-weight": t["body-zh"].fontWeight,
    "--canvas": c.canvas, "--paper": c.paper, "--paper-soft": c["paper-soft"],
    "--ink": c.primary, "--ink-muted": c.secondary, "--rule": c.rule,
    "--paper-edge": c["paper-edge"], "--paper-radius": tokens.rounded.paper,
    "--paper-shadow": "none", "--accent": c.primary, "--accent-soft": c["paper-soft"],
    "--control-border": c["control-border"], "--error": c.error,
    "--primary-hover": c["primary-hover"], "--control-radius": tokens.rounded.control,
    "--small-radius": tokens.rounded.small, "--control-height": s["control-height"],
    "--focus-width": s["focus-width"], "--focus-offset": s["focus-offset"],
    "--icon-stroke": parseFloat(tokens.components.icon.width),
    "--title-size": t["headline-zh"].fontSize,
    "--title-mobile-size": t["headline-zh-mobile"].fontSize,
    "--english-title-size": t["headline-en"].fontSize,
    "--english-title-mobile-size": t["headline-en-mobile"].fontSize,
    "--reading-size": t["body-zh"].fontSize,
    "--reading-line": t["body-zh"].lineHeight,
    "--english-reading-line": t["body-en"].lineHeight,
    "--section-size": t.section.fontSize, "--figure-size": t.figure.fontSize,
    "--metric-size": t.metric.fontSize, "--table-number-size": t["table-number"].fontSize,
    "--input-size": t.input.fontSize, "--label-size": t.label.fontSize,
    "--layout-shell": s.shell, "--layout-reading": s.reading,
    "--layout-aside": s.aside, "--layout-column-gap": s["column-gap"],
    "--page-padding": s["page-desktop"], "--page-mobile-padding": s["page-mobile"],
    "--card-padding": s["card-desktop"], "--card-mobile-padding": s["card-mobile"],
    "--section-space": s["section-desktop"], "--section-mobile-space": s["section-mobile"],
    "--table-padding": s["table-y"], "--chart-height": tokens.components["chart-primary"].height,
    "--dialog-padding": tokens.components.dialog.padding,
  } as CSSProperties;
}

// Match the linear RGB interpolation of the -6..6 heatmap. Keep each label
// readable across both saturated ends of the scale, including intermediate fills.
export function heatmapLabelColor(value: number): string {
  const position = Math.max(0, Math.min(4, (value + 6) / 3));
  const index = Math.min(3, Math.floor(position));
  const fraction = position - index;
  const rgb = (hex: string) =>
    [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const left = rgb(researchTheme.heatmap[index]);
  const right = rgb(researchTheme.heatmap[index + 1]);
  const linear = left.map((channel, i) => {
    const s = (channel + (right[i] - channel) * fraction) / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  const luminance =
    linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
  return luminance < 0.179 ? "#FFFFFF" : "#000000";
}
