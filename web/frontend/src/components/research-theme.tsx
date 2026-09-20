import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { ResearchThemeContext } from "@/lib/research-theme-context";
import { researchTheme, researchThemeStyle } from "@/lib/research-theme";
import "@fontsource/source-serif-4/latin-300.css";
import "@fontsource/source-serif-4/latin-400.css";
import "@fontsource/source-serif-4/latin-500.css";
import "@fontsource/alegreya/latin-400.css";
import "@fontsource/alegreya/latin-500.css";
import "@fontsource-variable/noto-sans-sc";
import "@fontsource-variable/noto-serif-sc";

export function ResearchTheme({ className, style, ...props }: ComponentProps<"div">) {
  return (
    <ResearchThemeContext.Provider value={true}>
      <div
        {...props}
        className={cn("research-theme", className)}
        style={{ ...style, ...researchThemeStyle() }}
        data-design-version={researchTheme.version}
        data-density={researchTheme.density}
      />
    </ResearchThemeContext.Provider>
  );
}
