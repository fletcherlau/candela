// Generated from docs/design/DESIGN.md. Run npm run design:generate; do not edit.
export const designTokens = {
  "version": "alpha",
  "name": "Candela Research",
  "description": "紧凑研究报告、细宋、Source Serif 4、净白纸面、胭脂湖蓝图表；设计基线 v1.0。",
  "colors": {
    "primary": "#242420",
    "secondary": "#69665F",
    "canvas": "#FFFFFF",
    "paper": "#FFFFFF",
    "paper-soft": "#F3F3F3",
    "paper-edge": "#DEDEDE",
    "rule": "#E3E3E3",
    "control-border": "#8B867C",
    "grid": "#DAD6CC",
    "error": "#A33343",
    "primary-hover": "#43423D",
    "rouge": "#C4435B",
    "lake": "#287E9A",
    "gold": "#927A29",
    "violet": "#75629E",
    "pine": "#3E8A72",
    "heatmap-negative-mid": "#8FBAC7",
    "heatmap-zero": "#F5F5F3",
    "heatmap-positive-mid": "#DD9CA7"
  },
  "typography": {
    "headline-zh": {
      "fontFamily": "\"Noto Serif SC Variable\", \"Songti SC\", serif",
      "fontSize": "36px",
      "fontWeight": 300,
      "lineHeight": 1.5
    },
    "headline-zh-mobile": {
      "fontFamily": "\"Noto Serif SC Variable\", \"Songti SC\", serif",
      "fontSize": "26px",
      "fontWeight": 300,
      "lineHeight": 1.5
    },
    "headline-en": {
      "fontFamily": "\"Source Serif 4\", serif",
      "fontSize": "30px",
      "fontWeight": 300,
      "lineHeight": 1.3
    },
    "headline-en-mobile": {
      "fontFamily": "\"Source Serif 4\", serif",
      "fontSize": "25px",
      "fontWeight": 300,
      "lineHeight": 1.3
    },
    "body-zh": {
      "fontFamily": "\"Noto Serif SC Variable\", \"Songti SC\", serif",
      "fontSize": "16px",
      "fontWeight": 400,
      "lineHeight": 1.8
    },
    "body-en": {
      "fontFamily": "\"Source Serif 4\", serif",
      "fontSize": "16px",
      "fontWeight": 400,
      "lineHeight": 1.7
    },
    "section": {
      "fontFamily": "\"Noto Serif SC Variable\", \"Songti SC\", serif",
      "fontSize": "23px",
      "fontWeight": 300,
      "lineHeight": 1.6
    },
    "figure": {
      "fontFamily": "\"Noto Serif SC Variable\", \"Songti SC\", serif",
      "fontSize": "21px",
      "fontWeight": 300,
      "lineHeight": 1.5
    },
    "metric": {
      "fontFamily": "\"Source Serif 4\", \"Noto Sans SC Variable\", serif",
      "fontSize": "36px",
      "fontWeight": 400,
      "lineHeight": 1.3,
      "fontFeature": "\"lnum\" 1, \"tnum\" 1"
    },
    "table-number": {
      "fontFamily": "\"Source Serif 4\", \"Noto Sans SC Variable\", serif",
      "fontSize": "14px",
      "fontWeight": 400,
      "lineHeight": 1.5,
      "fontFeature": "\"lnum\" 1, \"tnum\" 1"
    },
    "label": {
      "fontFamily": "\"Noto Sans SC Variable\", \"PingFang SC\", sans-serif",
      "fontSize": "12px",
      "fontWeight": 400,
      "lineHeight": 1.5
    },
    "input": {
      "fontFamily": "\"Noto Sans SC Variable\", \"PingFang SC\", sans-serif",
      "fontSize": "16px",
      "fontWeight": 400,
      "lineHeight": 1.5
    },
    "brand": {
      "fontFamily": "\"Alegreya\", Georgia, serif",
      "fontSize": "42px",
      "fontWeight": 500,
      "lineHeight": 1
    }
  },
  "rounded": {
    "control": "4px",
    "small": "3px",
    "paper": "12px"
  },
  "spacing": {
    "xs": "4px",
    "sm": "8px",
    "md": "16px",
    "lg": "24px",
    "xl": "32px",
    "shell": "1280px",
    "reading": "780px",
    "aside": "190px",
    "column-gap": "40px",
    "page-desktop": "48px",
    "page-mobile": "20px",
    "card-desktop": "20px",
    "card-mobile": "16px",
    "section-desktop": "44px",
    "section-mobile": "32px",
    "table-y": "10px",
    "control-height": "44px",
    "focus-width": "2px",
    "focus-offset": "4px"
  },
  "components": {
    "button-primary": {
      "backgroundColor": "{colors.primary}",
      "textColor": "{colors.paper}",
      "typography": "{typography.label}",
      "rounded": "{rounded.control}",
      "height": "{spacing.control-height}"
    },
    "button-primary-hover": {
      "backgroundColor": "{colors.primary-hover}",
      "textColor": "{colors.paper}"
    },
    "button-outline": {
      "textColor": "{colors.primary}",
      "rounded": "{rounded.control}",
      "height": "{spacing.control-height}"
    },
    "button-secondary": {
      "backgroundColor": "{colors.paper-soft}",
      "textColor": "{colors.primary}",
      "rounded": "{rounded.control}",
      "height": "{spacing.control-height}"
    },
    "input": {
      "backgroundColor": "{colors.paper}",
      "textColor": "{colors.primary}",
      "typography": "{typography.input}",
      "rounded": "{rounded.control}",
      "height": "{spacing.control-height}"
    },
    "card": {
      "backgroundColor": "{colors.paper}",
      "textColor": "{colors.primary}",
      "rounded": "{rounded.paper}",
      "padding": "{spacing.card-desktop}"
    },
    "dialog": {
      "backgroundColor": "{colors.paper}",
      "textColor": "{colors.primary}",
      "rounded": "{rounded.paper}",
      "padding": "30px"
    },
    "checkbox": {
      "backgroundColor": "{colors.paper}",
      "textColor": "{colors.primary}",
      "rounded": "{rounded.small}",
      "size": "18px"
    },
    "checkbox-checked": {
      "backgroundColor": "{colors.primary}",
      "textColor": "{colors.paper}"
    },
    "chart-primary": {
      "width": "1.6px",
      "height": "300px"
    },
    "chart-comparison": {
      "width": "1.2px"
    },
    "icon": {
      "size": "24px",
      "width": "1.5px"
    }
  }
} as const;
