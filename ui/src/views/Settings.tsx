import { useEffect, useRef, useState, type ReactNode } from 'react'
import { checkUpdate, fetchUpdate, setUpdateEnabled, type UpdateView } from '../api'
import { SummaryModels } from './SummaryModels'
import { Icon } from '../components/Icon'

interface Connector {
  Name: string
  Status: string
  Detail: string
  Values?: Record<string, string>
  Account?: string
  SyncError?: string
}

// secretConnectors need credentials entered here. Each field name matches the
// connect endpoint's JSON body; fields are hidden unless marked plain.
interface ConnectConfig {
  endpoint: string
  fields: { field: string; placeholder: string; plain?: boolean }[]
}

const secretConnectors: Record<string, ConnectConfig> = {
  Linear: {
    endpoint: '/api/connectors/linear',
    fields: [{ field: 'apiKey', placeholder: 'Linear API key (lin_api_...)' }],
  },
  Jira: {
    endpoint: '/api/connectors/jira',
    fields: [
      { field: 'site', placeholder: 'Jira site (your-team.atlassian.net)', plain: true },
      { field: 'email', placeholder: 'Atlassian account email', plain: true },
      { field: 'apiToken', placeholder: 'Jira API token' },
    ],
  },
  GitLab: {
    endpoint: '/api/connectors/gitlab',
    fields: [
      { field: 'site', placeholder: 'GitLab address (gitlab.com, or your own server)', plain: true },
      { field: 'token', placeholder: 'GitLab personal access token' },
    ],
  },
  'Azure DevOps': {
    endpoint: '/api/connectors/azure',
    fields: [
      { field: 'organization', placeholder: 'Organization (the name in dev.azure.com/your-organization)', plain: true },
      { field: 'token', placeholder: 'Azure DevOps personal access token' },
    ],
  },
}

const inputClass = 'min-w-0 flex-1 rounded border border-border bg-bg dark:bg-raised px-2 py-1 text-sm'

function SecretConnect({
  cfg,
  initial,
  onDone,
}: {
  cfg: ConnectConfig
  initial?: Record<string, string>
  onDone: () => void
}) {
  const [values, setValues] = useState<Record<string, string>>(initial ?? {})
  const [error, setError] = useState('')

  const connect = async () => {
    if (cfg.fields.some((f) => !values[f.field]?.trim())) return
    setError('')
    try {
      const res = await fetch(cfg.endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      })
      if (!res.ok) {
        // The server says which part failed, without echoing any secret.
        setError((await res.text().catch(() => '')).trim() || 'That did not work - check it and try again.')
        return
      }
      setValues({})
      onDone()
    } catch {
      setError('Could not reach the daemon.')
    }
  }

  return (
    <div className="mt-3">
      <div className="flex flex-wrap gap-2">
        {cfg.fields.map((f) => (
          <input
            key={f.field}
            type={f.plain ? 'text' : 'password'}
            value={values[f.field] ?? ''}
            onChange={(e) => setValues({ ...values, [f.field]: e.target.value })}
            placeholder={f.placeholder}
            aria-label={f.placeholder}
            className={inputClass}
          />
        ))}
        <button onClick={connect} className="rounded border border-border px-3 py-1 text-sm hover:text-accent">
          Connect
        </button>
      </div>
      {error && (
        <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
          {error}
        </div>
      )}
    </div>
  )
}

// Connected shows a connected connector's Change and Disconnect. Change opens
// the form with the stored non-secret values; the secret is entered again.
// Disconnect asks once more, since it forgets the secret and the synced items.
function Connected({ cfg, values, onDone }: { cfg: ConnectConfig; values?: Record<string, string>; onDone: () => void }) {
  const [mode, setMode] = useState<'' | 'change' | 'confirm'>('')
  const disconnect = async () => {
    await fetch(cfg.endpoint, { method: 'DELETE' }).catch(() => {})
    setMode('')
    onDone()
  }
  const button = 'rounded border border-border px-2 py-1 text-sm hover:text-accent'
  return (
    <div className="mt-3">
      <div className="flex flex-wrap gap-2">
        {mode === 'confirm' ? (
          <>
            <button onClick={disconnect} className={button}>
              Yes, disconnect
            </button>
            <button onClick={() => setMode('')} className={button}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <button onClick={() => setMode(mode === 'change' ? '' : 'change')} className={button}>
              Change
            </button>
            <button onClick={() => setMode('confirm')} className={button}>
              Disconnect
            </button>
          </>
        )}
      </div>
      {mode === 'confirm' && (
        <div className="mt-2 text-xs text-dim">This forgets the saved credentials and removes the synced items.</div>
      )}
      {mode === 'change' && (
        <SecretConnect
          cfg={cfg}
          initial={values}
          onDone={() => {
            setMode('')
            onDone()
          }}
        />
      )}
    </div>
  )
}

// GitHubConnect explains how GitHub connects (through the gh CLI, nothing to
// enter here) and checks gh again once the user installed it or logged in.
function GitHubConnect({ status, account, onDone }: { status: string; account?: string; onDone: () => void }) {
  const [checking, setChecking] = useState(false)
  const check = async () => {
    setChecking(true)
    await fetch('/api/connectors/github/check', { method: 'POST' }).catch(() => {})
    setChecking(false)
    onDone()
  }
  let hint: React.ReactNode
  if (status === 'connected') {
    hint = <>Connected through the GitHub CLI{account ? <> as <span className="font-semibold">{account}</span></> : null}.</>
  } else if (status === 'gh not installed') {
    hint = (
      <>
        Install the{' '}
        <a className="underline" href="https://cli.github.com" target="_blank" rel="noreferrer">
          GitHub CLI
        </a>
        , run <code>gh auth login</code> in a terminal, then check again.
      </>
    )
  } else {
    hint = (
      <>
        Run <code>gh auth login</code> in a terminal, then check again.
      </>
    )
  }
  return (
    <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
      <div className="text-xs text-dim">{hint}</div>
      <button
        onClick={check}
        disabled={checking}
        className="rounded border border-border px-2 py-1 text-sm hover:bg-bg dark:hover:bg-raised disabled:opacity-50"
      >
        {checking ? 'Checking…' : 'Check again'}
      </button>
    </div>
  )
}

// CalendarFeed is one connected calendar as GET /api/calendars returns it. The
// secret URL never comes back: only its label and host.
interface CalendarFeed {
  ID: string
  Label: string
  Name: string
  Host: string
  Status: 'ok' | 'error' | 'pending'
  Error?: string
}

// AddCalendar posts a label and a secret iCal URL as a new calendar feed.
function AddCalendar({ onDone, onCancel }: { onDone: () => void; onCancel?: () => void }) {
  const [label, setLabel] = useState('')
  const [url, setUrl] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const add = async () => {
    if (!url.trim() || busy) return
    setError('')
    setBusy(true)
    try {
      const res = await fetch('/api/calendars', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label: label.trim(), icalUrl: url.trim() }),
      })
      if (!res.ok) {
        setError((await res.text().catch(() => '')).trim() || 'That did not work - check it and try again.')
        return
      }
      setLabel('')
      setUrl('')
      onDone()
    } catch {
      setError('Could not reach the daemon.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-3">
      <div className="flex flex-wrap gap-2">
        <input
          type="text"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          placeholder="Name (optional; else Google's)"
          aria-label="Calendar name"
          className={`${inputClass} max-w-[12rem]`}
        />
        <input
          type="password"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="Private iCal URL (https://...)"
          aria-label="Private iCal URL"
          className={inputClass}
        />
        <button onClick={add} disabled={busy} className="rounded border border-border px-3 py-1 text-sm hover:text-accent">
          {busy ? 'Adding...' : 'Add calendar'}
        </button>
        {onCancel && (
          <button onClick={onCancel} className="rounded border border-border px-3 py-1 text-sm hover:text-accent">
            Cancel
          </button>
        )}
      </div>
      {error && (
        <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
          {error}
        </div>
      )}
    </div>
  )
}

// CalendarFeedRow is one connected calendar: its name (the one you gave it,
// else its own name from Google), host and last sync, a Rename, and
// a Remove that asks once more, since it drops that calendar's meetings.
function CalendarFeedRow({ feed, onRemoved }: { feed: CalendarFeed; onRemoved: () => void }) {
  const [mode, setMode] = useState<'' | 'confirm' | 'rename'>('')
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const remove = async () => {
    await fetch(`/api/calendars/${encodeURIComponent(feed.ID)}`, { method: 'DELETE' }).catch(() => {})
    setMode('')
    onRemoved()
  }
  const rename = async () => {
    setError('')
    try {
      const res = await fetch(`/api/calendars/${encodeURIComponent(feed.ID)}/label`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label: draft.trim() }),
      })
      if (!res.ok) {
        setError((await res.text().catch(() => '')).trim() || 'Could not rename it.')
        return
      }
      setMode('')
      onRemoved()
    } catch {
      setError('Could not reach the daemon.')
    }
  }
  // The name you gave it, else the calendar's own name from Google.
  const name = feed.Label || feed.Name || (feed.Status === 'pending' ? 'New calendar' : 'Unnamed calendar')
  const button = 'rounded border border-border px-2 py-1 text-sm hover:text-accent'
  const status =
    feed.Status === 'error'
      ? { text: `sync failed: ${feed.Error ?? 'unknown error'}`, color: 'var(--waiting)' }
      : feed.Status === 'ok'
        ? { text: 'synced', color: 'var(--done)' }
        : { text: 'not synced yet', color: 'var(--dim)' }
  const details = [feed.Label && feed.Name && feed.Label !== feed.Name ? `in Google: ${feed.Name}` : '', feed.Host]
    .filter(Boolean)
    .join(' · ')
  return (
    <li className="rounded border border-border px-3 py-2" aria-label={`Calendar ${name}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="text-sm font-medium">{name}</div>
          <div className="text-xs text-dim">
            {details ? `${details} · ` : ''}
            <span style={{ color: status.color }}>{status.text}</span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          {mode === 'confirm' ? (
            <>
              <button onClick={remove} className={button}>
                Yes, remove
              </button>
              <button onClick={() => setMode('')} className={button}>
                Cancel
              </button>
            </>
          ) : (
            mode === '' && (
              <>
                <button
                  onClick={() => {
                    setDraft(feed.Label || feed.Name || '')
                    setError('')
                    setMode('rename')
                  }}
                  className={button}
                  aria-label={`Rename ${name}`}
                >
                  Rename
                </button>
                <button onClick={() => setMode('confirm')} className={button} aria-label={`Remove ${name}`}>
                  Remove
                </button>
              </>
            )
          )}
        </div>
      </div>
      {mode === 'rename' && (
        <form
          className="mt-2 flex flex-wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            rename()
          }}
        >
          <input
            type="text"
            value={draft}
            autoFocus
            maxLength={80}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={feed.Name ? `Empty for "${feed.Name}"` : 'Name, e.g. Work'}
            aria-label="New calendar name"
            className={inputClass}
          />
          <button type="submit" className={button}>
            Save
          </button>
          <button type="button" onClick={() => setMode('')} className={button}>
            Cancel
          </button>
        </form>
      )}
      {error && (
        <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
          {error}
        </div>
      )}
      {mode === 'confirm' && (
        <div className="mt-2 text-xs text-dim">This forgets this calendar's address and removes its meetings.</div>
      )}
    </li>
  )
}

// CalendarCard is the inside of the Calendar card: any number of calendars,
// each added with its own secret iCal URL and removed on its own.
function CalendarCard({ onChange }: { onChange: () => void }) {
  const [feeds, setFeeds] = useState<CalendarFeed[]>([])
  const [adding, setAdding] = useState(false)
  const load = () =>
    fetch('/api/calendars')
      .then((r) => r.json())
      .then((f) => setFeeds(Array.isArray(f) ? f : []))
      .catch(() => {})
  useEffect(() => {
    load()
  }, [])
  const changed = () => {
    setAdding(false)
    load()
    onChange()
  }
  return (
    <>
      {feeds.length > 0 && (
        <ul className="mt-3 flex flex-col gap-2">
          {feeds.map((f) => (
            <CalendarFeedRow key={f.ID} feed={f} onRemoved={changed} />
          ))}
        </ul>
      )}
      {feeds.length === 0 || adding ? (
        <AddCalendar onDone={changed} onCancel={feeds.length > 0 ? () => setAdding(false) : undefined} />
      ) : (
        <div className="mt-3">
          <button
            onClick={() => setAdding(true)}
            className="rounded border border-border px-2 py-1 text-sm hover:text-accent"
          >
            Add another calendar
          </button>
        </div>
      )}
    </>
  )
}

const day = (iso: string | null) =>
  iso ? new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : ''

const card = 'mt-6 scroll-mt-4 rounded-lg border border-border bg-panel p-4'

// AboutAndUpdates is the About card and the Updates card. They share the one
// release check, and #settings?section=about or section=updates (the help
// menu, the status bar's "Update available") scrolls to its card, also while
// Settings is already open.
function AboutAndUpdates() {
  const [u, setU] = useState<UpdateView | null>(null)
  const about = useRef<HTMLElement>(null)
  const updates = useRef<HTMLElement>(null)
  useEffect(() => {
    fetchUpdate()
      .then(setU)
      .catch(() => {})
  }, [])
  const loaded = u !== null
  useEffect(() => {
    if (!loaded) return
    const show = () => {
      const section = /[?&]section=(\w+)/.exec(window.location.hash)?.[1]
      const target = section === 'about' ? about : section === 'updates' ? updates : null
      target?.current?.scrollIntoView?.({ block: 'start' })
    }
    show()
    window.addEventListener('hashchange', show)
    return () => window.removeEventListener('hashchange', show)
  }, [loaded])
  return (
    <>
      <AboutCard anchor={about} u={u} />
      <UpdatesCard anchor={updates} u={u} onChange={setU} />
    </>
  )
}

// AboutCard says which DevTopo this is, its license, where its data lives,
// and points at Help.
function AboutCard({ anchor, u }: { anchor: React.Ref<HTMLElement>; u: UpdateView | null }) {
  return (
    <section ref={anchor} id="about" aria-labelledby="about-title" className={card}>
      <div className="flex items-center justify-between gap-4">
        <div id="about-title" className="font-semibold">
          About
        </div>
        {u?.Current && <span className="shrink-0 text-xs text-dim">DevTopo {u.Current}</span>}
      </div>
      <div className="mt-2 flex flex-col gap-1 text-sm text-dim">
        <p>DevTopo is free, open source software under the MIT License.</p>
        <p>
          Your data stays on this machine, in <code className="text-fg">~/.devtopo</code>.
        </p>
        <p>
          Setup and how it works:{' '}
          <a href="#help" className="underline hover:text-accent">
            Help
          </a>
          .
        </p>
      </div>
    </section>
  )
}

// UpdatesCard is the release check: the latest release and how to upgrade for
// the way DevTopo was installed, a Check for updates, and the switch for the
// automatic check. Nothing installs itself: pull, not push.
function UpdatesCard({
  anchor,
  u,
  onChange,
}: {
  anchor: React.Ref<HTMLElement>
  u: UpdateView | null
  onChange: (u: UpdateView) => void
}) {
  const [busy, setBusy] = useState(false)
  const check = async () => {
    setBusy(true)
    try {
      onChange(await checkUpdate())
    } catch {
      /* the card keeps the last result */
    } finally {
      setBusy(false)
    }
  }
  const toggle = async (enabled: boolean) => {
    try {
      onChange(await setUpdateEnabled(enabled))
    } catch {
      /* unchanged */
    }
  }

  const button = 'rounded border border-border px-2 py-1 text-sm hover:bg-bg dark:hover:bg-raised disabled:opacity-50'
  let body: React.ReactNode = null
  if (u?.Current && !u.Supported) {
    body = <div className="mt-2 text-xs text-dim">Update checks run in release builds (this is {u.Current}).</div>
  } else if (u?.Current) {
    const packaged = u.Install === 'homebrew' || u.Install === 'scoop'
    body = (
      <>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
          <div className="text-sm">
            {u.Available ? (
              <>
                <span className="font-semibold">{u.Latest}</span> is out
                {u.PublishedAt ? <span className="text-dim"> (released {day(u.PublishedAt)})</span> : null}.{' '}
                <a href={u.URL} target="_blank" rel="noreferrer" className="underline hover:text-accent">
                  Release notes
                </a>
              </>
            ) : u.Latest ? (
              <span className="text-dim">
                You run the latest release ({u.Latest}
                {u.PublishedAt ? `, released ${day(u.PublishedAt)}` : ''}).
              </span>
            ) : (
              <span className="text-dim">Not checked yet.</span>
            )}
          </div>
          <button onClick={check} disabled={busy} className={button}>
            {busy ? 'Checking…' : 'Check for updates'}
          </button>
        </div>
        {u.Available && (
          <div className="mt-2 text-xs text-dim">
            {packaged ? (
              <>
                To update, run <code className="text-fg">{u.Hint}</code> in a terminal, then restart DevTopo.
              </>
            ) : (
              <>
                To update,{' '}
                <a href={u.URL} target="_blank" rel="noreferrer" className="underline hover:text-accent">
                  download it from the release page
                </a>{' '}
                and replace this copy.
              </>
            )}
          </div>
        )}
        {u.Error && (
          <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
            Last check failed: {u.Error}
          </div>
        )}
        {u.CheckedAt && <div className="mt-1 text-2xs text-dim">Checked {new Date(u.CheckedAt).toLocaleString()}.</div>}
        <label className="mt-3 flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            checked={u.Enabled}
            onChange={(e) => toggle(e.target.checked)}
            className="mt-1"
          />
          <span>
            Check for updates automatically
            <span className="block text-xs text-dim">
              Twice a day DevTopo asks GitHub for its latest release. It sends nothing about you or your work.
            </span>
          </span>
        </label>
      </>
    )
  }
  return (
    <section ref={anchor} id="updates" aria-labelledby="updates-title" className={card}>
      <div id="updates-title" className="font-semibold">
        Updates
      </div>
      {body}
    </section>
  )
}

// icons are line icons for each service, drawn like the app's others.
const icons: Record<string, ReactNode> = {
  GitHub: (
    <>
      <path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" />
      <path d="M9 18c-4.51 2-5-2-7-2" />
    </>
  ),
  GitLab: (
    <path d="m22 13.29-3.33-10a.42.42 0 0 0-.14-.18.38.38 0 0 0-.22-.11.39.39 0 0 0-.23.07.42.42 0 0 0-.14.18l-2.26 6.67H8.32L6.1 3.26a.42.42 0 0 0-.1-.18.38.38 0 0 0-.26-.08.39.39 0 0 0-.23.07.42.42 0 0 0-.14.18L2 13.29a.74.74 0 0 0 .27.83L12 21l9.69-6.88a.71.71 0 0 0 .31-.83Z" />
  ),
  'Azure DevOps': (
    <>
      <circle cx="18" cy="18" r="3" />
      <circle cx="6" cy="6" r="3" />
      <path d="M13 6h3a2 2 0 0 1 2 2v7" />
      <path d="M6 9v12" />
    </>
  ),
  Linear: (
    <>
      <path d="m3 17 2 2 4-4" />
      <path d="m3 7 2 2 4-4" />
      <path d="M13 6h8" />
      <path d="M13 12h8" />
      <path d="M13 18h8" />
    </>
  ),
  Jira: (
    <>
      <path d="M2 9a3 3 0 0 1 0 6v2a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-2a3 3 0 0 1 0-6V7a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2Z" />
      <path d="M13 5v2" />
      <path d="M13 17v2" />
      <path d="M13 11v2" />
    </>
  ),
  'Calendar': (
    <>
      <path d="M8 2v4" />
      <path d="M16 2v4" />
      <rect width="18" height="18" x="3" y="4" rx="2" />
      <path d="M3 10h18" />
    </>
  ),
}

const helpLink = 'underline hover:text-accent'

// help is how to connect each service, shown in its own card. GitHub's lives in
// GitHubConnect, as it depends on whether gh is there.
const help: Record<string, ReactNode> = {
  GitLab: (
    <>
      Create a personal access token under your avatar - Edit profile - Access tokens, with the read_api scope. A project
      or group access token acts as a bot, so it won't work.
    </>
  ),
  'Azure DevOps': (
    <>
      Create a personal access token under User settings - Personal access tokens, for this organization, with the
      Code (Read) scope.
    </>
  ),
  Linear: <>In Linear: Settings - Security &amp; access - Personal API keys - create a key.</>,
  Jira: (
    <>
      Create an{' '}
      <a href="https://id.atlassian.com/manage-profile/security/api-tokens" target="_blank" rel="noreferrer" className={helpLink}>
        API token
      </a>{' '}
      for your Atlassian account. A token with scopes works too: read:jira-work and read:jira-user to read, plus
      write:jira-work for Mark done.
    </>
  ),
  'Calendar': (
    <>
      Add one for each calendar you want to see. Where to find its address:
      <ul className="mt-1 list-disc pl-4">
        <li>Google Calendar: Settings - your calendar - Integrate calendar - the "Secret address in iCal format".</li>
        <li>
          Outlook and Microsoft Teams (Teams meetings are in your Outlook calendar): Outlook on the web - Settings - Calendar -
          Shared calendars - Publish a calendar, then copy the ICS link. Some work accounts don't allow publishing.
        </li>
        <li>Apple Calendar (iCloud): share the calendar as a Public Calendar, then copy its link.</li>
      </ul>
    </>
  ),
}

// groups puts the services in order, by what they bring.
const groups = [
  { title: 'Code hosts', sub: 'Reviews waiting on you, and your own pull requests.', names: ['GitHub', 'GitLab', 'Azure DevOps'] },
  { title: 'Issue trackers', sub: 'Your assigned issues, as plannable tasks.', names: ['Linear', 'Jira'] },
  { title: 'Calendars', sub: 'Your meetings, for planning your day.', names: ['Calendar'] },
]

// pill is a service's status at a glance: a failed sync over everything else.
function pill(c: Connector) {
  if (c.SyncError) return { text: 'sync failed', color: 'var(--danger)' }
  if (c.Status === 'connected') return { text: c.Status, color: 'var(--done)' }
  if (c.Status === 'not configured') return { text: c.Status, color: 'var(--dim)' }
  return { text: c.Status, color: 'var(--waiting)' }
}

// ServiceCard is one service: closed to its icon, name and status, and opened by a
// click to what it brings, how to connect it, and its fields.
function ServiceCard({ c, children }: { c: Connector; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const p = pill(c)
  return (
    <li className="rounded-lg border border-border bg-panel">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:text-accent"
      >
        <span className="shrink-0 text-dim">
          <Icon>{icons[c.Name]}</Icon>
        </span>
        <span className="min-w-0 flex-1 truncate font-semibold">{c.Name}</span>
        <span
          className="shrink-0 rounded-full border px-2 py-0.5 text-xs"
          style={{ color: p.color, borderColor: `color-mix(in srgb, ${p.color} 45%, transparent)` }}
        >
          {p.text}
        </span>
      </button>
      {open && (
        <div className="border-t border-border px-4 pb-4 pt-3">
          <div className="text-xs text-dim">{c.Detail}</div>
          {help[c.Name] && <div className="mt-2 text-xs text-dim">{help[c.Name]}</div>}
          {c.SyncError && (
            <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
              Last sync failed: {c.SyncError}
            </div>
          )}
          {children}
        </div>
      )}
    </li>
  )
}

// Settings lists the services DevTopo reads, grouped by what they bring. All but
// GitHub take a secret you enter here (stored locally, never shown back).
export function Settings() {
  const [conns, setConns] = useState<Connector[]>([])

  const refresh = () =>
    fetch('/api/connectors')
      .then((r) => r.json())
      .then(setConns)
      .catch(() => {})
  useEffect(() => {
    refresh()
  }, [])

  const body = (c: Connector) => {
    if (c.Name === 'Calendar') return <CalendarCard onChange={refresh} />
    const cfg = secretConnectors[c.Name]
    return (
      <>
        {c.Status !== 'connected' && cfg && <SecretConnect cfg={cfg} onDone={refresh} />}
        {c.Status === 'connected' && cfg && <Connected cfg={cfg} values={c.Values} onDone={refresh} />}
        {c.Name === 'GitHub' && <GitHubConnect status={c.Status} account={c.Account} onDone={refresh} />}
      </>
    )
  }
  // A service the daemon knows but no group names goes in a last group, so none hides.
  const named = new Set(groups.flatMap((g) => g.names))
  const others = conns.filter((c) => !named.has(c.Name)).map((c) => c.Name)
  const shown = others.length ? [...groups, { title: 'Other', sub: '', names: others }] : groups

  return (
    <div>
      <h1 className="mb-1 text-xl font-semibold">Settings</h1>
      <p className="mb-4 text-dim">
        Connect the tools DevTopo reads. Everything stays on this machine: your keys and URLs are stored only on this
        machine and never leave it.
      </p>
      {conns.length > 0 &&
        shown.map((g) => {
          const cards = g.names.map((n) => conns.find((c) => c.Name === n)).filter((c): c is Connector => !!c)
          if (!cards.length) return null
          return (
            <section key={g.title} aria-label={g.title} className="mb-5">
              <h2 className="text-sm font-semibold">{g.title}</h2>
              {g.sub && <p className="mb-2 text-xs text-dim">{g.sub}</p>}
              <ul className="flex flex-col gap-2">
                {cards.map((c) => (
                  <ServiceCard key={c.Name} c={c}>
                    {body(c)}
                  </ServiceCard>
                ))}
              </ul>
            </section>
          )
        })}
      <SummaryModels />
      <AboutAndUpdates />
    </div>
  )
}
