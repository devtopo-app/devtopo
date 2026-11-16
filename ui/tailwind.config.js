/** @type {import('tailwindcss').Config} */
export default {
  // dark: utilities follow the theme theme.ts sets on <html>, not the OS.
  darkMode: ['selector', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    // The one type scale: use these, never a pixel size inline, so text that
    // plays the same role has the same size everywhere.
    fontSize: {
      '2xs': ['11px', '14px'], // tiny labels: units, badges, block times
      xs: ['12.5px', '17px'], // secondary text, meta, pills, block titles
      sm: ['14px', '20px'], // body: card titles, buttons
      base: ['15px', '22px'], // inputs, the plan name
      lg: ['17px', '24px'],
      xl: ['20px', '28px'], // page titles
    },
    extend: {
      fontFamily: {
        sans: ["'Manrope Variable'", 'system-ui', '-apple-system', "'Segoe UI'", 'Roboto', 'sans-serif'],
      },
      // Semantic colors read the CSS variables set by theme.ts, so Tailwind
      // utilities follow the active theme.
      colors: {
        bg: 'var(--bg)',
        panel: 'var(--panel)',
        raised: 'var(--raised)',
        tag: 'var(--tag)',
        aiH: 'var(--aiH)',
        claude: 'var(--claude)',
        codex: 'var(--codex)',
        border: 'var(--border)',
        fg: 'var(--fg)',
        dim: 'var(--dim)',
        accent: 'var(--accent)',
        running: 'var(--running)',
        waiting: 'var(--waiting)',
        idle: 'var(--idle)',
        done: 'var(--done)',
        danger: 'var(--danger)',
      },
    },
  },
  plugins: [],
}
