import { useCallback, useState } from "react";

export interface ThemeOption {
  id: string;
  label: string;
  accent: string;
  background: string;
}

const THEMES: ThemeOption[] = [
  { id: "zinc", label: "Zinc", accent: "#a1a1aa", background: "#09090b" },
  {
    id: "indigo-night",
    label: "Indigo Night",
    accent: "#818cf8",
    background: "#12131f",
  },
  {
    id: "warm-slate",
    label: "Warm Slate",
    accent: "#d4a574",
    background: "#141210",
  },
  {
    id: "ocean-depth",
    label: "Ocean Depth",
    accent: "#2dd4bf",
    background: "#0f171a",
  },
  {
    id: "muted-violet",
    label: "Muted Violet",
    accent: "#a78bfa",
    background: "#13131b",
  },
  {
    id: "gunmetal",
    label: "Gunmetal",
    accent: "#60a5fa",
    background: "#131517",
  },
  {
    id: "rosewood",
    label: "Rosewood",
    accent: "#f0abab",
    background: "#161314",
  },
  {
    id: "forest-floor",
    label: "Forest Floor",
    accent: "#34d399",
    background: "#101614",
  },
  { id: "dusk", label: "Dusk", accent: "#c4b5fd", background: "#151318" },
  {
    id: "graphite",
    label: "Graphite",
    accent: "#fbbf24",
    background: "#121416",
  },
  {
    id: "obsidian-sand",
    label: "Obsidian Sand",
    accent: "#e8935a",
    background: "#14120f",
  },
];

const STORAGE_KEY = "topham-theme";

function getInitialTheme(): string {
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored ?? "zinc";
}

export function useTheme() {
  const [theme, setThemeState] = useState<string>(getInitialTheme);

  const setTheme = useCallback((name: string) => {
    if (name === "" || name === "zinc") {
      document.documentElement.removeAttribute("data-theme");
      localStorage.removeItem(STORAGE_KEY);
      setThemeState("zinc");
    } else {
      document.documentElement.setAttribute("data-theme", name);
      localStorage.setItem(STORAGE_KEY, name);
      setThemeState(name);
    }
  }, []);

  return { theme, setTheme, themes: THEMES };
}
