import { useEffect, useRef, useState, type ReactNode } from 'react'
import { checkUpdate, fetchUpdate, type UpdateView } from '../api'
import { usePoll } from '../lib/poll'
import { Icon } from './Icon'

const icons: Record<string, ReactNode> = {
  // Today is a dashboard: a gauge, its dial and its needle.
  today: (
    <Icon>
      <path data-part="dial" d="M3.34 19a10 10 0 1 1 17.32 0" />
      <path data-part="needle" d="m12 14 4-4" />
    </Icon>
  ),
  planner: (
    <Icon>
      <rect x="3" y="4" width="18" height="18" rx="2" />
      <path d="M16 2v4M8 2v4M3 10h18" />
    </Icon>
  ),
  history: (
    <Icon>
      <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
      <path d="M3 3v5h5M12 7v5l3 2" />
    </Icon>
  ),
  settings: (
    <Icon>
      <path d="M12.2 2h-.4a2 2 0 0 0-2 2v.2a2 2 0 0 1-1 1.7l-.4.3a2 2 0 0 1-2 0l-.2-.1a2 2 0 0 0-2.7.7l-.2.4a2 2 0 0 0 .7 2.7l.2.1a2 2 0 0 1 1 1.7v.5a2 2 0 0 1-1 1.7l-.2.1a2 2 0 0 0-.7 2.7l.2.4a2 2 0 0 0 2.7.7l.2-.1a2 2 0 0 1 2 0l.4.3a2 2 0 0 1 1 1.7v.2a2 2 0 0 0 2 2h.4a2 2 0 0 0 2-2v-.2a2 2 0 0 1 1-1.7l.4-.3a2 2 0 0 1 2 0l.2.1a2 2 0 0 0 2.7-.7l.2-.4a2 2 0 0 0-.7-2.7l-.2-.1a2 2 0 0 1-1-1.7v-.5a2 2 0 0 1 1-1.7l.2-.1a2 2 0 0 0 .7-2.7l-.2-.4a2 2 0 0 0-2.7-.7l-.2.1a2 2 0 0 1-2 0l-.4-.3a2 2 0 0 1-1-1.7V4a2 2 0 0 0-2-2Z" />
      <circle cx="12" cy="12" r="3" />
    </Icon>
  ),
  help: (
    <Icon>
      <circle cx="12" cy="12" r="10" />
      <path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3M12 17h.01" />
    </Icon>
  ),
}

const items = [
  { key: 'today', label: 'Today', href: '#/' },
  { key: 'planner', label: 'Planner', href: '#planner' },
  { key: 'history', label: 'History', href: '#history' },
]

// tip is a corner icon's name, shown above it on mouse-over.
const tip =
  'pointer-events-none absolute bottom-full left-1/2 mb-1 hidden -translate-x-1/2 whitespace-nowrap rounded bg-fg px-1.5 py-0.5 text-2xs font-semibold text-bg group-hover:block'

// Nav is the collapsible left bar: the tabs, each an icon and its name (only
// the icon when collapsed), and at the bottom Settings and Help as icons named
// on hover.
export function Nav({ route }: { route: string }) {
  const [collapsed, setCollapsed] = useState(false)
  const tone = (active: boolean) => (active ? 'font-semibold text-accent' : 'text-dim hover:bg-border/40')
  return (
    <nav
      className="flex flex-col gap-1 border-r border-border bg-panel dark:bg-bg p-2"
      style={{ width: collapsed ? 56 : 180, transition: 'width 120ms' }}
    >
      <button
        aria-label="Toggle navigation"
        aria-expanded={!collapsed}
        onClick={() => setCollapsed((c) => !c)}
        className={`mb-2 rounded p-2 text-dim hover:text-fg ${collapsed ? 'self-center' : 'self-end'}`}
      >
        <Icon>
          <path d={collapsed ? 'M6 17l5-5-5-5M13 17l5-5-5-5' : 'M18 17l-5-5 5-5M11 17l-5-5 5-5'} />
        </Icon>
      </button>
      <ul className="flex flex-col gap-1">
        {items.map((it) => (
          <li key={it.key}>
            <a
              href={it.href}
              title={collapsed ? it.label : undefined}
              aria-current={it.key === route ? 'page' : undefined}
              className={`flex w-full items-center gap-2.5 rounded py-2 text-left text-sm ${collapsed ? 'justify-center px-0' : 'px-3'} ${tone(it.key === route)}`}
            >
              {icons[it.key]}
              <span className={collapsed ? 'sr-only' : ''}>{it.label}</span>
            </a>
          </li>
        ))}
      </ul>
      <div role="group" aria-label="Settings and help" className={`mt-auto flex gap-1 ${collapsed ? 'flex-col items-center' : 'px-1'}`}>
        <a
          href="#settings"
          aria-label="Settings"
          aria-current={route === 'settings' ? 'page' : undefined}
          className={`group relative rounded p-2 ${tone(route === 'settings')}`}
        >
          {icons.settings}
          <span role="tooltip" className={tip}>
            Settings
          </span>
        </a>
        <HelpMenu active={route === 'help'} tone={tone} />
      </div>
    </nav>
  )
}

// HelpMenu is the question-mark corner button: a small menu with the guide
// (Help), About, and Check for updates, which asks for the latest release right
// there in the menu and says what it found. A dot on the icon says a newer
// release is out.
function HelpMenu({ active, tone }: { active: boolean; tone: (active: boolean) => string }) {
  const [open, setOpen] = useState(false)
  const [check, setCheck] = useState<Check>({ state: 'idle' })
  const ref = useRef<HTMLDivElement>(null)
  const { data: polled } = usePoll(fetchUpdate, 30 * 60_000)
  const update = check.state === 'done' ? check.view : polled
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', down)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  const toggle = () => {
    // A result belongs to the menu it was asked in; reopened, the menu starts fresh.
    if (!open && check.state !== 'checking') setCheck({ state: 'idle' })
    setOpen((o) => !o)
  }
  const runCheck = async () => {
    setCheck({ state: 'checking' })
    try {
      setCheck({ state: 'done', view: await checkUpdate() })
    } catch {
      setCheck({ state: 'failed' })
    }
  }
  const item = 'flex w-full items-center justify-between gap-3 rounded px-2.5 py-1.5 text-left text-sm text-fg hover:bg-border/40'
  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label="Help and about"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={toggle}
        className={`group relative rounded p-2 ${tone(active || open)}`}
      >
        {icons.help}
        {update?.Available && (
          <span aria-hidden className="absolute right-1 top-1 h-2 w-2 rounded-full bg-accent ring-2 ring-panel dark:ring-bg" />
        )}
        {!open && (
          <span role="tooltip" className={tip}>
            Help and about
          </span>
        )}
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Help and about"
          className="absolute bottom-full left-0 z-50 mb-1 w-60 rounded-[10px] border border-border bg-bg p-1 shadow-xl dark:bg-raised"
        >
          <a role="menuitem" href="#help" onClick={() => setOpen(false)} className={item}>
            Help
          </a>
          <a role="menuitem" href="#settings?section=about" onClick={() => setOpen(false)} className={item}>
            About
          </a>
          <button
            type="button"
            role="menuitem"
            onClick={runCheck}
            disabled={check.state === 'checking'}
            className={`${item} disabled:opacity-60`}
          >
            Check for updates
            {update?.Available && <span className="text-xs font-semibold text-accent">{update.Latest}</span>}
          </button>
          <div
            role="status"
            aria-live="polite"
            className={check.state === 'idle' ? 'sr-only' : 'mx-1 mb-1 mt-1 border-t border-border px-1.5 pt-2 text-xs text-dim'}
          >
            <CheckResult check={check} onNavigate={() => setOpen(false)} />
          </div>
        </div>
      )}
    </div>
  )
}

type Check = { state: 'idle' } | { state: 'checking' } | { state: 'failed' } | { state: 'done'; view: UpdateView }

// CheckResult says what Check for updates found, in the menu's live region.
function CheckResult({ check, onNavigate }: { check: Check; onNavigate: () => void }) {
  if (check.state === 'idle') return null
  if (check.state === 'checking') {
    return (
      <span className="flex items-center gap-2">
        <svg aria-hidden width="12" height="12" viewBox="0 0 24 24" className="animate-spin" fill="none" stroke="currentColor" strokeWidth="3">
          <path d="M21 12a9 9 0 1 1-6.2-8.6" strokeLinecap="round" />
        </svg>
        Checking…
      </span>
    )
  }
  if (check.state === 'failed') {
    return <span style={{ color: 'var(--waiting)' }}>Could not check: DevTopo did not answer.</span>
  }
  const u = check.view
  if (!u.Supported) return <>Update checks run in release builds (this is {u.Current}).</>
  if (u.Available) {
    const link = 'underline hover:text-accent'
    return (
      <>
        <span className="font-semibold text-fg">Update available: {u.Latest}</span>
        <span className="mt-1 flex gap-3">
          {u.URL && (
            <a href={u.URL} target="_blank" rel="noreferrer" className={link}>
              Release notes
            </a>
          )}
          <a href="#settings?section=updates" onClick={onNavigate} className={link}>
            How to update
          </a>
        </span>
      </>
    )
  }
  if (u.Error) return <span style={{ color: 'var(--waiting)' }}>Check failed: {u.Error}</span>
  return <>You're up to date ({u.Current}).</>
}
