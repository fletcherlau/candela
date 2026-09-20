import { createContext } from "react";

// React context crosses portals; plain pages keep their existing theme.
export const ResearchThemeContext = createContext(false);
