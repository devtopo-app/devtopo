// The sticker pictures: small illustrations in a 64-unit box, as SVG markup.
// They are this file's own constants, never user input. Each has a default
// name and a rim in one of the app's colours, so the rims follow the theme.

const L = 'stroke="#233140" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round"'
const STEAM = '#94a3b8'

export interface Pic {
  name: string
  rim: string
  svg: string
  // kind is the sticker kind the picture suggests; counter when unset.
  kind?: 'counter' | 'done'
}

export const PICS: Record<string, Pic> = {
  water: {
    name: 'Water',
    rim: 'var(--running)',
    svg: `<path d="M50 24c0 0-4.5 5.4-4.5 8.2a4.5 4.5 0 0 0 9 0C54.5 29.4 50 24 50 24Z" fill="#38bdf8" ${L} stroke-width="1.6"/>
      <path d="M15 9h30l-4.2 45.5a4.5 4.5 0 0 1-4.5 4.1H23.7a4.5 4.5 0 0 1-4.5-4.1Z" fill="url(#sg-glass)"/>
      <path d="M17.4 25.5c4.2-2.6 8.4 2.2 12.6 0s8.4-2.6 12.6 0l-2.8 29a4.5 4.5 0 0 1-4.5 4.1H23.7a4.5 4.5 0 0 1-4.5-4.1Z" fill="url(#sg-water)"/>
      <rect x="21" y="30" width="8" height="8" rx="1.6" transform="rotate(-14 25 34)" fill="#f0f9ff" fill-opacity=".85" stroke="#bae6fd" stroke-width="1.2"/>
      <rect x="30.5" y="35.5" width="7" height="7" rx="1.4" transform="rotate(12 34 39)" fill="#f0f9ff" fill-opacity=".8" stroke="#bae6fd" stroke-width="1.2"/>
      <circle cx="27" cy="48" r="1.4" fill="#e0f2fe"/><circle cx="34" cy="51" r="1" fill="#e0f2fe"/><circle cx="31" cy="45" r=".9" fill="#e0f2fe"/>
      <path d="M19.5 13l3 37" stroke="#fff" stroke-opacity=".7" stroke-width="2.4" stroke-linecap="round"/>
      <path d="M15 9h30l-4.2 45.5a4.5 4.5 0 0 1-4.5 4.1H23.7a4.5 4.5 0 0 1-4.5-4.1Z" fill="none" ${L}/>`,
  },
  coffee: {
    name: 'Coffee',
    rim: 'var(--waiting)',
    svg: `<path d="M23 17c-3-3.6 3-5.4 0-10M31 16c-3-3.6 3-5.4 0-10M39 17c-3-3.6 3-5.4 0-10" fill="none" stroke="${STEAM}" stroke-width="2.2" stroke-linecap="round"/>
      <ellipse cx="32" cy="53" rx="24" ry="5.5" fill="#e7e5e4" ${L}/>
      <path d="M12 23h36v13a15 15 0 0 1-15 15h-6a15 15 0 0 1-15-15Z" fill="url(#sg-cup)" ${L}/>
      <path d="M48 27h3.5a6.5 6.5 0 0 1 0 13H47" fill="none" ${L} stroke-width="3"/>
      <path d="M12.6 31h34.8" stroke="#13b5a6" stroke-width="3.2"/>
      <ellipse cx="30" cy="23" rx="18" ry="3.6" fill="#6b3a1f" ${L}/>
      <path d="M23 22.4c3.5 1.4 8.5 1.4 12 0" stroke="#c08457" stroke-width="1.4" stroke-linecap="round" fill="none"/>
      <path d="M17 35c0 5 3 9.5 7.5 11" stroke="#fff" stroke-opacity=".9" stroke-width="2.4" stroke-linecap="round" fill="none"/>`,
  },
  walk: {
    name: 'Walk',
    rim: 'var(--danger)',
    svg: `<path d="M3 28h8M1 35h9M4 42h5" stroke="${STEAM}" stroke-width="2.4" stroke-linecap="round"/>
      <path d="M12 46h40a5 5 0 0 1 0 10H16a5 5 0 0 1-5-5Z" fill="#f8fafc" ${L}/>
      <path d="M13 47c0-11 4-20 12-23l7 6c4 2.4 10 3.6 15 4.6 6 1.2 9 6 9 12.4Z" fill="url(#sg-shoe)" ${L}/>
      <path d="M47 34.6c6 1.2 9 6 9 12.4h-8.5c0-5-1-9-.5-12.4Z" fill="#fff" fill-opacity=".35"/>
      <path d="M13.5 41c4-1 7-.5 9 1.5" stroke="#fff" stroke-opacity=".55" stroke-width="2" stroke-linecap="round" fill="none"/>
      <path d="M25.5 31l5-3.4M29 34l5-3.4M33 36.6l5-3.4" stroke="#fff" stroke-width="2.2" stroke-linecap="round"/>
      <path d="M17 51h4M26 51h4M35 51h4M44 51h4" stroke="${STEAM}" stroke-width="1.6" stroke-linecap="round"/>`,
  },
  pills: {
    name: 'Pills',
    rim: 'var(--tag)',
    svg: `<g transform="rotate(-35 26 26)"><rect x="11" y="20" width="30" height="13" rx="6.5" fill="#fff" ${L}/>
        <path d="M17.5 20H26v13h-8.5a6.5 6.5 0 0 1 0-13Z" fill="#f59e0b" ${L}/>
        <path d="M15 24h7" stroke="#fff" stroke-opacity=".7" stroke-width="2" stroke-linecap="round"/></g>
      <g transform="rotate(22 42 44)"><rect x="29" y="38" width="27" height="12" rx="6" fill="#fff" ${L}/>
        <path d="M35 38h7.5v12H35a6 6 0 0 1 0-12Z" fill="#8b5cf6" ${L}/>
        <path d="M33 42h6" stroke="#fff" stroke-opacity=".7" stroke-width="2" stroke-linecap="round"/></g>
      <circle cx="17" cy="48" r="8" fill="#f1f5f9" ${L}/><path d="M11.5 48h11" ${L}/>`,
  },
  apple: {
    name: 'Fruit',
    rim: 'var(--claude)',
    svg: `<path d="M32 20c-6-5-20-4-20 12 0 14 10 24 16 24 2 0 3-1 4-1s2 1 4 1c6 0 16-10 16-24 0-16-14-17-20-12Z" fill="url(#sg-apple)" ${L}/>
      <path d="M32 20c0-5 1-8.5 3-11" fill="none" stroke="#78350f" stroke-width="2.6" stroke-linecap="round"/>
      <path d="M34.5 13c4-6 11.5-5.2 13.5-3-4 5.2-9.5 6-13.5 3Z" fill="#22c55e" ${L} stroke-width="1.6"/>
      <path d="M37 11.8c3-1.6 6-2 8.6-1.8" stroke="#15803d" stroke-width="1.2" fill="none" stroke-linecap="round"/>
      <path d="M19 31c0-5 3-8.5 7-9.5" stroke="#fff" stroke-opacity=".7" stroke-width="3" stroke-linecap="round" fill="none"/>`,
  },
  book: {
    name: 'Reading',
    rim: 'var(--codex)',
    svg: `<path d="M5 48V15c9-2.4 19-1.2 27 4 8-5.2 18-6.4 27-4v33c-9-2.2-19-1-27 4-8-5-18-6.2-27-4Z" fill="url(#sg-book)" ${L}/>
      <path d="M9 44V18c7.5-1.2 15.5 0 21 4v25c-5.5-3.4-13.5-4.4-21-3Z" fill="#fffbeb" ${L} stroke-width="1.6"/>
      <path d="M55 44V18c-7.5-1.2-15.5 0-21 4v25c5.5-3.4 13.5-4.4 21-3Z" fill="#fffbeb" ${L} stroke-width="1.6"/>
      <path d="M13 24.5c4-.6 8-.2 12.5 1.6M13 30c4-.6 8-.2 12.5 1.6M13 35.5c4-.6 8-.2 12.5 1.6M38.5 26.1c4.5-1.8 8.5-2.2 12.5-1.6M38.5 31.6c4.5-1.8 8.5-2.2 12.5-1.6" stroke="#cbd5e1" stroke-width="1.6" stroke-linecap="round" fill="none"/>
      <path d="M43 19.5v12l3-2.4 3 2.4V18.6" fill="#ef4444" stroke="#991b1b" stroke-width="1.2" stroke-linejoin="round"/>`,
  },
  weights: {
    name: 'Exercise',
    rim: 'var(--idle)',
    svg: `<rect x="15" y="29" width="34" height="6" rx="2" fill="#cbd5e1" ${L}/>
      <path d="M26 30v4M29 30v4M35 30v4M38 30v4" stroke="#94a3b8" stroke-width="1.4"/>
      <rect x="9" y="19" width="8" height="26" rx="2.5" fill="url(#sg-plate)" ${L}/>
      <rect x="3" y="23.5" width="7" height="17" rx="2.2" fill="url(#sg-plate)" ${L}/>
      <rect x="47" y="19" width="8" height="26" rx="2.5" fill="url(#sg-plate)" ${L}/>
      <rect x="54" y="23.5" width="7" height="17" rx="2.2" fill="url(#sg-plate)" ${L}/>
      <path d="M11.5 22v8M49.5 22v8" stroke="#fff" stroke-opacity=".45" stroke-width="2" stroke-linecap="round"/>`,
  },
  plant: {
    name: 'Break',
    rim: 'var(--done)',
    svg: `<path d="M32 37c0-10-6-18-17-20 0 10 6 18 17 20Z" fill="#4ade80" ${L}/>
      <path d="M32 37c0-12 8-22 19-24 0 12-8 22-19 24Z" fill="#22c55e" ${L}/>
      <path d="M32 37c-3-8-2-17 0-25 2 8 3 17 0 25Z" fill="#16a34a" ${L}/>
      <path d="M19 21c4 3.4 8 8 12 14M47 17c-4.4 4-9 10-14 18" stroke="#15803d" stroke-width="1.3" fill="none" stroke-linecap="round"/>
      <path d="M19 41h26l-3 15a3 3 0 0 1-3 2.4H25a3 3 0 0 1-3-2.4Z" fill="url(#sg-pot)" ${L}/>
      <rect x="16.5" y="36" width="31" height="6.5" rx="2" fill="#fdba74" ${L}/>
      <path d="M24 46l1.6 8" stroke="#fff" stroke-opacity=".5" stroke-width="2" stroke-linecap="round"/>`,
  },
  call: {
    name: 'Call',
    rim: 'var(--done)',
    kind: 'done',
    svg: `<path d="M41 7a17 17 0 0 1 16 16M41 14a10 10 0 0 1 9 9" fill="none" stroke="${STEAM}" stroke-width="2.6" stroke-linecap="round"/>
      <g transform="translate(3 6) scale(2.05)">
        <path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1L8.1 9.9a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.4c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.7 2Z" fill="url(#sg-phone)" stroke="#233140" stroke-width="1.1" stroke-linejoin="round"/>
      </g>
      <path d="M12 12.5c-1.6 6 1 15 7 22" stroke="#fff" stroke-opacity=".6" stroke-width="2.4" stroke-linecap="round" fill="none"/>`,
  },
  message: {
    name: 'Message',
    rim: 'var(--codex)',
    kind: 'done',
    svg: `<path d="M27 8h25a6 6 0 0 1 6 6v13a6 6 0 0 1-6 6h-2v7l-7.5-7H27a6 6 0 0 1-6-6V14a6 6 0 0 1 6-6Z" fill="#c7d2fe" ${L}/>
      <path d="M10 21h26a6 6 0 0 1 6 6v14a6 6 0 0 1-6 6H21l-8.5 8v-8H10a6 6 0 0 1-6-6V27a6 6 0 0 1 6-6Z" fill="url(#sg-msg)" ${L}/>
      <circle cx="14" cy="34" r="2.7" fill="#fff"/><circle cx="23" cy="34" r="2.7" fill="#fff"/><circle cx="32" cy="34" r="2.7" fill="#fff"/>
      <path d="M9 26.5h12" stroke="#fff" stroke-opacity=".55" stroke-width="2" stroke-linecap="round"/>`,
  },
  count: {
    name: 'Count',
    rim: 'var(--aiH)',
    svg: `<rect x="6" y="12" width="44" height="40" rx="5" fill="#fffbeb" ${L}/>
      <path d="M6 20h44" stroke="#fcd34d" stroke-width="2"/>
      <path d="M15 26v18M21 26v18M27 26v18M33 26v18" stroke="#233140" stroke-width="2.8" stroke-linecap="round"/>
      <path d="M11 41l27-12" stroke="#ef4444" stroke-width="2.8" stroke-linecap="round"/>
      <path d="M41 30v14" stroke="#233140" stroke-width="2.8" stroke-linecap="round"/>
      <g transform="rotate(35 52 34)">
        <rect x="48.5" y="16" width="7" height="24" rx="1.5" fill="#facc15" ${L} stroke-width="1.6"/>
        <rect x="48.5" y="13" width="7" height="4.5" rx="1.2" fill="#f472b6" ${L} stroke-width="1.6"/>
        <path d="M48.5 40l3.5 7 3.5-7Z" fill="#fde68a" ${L} stroke-width="1.6"/>
        <path d="M51 45.2l1 2 1-2Z" fill="#233140"/>
      </g>`,
  },
  done: {
    name: 'Done',
    rim: 'var(--accent)',
    kind: 'done',
    svg: `<circle cx="32" cy="34" r="21" fill="url(#sg-done)" ${L}/>
      <path d="M21 34.5l7.5 7.5L44 26" fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/>
      <path d="M18 27a15 15 0 0 1 9-9" stroke="#fff" stroke-opacity=".55" stroke-width="2.4" stroke-linecap="round" fill="none"/>
      <path d="M52 6l1.3 3.7L57 11l-3.7 1.3L52 16l-1.3-3.7L47 11l3.7-1.3Z" fill="#facc15" stroke="#b45309" stroke-width="1"/>
      <path d="M10 6.5l.9 2.4 2.4.9-2.4.9-.9 2.4-.9-2.4-2.4-.9 2.4-.9Z" fill="#facc15" stroke="#b45309" stroke-width="1"/>
      <path d="M57 46l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7Z" fill="#facc15"/>`,
  },
  sleep: {
    name: 'Sleep',
    rim: 'var(--tag)',
    svg: `<path d="M36 10a23 23 0 1 0 20 33A18 18 0 0 1 36 10Z" fill="url(#sg-moon)" ${L}/>
      <circle cx="25" cy="38" r="2.6" fill="#f59e0b" fill-opacity=".5"/><circle cx="33" cy="48" r="1.8" fill="#f59e0b" fill-opacity=".5"/><circle cx="20" cy="27" r="1.5" fill="#f59e0b" fill-opacity=".5"/>
      <path d="M15 22a17 17 0 0 1 9-9" stroke="#fff" stroke-opacity=".6" stroke-width="2.4" stroke-linecap="round" fill="none"/>
      <path d="M42 8h8l-8 9h8M52 20h5l-5 6h5" fill="none" stroke="#6366f1" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/>
      <path d="M53 34l1 2.6 2.6 1-2.6 1-1 2.6-1-2.6-2.6-1 2.6-1Z" fill="#fde68a" stroke="#b45309" stroke-width=".8"/>`,
  },
  eyes: {
    name: 'Eye break',
    rim: 'var(--running)',
    svg: `<path d="M14 16l-3-4.5M24 11.5l-1.2-5M40 11.5l1.2-5M50 16l3-4.5" ${L}/>
      <path d="M4 32c8-13 17-19 28-19s20 6 28 19c-8 13-17 19-28 19S12 45 4 32Z" fill="#fff" ${L}/>
      <circle cx="32" cy="32" r="12" fill="url(#sg-iris)" ${L}/>
      <circle cx="32" cy="32" r="5.2" fill="#0f172a"/>
      <circle cx="36" cy="27.5" r="2.6" fill="#fff"/><circle cx="28.5" cy="36" r="1.2" fill="#fff" fill-opacity=".7"/>
      <path d="M10 41c4 4.5 8.5 7.4 14 8.6" stroke="#cbd5e1" stroke-width="2" stroke-linecap="round" fill="none"/>`,
  },
  meditate: {
    name: 'Meditate',
    rim: 'var(--claude)',
    svg: `<path d="M8 56h48" stroke="${STEAM}" stroke-width="2.4" stroke-linecap="round"/>
      <path d="M32 50C22 49 10 43 6 31c10-1 21 6 26 19Z" fill="#fbcfe8" ${L}/>
      <path d="M32 50c10-1 22-7 26-19-10-1-21 6-26 19Z" fill="#fbcfe8" ${L}/>
      <path d="M32 50C22 46 15 35 16 22c10 3 16 14 16 28Z" fill="#f472b6" ${L}/>
      <path d="M32 50c10-4 17-15 16-28-10 3-16 14-16 28Z" fill="#f472b6" ${L}/>
      <path d="M32 50c-7-8-8-21 0-34 8 13 7 26 0 34Z" fill="url(#sg-lotus)" ${L}/>
      <path d="M32 22v20" stroke="#fff" stroke-opacity=".55" stroke-width="2" stroke-linecap="round"/>`,
  },
  meal: {
    name: 'Meal',
    rim: 'var(--waiting)',
    svg: `<path d="M44 4L31 30M52 7L36 31" stroke="#a16207" stroke-width="3" stroke-linecap="round"/>
      <path d="M12 34c1.5-8 9.5-13 20-13s18.5 5 20 13Z" fill="#fde68a" ${L} stroke-width="1.8"/>
      <ellipse cx="24" cy="27.5" rx="5.5" ry="3.6" fill="#fff" ${L} stroke-width="1.4"/><circle cx="24" cy="27.5" r="2" fill="#f59e0b"/>
      <circle cx="35" cy="26" r="2.4" fill="#22c55e"/><circle cx="41" cy="29.5" r="2" fill="#16a34a"/><circle cx="31" cy="30.5" r="1.8" fill="#ef4444"/>
      <path d="M7 34h50c0 12-10.5 20-25 20S7 46 7 34Z" fill="url(#sg-bowl)" ${L}/>
      <rect x="23" y="52" width="18" height="5" rx="2" fill="#7f1d1d" ${L} stroke-width="1.6"/>
      <path d="M13 39c2 6 7 9.5 13 10.6" stroke="#fff" stroke-opacity=".5" stroke-width="2.2" stroke-linecap="round" fill="none"/>`,
  },
}

// GRADIENTS defines, once per page, the gradients the pictures fill with.
export const GRADIENTS = `<defs>
  <linearGradient id="sg-water" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#7dd3fc"/><stop offset="1" stop-color="#0369a1"/></linearGradient>
  <linearGradient id="sg-glass" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#e0f2fe" stop-opacity=".55"/><stop offset="1" stop-color="#bae6fd" stop-opacity=".25"/></linearGradient>
  <linearGradient id="sg-cup" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#ffffff"/><stop offset="1" stop-color="#d6d3d1"/></linearGradient>
  <linearGradient id="sg-shoe" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fb7185"/><stop offset="1" stop-color="#be123c"/></linearGradient>
  <linearGradient id="sg-apple" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#fca5a5"/><stop offset=".45" stop-color="#ef4444"/><stop offset="1" stop-color="#991b1b"/></linearGradient>
  <linearGradient id="sg-book" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#2dd4bf"/><stop offset="1" stop-color="#0f766e"/></linearGradient>
  <linearGradient id="sg-plate" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#64748b"/><stop offset="1" stop-color="#1e293b"/></linearGradient>
  <linearGradient id="sg-pot" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#fdba74"/><stop offset="1" stop-color="#c2410c"/></linearGradient>
  <linearGradient id="sg-phone" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#86efac"/><stop offset="1" stop-color="#15803d"/></linearGradient>
  <linearGradient id="sg-msg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#93c5fd"/><stop offset="1" stop-color="#2563eb"/></linearGradient>
  <linearGradient id="sg-done" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#5eead4"/><stop offset="1" stop-color="#0f766e"/></linearGradient>
  <linearGradient id="sg-moon" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#fef9c3"/><stop offset="1" stop-color="#f59e0b"/></linearGradient>
  <linearGradient id="sg-iris" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#7dd3fc"/><stop offset="1" stop-color="#0e7490"/></linearGradient>
  <linearGradient id="sg-lotus" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#f9a8d4"/><stop offset="1" stop-color="#db2777"/></linearGradient>
  <linearGradient id="sg-bowl" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#fca5a5"/><stop offset="1" stop-color="#b91c1c"/></linearGradient>
</defs>`
