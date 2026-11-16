import { useEffect, useState } from 'react'

// Token maps from docs/design-system.md. Light is white / near-white gray /
// turquoise; dark is Dracula. accentText is a legible-on-background accent.
export const light = {
  bg: '#ffffff',
  panel: '#e5ebeb',
  raised: '#ffffff',
  border: '#ccd6d5',
  fg: '#141c1b',
  dim: '#4c5a58',
  accent: '#13b5a6',
  accentText: '#0f9488',
  running: '#0f9488',
  waiting: '#c06a00',
  idle: '#5f6f8a',
  done: '#16a34a',
  danger: '#c62828',
  tint: '40%',
  // Field colours on session cards: the branch, AI hours, and the agent tags.
  tag: '#7c3aed',
  aiH: '#7a6000',
  claude: '#a34a2c',
  codex: '#3b5bdb',
} as const

export const dark = {
  // Depth only goes up: the page (and the nav and status bar around it) is the
  // darkest, panels holding text are lighter, controls inside a panel lighter again.
  bg: '#282a36',
  panel: '#343746',
  raised: '#424559',
  border: '#555970',
  fg: '#f8f8f2',
  // Lighter than Dracula's comment grey, which is too faint for text on panel.
  dim: '#a4acd0',
  accent: '#8be9fd',
  accentText: '#8be9fd',
  running: '#8be9fd',
  waiting: '#ffb86c',
  idle: '#7d89b9',
  done: '#50fa7b',
  danger: '#ff5555',
  // How strongly a block's kind colour fills it; dark needs more to read.
  tint: '60%',
  // Text on a bright block fill (ticket, review, own); meetings keep fg.
  blockInk: '#191a21',
  // Field colours on session cards: the branch, AI hours, and the agent tags.
  tag: '#bd93f9',
  aiH: '#f1fa8c',
  claude: '#eb8f6e',
  codex: '#9cb0ff',
} as const

export type ThemeName = 'light' | 'dark'

const STORAGE_KEY = 'devtopo-theme'

function systemPrefersDark(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
}

function storedTheme(): ThemeName | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : null
  } catch {
    return null
  }
}

// applyTheme writes the token map onto :root as CSS variables, so Tailwind
// utilities and inline styles both follow the active theme.
export function applyTheme(name: ThemeName): void {
  const tokens = name === 'dark' ? dark : light
  const root = document.documentElement
  for (const [k, v] of Object.entries(tokens)) {
    root.style.setProperty(`--${k}`, v)
  }
  root.dataset.theme = name
}

// useTheme returns the active theme and a setter. It follows the OS preference
// by default, with a manual override persisted to localStorage (best-effort).
export function useTheme(): [ThemeName, (n: ThemeName) => void] {
  const [name, setName] = useState<ThemeName>(
    () => storedTheme() ?? (systemPrefersDark() ? 'dark' : 'light'),
  )
  useEffect(() => {
    applyTheme(name)
    try {
      localStorage.setItem(STORAGE_KEY, name)
    } catch {
      // ignore: a per-viewer convenience, not required state
    }
  }, [name])
  return [name, setName]
}
