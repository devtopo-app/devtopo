import { useEffect, useState } from 'react'
import {
  checkSummaryModels,
  fetchSummaryModels,
  saveSummaryModel,
  type SummaryModelsView,
  type SummaryProvider,
  type SummaryRow,
} from '../api'

const card = 'mt-6 scroll-mt-4 rounded-lg border border-border bg-panel p-4'
const picker = 'min-w-0 rounded border border-border bg-bg dark:bg-raised px-2 py-1 text-sm disabled:opacity-50'
const WARNING = 'Summaries run in the background on every active session, so this model uses your limits faster.'

// SetupHint says how to make a tool available. The commands are the same ones
// the README gives.
function SetupHint({ provider }: { provider: SummaryProvider }) {
  return (
    <div className="text-xs text-dim">
      {provider.ID === 'codex' ? (
        <>
          Codex is not available. Install the command-line tool with <code>npm install -g @openai/codex</code>, then run{' '}
          <code>codex login</code>.
        </>
      ) : (
        <>
          Claude is not available. Install Claude Code and log in: run <code>claude</code>, then <code>/login</code>.
        </>
      )}
    </div>
  )
}

function warning(row: SummaryRow, provider?: SummaryProvider): string {
  if (row.Fallback || !provider || (row.Model === provider.Cheapest && provider.CheapestKnown)) return ''
  const model = provider.Models.find((m) => m.ID === row.Model)
  let text = WARNING
  if (model?.Multiple) text += ` About ${model.Multiple}x the usage of GPT-6 Luna.`
  if (model?.PaidCredits) text += ' It may bill to paid usage credits on some plans.'
  return text
}

function Row({
  row,
  providers,
  busy,
  onSave,
}: {
  row: SummaryRow
  providers: SummaryProvider[]
  busy: boolean
  onSave: (kind: string, provider: string, model: string) => void
}) {
  const provider = providers.find((p) => p.ID === row.Provider)
  const inList = provider?.Models.some((m) => m.ID === row.Model)
  const nameOf = (id: string) => providers.find((p) => p.ID === id)?.Name ?? id
  const warn = warning(row, provider)
  return (
    <li>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm font-medium">{row.Label}</div>
        <div className="flex flex-wrap gap-2">
          <select
            aria-label={`Provider for ${row.Label}`}
            className={picker}
            value={row.Provider}
            disabled={busy}
            onChange={(e) => {
              const next = providers.find((p) => p.ID === e.target.value)
              if (next) onSave(row.Kind, next.ID, next.Cheapest)
            }}
          >
            {providers.map((p) =>
              p.Available ? (
                <option key={p.ID} value={p.ID}>
                  {p.Name}
                </option>
              ) : p.ID === row.Provider ? (
                <option key={p.ID} value={p.ID} disabled>
                  {p.Name} (not available)
                </option>
              ) : null,
            )}
          </select>
          <select
            aria-label={`Model for ${row.Label}`}
            className={picker}
            value={row.Model}
            disabled={busy || !provider?.Available}
            onChange={(e) => onSave(row.Kind, row.Provider, e.target.value)}
          >
            {provider?.Models.map((m) => (
              <option key={m.ID} value={m.ID}>
                {m.Name}
              </option>
            ))}
            {!inList && (
              <option value={row.Model}>{row.Listed ? row.Model : `${row.Model} (no longer listed)`}</option>
            )}
          </select>
        </div>
      </div>
      {row.Fallback && (
        <div className="mt-1 text-xs text-dim">
          {nameOf(row.Provider)} is not available, so these sessions use {nameOf(row.EffectiveProvider)} ({row.EffectiveModel}).
        </div>
      )}
      {warn && (
        <div className="mt-1 text-xs" style={{ color: 'var(--waiting)' }}>
          {warn}
        </div>
      )}
      {row.Error && (
        <div className="mt-1 text-xs" style={{ color: 'var(--waiting)' }}>
          The last summary failed: {row.Error}
        </div>
      )}
    </li>
  )
}

// SummaryModels chooses which tool and model summarize each kind of session,
// among the tools installed and logged in on this machine.
export function SummaryModels() {
  const [view, setView] = useState<SummaryModelsView | null>(null)
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    fetchSummaryModels()
      .then(setView)
      .catch(() => {})
  }, [])

  const run = async (action: () => Promise<SummaryModelsView>) => {
    setBusy(true)
    setError('')
    try {
      setView(await action())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not reach the daemon.')
    } finally {
      setBusy(false)
    }
  }

  if (!view || (!view.Off && !view.Rows)) return null

  const heading = (
    <div id="summaries-title" className="font-semibold">
      Session summaries
    </div>
  )
  if (view.Off) {
    return (
      <section aria-labelledby="summaries-title" className={card}>
        {heading}
        <div className="mt-2 text-xs text-dim">Summaries are turned off (DevTopo was started with --no-narrative).</div>
      </section>
    )
  }

  const missing = view.Providers.filter((p) => !p.Available)
  const anyAvailable = missing.length < view.Providers.length
  return (
    <section aria-labelledby="summaries-title" className={card}>
      {heading}
      <div className="mt-1 text-xs text-dim">
        Which tool and model write the summaries. A change applies to new activity, not to summaries already written.
      </div>
      {anyAvailable && (
        <ul className="mt-3 flex flex-col gap-3">
          {view.Rows.map((r) => (
            <Row
              key={r.Kind}
              row={r}
              providers={view.Providers}
              busy={busy}
              onSave={(kind, provider, model) => run(() => saveSummaryModel(kind, provider, model))}
            />
          ))}
        </ul>
      )}
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-1">
          {missing.map((p) => (
            <SetupHint key={p.ID} provider={p} />
          ))}
        </div>
        <button
          onClick={async () => {
            setChecking(true)
            await run(checkSummaryModels)
            setChecking(false)
          }}
          disabled={busy}
          className="rounded border border-border px-2 py-1 text-sm hover:bg-bg dark:hover:bg-raised disabled:opacity-50"
        >
          {checking ? 'Checking…' : 'Check again'}
        </button>
      </div>
      {error && (
        <div className="mt-2 text-xs" style={{ color: 'var(--waiting)' }}>
          {error}
        </div>
      )}
    </section>
  )
}
