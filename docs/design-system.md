# DevTopo - design system

The visual foundation: themes, palettes, and the app shell. Hex values are the
source of truth to build tokens from; exact token names are an implementation
detail.

## Themes

- **[Decided] Light and dark, both first-class.** Follow the OS by default, with a
  manual toggle.

## Light palette [Decided direction]

- Base: **white / very light gray, almost white** (e.g. page `#ffffff`, raised
  surfaces `#ffffff`, sidebar / panels a light gray `#e5ebeb`, so white cards stand out on them).
- Structure: **grays** for borders and secondary text (`#e4e8e8` borders, text
  `#1c2524`, dim text `#5c6a68`). [Proposed exact values]
- Accent: **turquoise** (around `#13b5a6`, with a slightly darker shade for text /
  small elements so it stays legible on white). [Proposed exact value]

## Dark palette - Dracula [Decided]

Use the Dracula palette (the one from the VS Code theme). Reference hexes:

- Background `#282a36`, panel / lighter bg `#343746`, current-line / selection
  `#44475a`, foreground `#f8f8f2`, comment / dim `#6272a4`.
- Cyan `#8be9fd`, green `#50fa7b`, orange `#ffb86c`, pink `#ff79c6`, purple
  `#bd93f9`, red `#ff5555`, yellow `#f1fa8c`.
- **[Decided] Accent = Dracula cyan `#8be9fd`**, to echo the light theme's turquoise
  and keep one accent identity across both themes. Purple was tried as the accent
  and was too much; it stays for small tags only (the branch on a session card).
- **[Decided] Depth only goes one way: dark around, light inside.** The page and
  the frame around it (nav, status bar) are the darkest `#282a36`; panels that
  hold text are lighter `#343746`; anything inside a panel (lists, chips, inputs,
  timeline lanes) is lighter again `#424559`, never darker. Borders `#555970`.
- **[Decided] Calendar blocks read strongly**: kind colour at 60% fill, no fading
  of past or done blocks. Small text on a bright block (time, ticket key) uses
  near-black `#191a21`; meetings, on a muted fill, keep the foreground.
- These rules are dark only, written as `dark:` classes and dark-only tokens, so
  light renders exactly as it did.

## Semantic colors [Proposed]

Kept separate from the accent. In dark, map to Dracula; in light, to legible
equivalents. Snag stays de-fanged (orange, not red):

- done / on-track: green (`#50fa7b` dark).
- waiting: orange / yellow (`#ffb86c` dark).
- snag: soft orange, never alarm-red.
- running / info: cyan / the accent.
- red (`#ff5555`) reserved for genuine errors only, used sparingly. Token
  `danger` (`#c62828` light): also a Claude usage limit over 85%, about to block.

## App shell [Decided]

Follow the familiar pattern from Claude, Orca, Langfuse:

- **Left menu bar, collapsible.** Primary navigation (today, planner, projects,
  settings) down the left, collapsible to icons-only.
- **Lower status bar.** Daemon status, active-session count, last checkpoint, and
  the day's AI-hours live along the bottom.
- **Main content area** to the right of the nav.
- Clean, dense-but-calm, developer-tool feel - at home next to those apps.

## Open questions

- **Typography**: faces for UI and for data / numbers. Not yet chosen.
- **Accent in dark**: cyan (echoes turquoise) vs Dracula purple (signature look).
- **Exact light-theme hexes**: the turquoise and gray values above are a starting
  point to tune on real screens.
