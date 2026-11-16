import { useEffect, useRef, useState } from 'react'
import { fetchToday, focusSession, type CardData } from '../api'
import { fetchAskModels, type AskModel, type AskResult } from '../ask-api'
import type { useAsk } from '../lib/ask'
import { usePoll } from '../lib/poll'
import { openAction } from './Card'
import { ModelPicker } from './ModelPicker'

const MODEL_KEY = 'devtopo-ask-model'

type Models = { state: 'loading' } | { state: 'error' } | { state: 'ready'; models: AskModel[]; off: boolean }

const agentName = (agent: string) => (agent === 'codex' ? 'Codex' : 'Claude')

function savedModel(): string | null {
  try {
    return localStorage.getItem(MODEL_KEY)
  } catch {
    return null
  }
}

// summaryLine says how much was read for an answer; nothing when there was
// nothing to read.
function summaryLine(r: AskResult): string {
  if (r.FromSummaries) return 'Answered from summaries'
  return r.Total > 0 ? `Read ${r.Read} of ${r.Total} sessions` : ''
}

// AskWindow is the centered chat about today over a dimmed screen. The chat
// itself lives in App (useAsk), so closing the window keeps it and any run.
export function AskWindow({ chat, onClose }: { chat: ReturnType<typeof useAsk>; onClose: () => void }) {
  const { turns, running, progress, ask, stop } = chat
  const [models, setModels] = useState<Models>({ state: 'loading' })
  const [chosen, setChosen] = useState<string | null>(savedModel)
  const [text, setText] = useState('')
  const [openErr, setOpenErr] = useState('')
  const { data: day } = usePoll(fetchToday, 5000)
  const chatRef = useRef<HTMLDivElement>(null)
  const downOnScrim = useRef(false)

  useEffect(() => {
    let alive = true
    fetchAskModels().then(
      (m) => alive && setModels({ state: 'ready', models: m.Models ?? [], off: m.Off }),
      () => alive && setModels({ state: 'error' }),
    )
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [onClose])

  // keep the newest turn in view
  useEffect(() => {
    const el = chatRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [turns, progress])

  const cards = new Map<string, CardData>()
  if (day) for (const lane of [day.Running, day.Waiting, day.Idle]) for (const c of lane?.Cards ?? []) cards.set(c.ID, c)

  const list = models.state === 'ready' ? models.models : []
  const model = list.find((m) => m.ID === chosen) ?? list.find((m) => m.Default) ?? list[0]
  const bothTools = new Set(list.map((m) => m.Tool)).size > 1

  function submit() {
    const q = text.trim()
    if (!q || !model || running) return
    ask(q, model.ID)
    setText('')
  }

  function pick(id: string) {
    setChosen(id)
    try {
      localStorage.setItem(MODEL_KEY, id)
    } catch {
      // remembering the choice is best effort
    }
  }

  function open(id: string, action: 'focus' | 'resume') {
    focusSession(id, action).then(onClose, (e: Error) => setOpenErr(e.message))
  }

  const step = 'py-0.5'
  const ready = models.state === 'ready' && list.length > 0

  return (
    <div data-testid="ask-scrim" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-5 py-8" onMouseDown={(e) => (downOnScrim.current = e.target === e.currentTarget)} onClick={(e) => e.target === e.currentTarget && downOnScrim.current && onClose()}>
      <div role="dialog" aria-modal="true" aria-label="Ask about today" className="flex max-h-full min-h-0 w-full max-w-[620px] flex-col rounded-[14px] border border-border bg-bg shadow-xl dark:bg-panel" style={{ height: 600 }}>
        <div className="flex items-center justify-between border-b border-border px-3.5 py-2.5">
          <h2 className="text-sm font-bold">Ask about today</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="rounded px-1.5 text-dim hover:text-fg">
            &#10005;
          </button>
        </div>
        <p className="mx-3.5 mt-2.5 rounded-lg border border-waiting border-l-4 px-2.5 py-1.5 text-xs">
          Runs through your own subscription and counts toward its usage limits.
        </p>
        {bothTools && <p className="mx-3.5 mt-1.5 text-xs text-dim">The selected model reads every session, including those from the other tool.</p>}

        <div ref={chatRef} className="flex min-h-0 flex-1 flex-col gap-2.5 overflow-auto px-3.5 py-3">
          {models.state === 'error' && (
            <p role="alert" className="text-sm text-waiting">
              Couldn't load the models.
            </p>
          )}
          {models.state === 'ready' && models.off && <p className="m-auto text-center text-sm text-dim">Summaries and chat are switched off (--no-narrative).</p>}
          {models.state === 'ready' && !models.off && list.length === 0 && (
            <p className="m-auto text-center text-sm text-dim">
              Asking needs Claude Code or Codex installed and logged in. After installing one, press Check again in Settings.{' '}
              <a href="#/help" className="text-accent underline">
                How to install
              </a>
            </p>
          )}
          {ready && turns.length === 0 && <p className="m-auto text-center text-sm text-dim">Ask what happened in today's sessions.</p>}
          {turns.map((t, i) => {
            const r = t.result
            const sources = r?.Sources ?? []
            const shortened = r?.Shortened ?? []
            const failed = r?.Failed ?? []
            const line = r ? summaryLine(r) : ''
            return (
              <div key={i} className="flex flex-col gap-2">
                <div className="max-w-[85%] self-end whitespace-pre-wrap rounded-[10px] border border-border bg-raised px-3 py-1.5 text-sm dark:border-transparent">{t.question}</div>
                {r && (
                  <div className="text-sm">
                    <div className="whitespace-pre-wrap">{r.Answer}</div>
                    {sources.length > 0 && (
                      <ul className="mt-1">
                        {sources.map((s, si) => {
                          const c = cards.get(s.SessionID)
                          const act = c ? openAction(c) : null
                          return (
                            <li key={si} className="mt-1.5 rounded-lg border border-border bg-bg px-2.5 py-1.5 text-xs dark:bg-raised">
                              <div className="flex items-start gap-2">
                                <span className="min-w-0 flex-1">
                                  <span className="text-tag">{s.Label}</span>
                                  {s.At && <span className="text-dim"> &middot; {s.At}</span>}
                                  <span className="text-dim">, {agentName(s.Agent)}</span>
                                </span>
                                {act && (
                                  <button type="button" title={act.label} onClick={() => open(s.SessionID, act.action)} className="shrink-0 text-accent hover:underline">
                                    Open
                                  </button>
                                )}
                              </div>
                              {s.Why && <div className="mt-0.5 text-dim">{s.Why}</div>}
                            </li>
                          )
                        })}
                      </ul>
                    )}
                    {line && <p className="mt-2 text-xs text-dim">{line}</p>}
                    {shortened.length > 0 && <p className="mt-0.5 text-xs text-dim">Shortened to fit: {shortened.join(', ')}</p>}
                    {failed.length > 0 && (
                      <p className="mt-0.5 text-xs text-dim">
                        Couldn't read {failed.length} of {r.Read + failed.length} sessions: {failed.join(', ')}
                      </p>
                    )}
                  </div>
                )}
                {t.stopped && <p className="text-xs text-dim">Stopped.</p>}
                {t.error && (
                  <p role="alert" className="text-sm text-waiting">
                    {t.error}
                  </p>
                )}
              </div>
            )
          })}
          {running && progress && (
            <div className="text-xs text-dim">
              {progress.checked ? <div className={`${step} text-done`}>Checked all {progress.total} sessions against their summaries</div> : <div className={`${step} text-accent`}>Checking {progress.total} sessions</div>}
              {progress.relevant !== null && <div className={`${step} text-done`}>{progress.relevant} look relevant</div>}
              {progress.relevant !== null && progress.relevant > 0 && !progress.combining && (
                <>
                  <div className={`${step} text-accent`}>
                    Reading the relevant sessions: {progress.done} of {progress.relevant}
                  </div>
                  <div role="progressbar" aria-label="Reading progress" aria-valuemin={0} aria-valuemax={progress.relevant} aria-valuenow={progress.done} className="my-1 h-[5px] overflow-hidden rounded bg-panel dark:bg-raised">
                    <div className="h-full bg-accent" style={{ width: `${Math.min(100, (progress.done / progress.relevant) * 100)}%` }} />
                  </div>
                </>
              )}
              {progress.combining && <div className={`${step} text-accent`}>Writing the answer</div>}
            </div>
          )}
          {openErr && (
            <p role="alert" className="text-sm text-waiting">
              {openErr}
            </p>
          )}
        </div>

        {ready && (
          <div className="border-t border-border px-3.5 py-2.5">
            <input
              aria-label="Question"
              autoFocus
              value={text}
              placeholder={turns.length ? 'Ask another question…' : 'Ask a question…'}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && !e.nativeEvent.isComposing && submit()}
              className="mb-2 w-full rounded-lg border border-border bg-bg px-2.5 py-2 text-sm dark:bg-raised"
            />
            <div className="flex items-center justify-between text-xs text-dim">
              {running ? (
                <>
                  <span>{model?.Label}</span>
                  <button type="button" onClick={stop} className="rounded-md border border-border px-3.5 py-1 text-fg">
                    Stop
                  </button>
                </>
              ) : (
                <>
                  <ModelPicker models={list} value={model} onChange={pick} />
                  <button type="button" onClick={submit} className="rounded-md bg-accent px-3.5 py-1 font-bold text-bg">
                    Ask
                  </button>
                </>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
