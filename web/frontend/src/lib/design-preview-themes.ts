import type { CSSProperties } from "react";

// Interface ink and figure colors have separate roles. Earlier color studies
// remain as screenshots in docs/design/preview, not as competing brand themes.
export const researchTheme = {
  version: "1.0",
  density: "compact",
  surface: {
    id: "white",
    canvas: "#FFFFFF",
    paper: "#FFFFFF",
    soft: "#F3F3F3",
    rule: "#E3E3E3",
    edge: "#DEDEDE",
    radius: "12px",
    shadow: "none",
  },
  typography: {
    chinese: '"Noto Serif SC Variable", "Songti SC", serif',
    english: '"Source Serif 4", serif',
    numbers: '"Source Serif 4", "Noto Sans SC Variable", serif',
    titleWeight: 300,
    readingWeight: 400,
  },
  ink: "#242420",
  muted: "#69665F",
  // Figure grid is quieter than data marks.
  rule: "#DAD6CC",
  controlBorder: "#8B867C",
  // Selected A2: rouge / lake blue. Series identity is separate from direction.
  series: ["#C4435B", "#287E9A", "#927A29", "#75629E", "#3E8A72"],
  // -6 / -3 / 0 / +3 / +6%; blue negative, rouge positive.
  heatmap: ["#287E9A", "#8FBAC7", "#F5F5F3", "#DD9CA7", "#C4435B"],
} as const;

export function previewThemeStyle(): CSSProperties {
  const { surface, typography } = researchTheme;
  return {
    "--display-font": typography.chinese,
    "--english-font": typography.english,
    "--number-font": typography.numbers,
    "--report-title-weight": typography.titleWeight,
    "--report-reading-weight": typography.readingWeight,
    "--canvas": surface.canvas,
    "--paper": surface.paper,
    "--paper-soft": surface.soft,
    "--ink": researchTheme.ink,
    "--ink-muted": researchTheme.muted,
    "--rule": surface.rule,
    "--paper-edge": surface.edge,
    "--paper-radius": surface.radius,
    "--paper-shadow": surface.shadow,
    "--accent": researchTheme.ink,
    "--accent-soft": surface.soft,
    "--control-border": researchTheme.controlBorder,
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
