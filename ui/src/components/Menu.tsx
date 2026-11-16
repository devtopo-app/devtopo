import { useEffect, useRef, useState, type ReactNode } from 'react'
import { clamp } from '../lib/timeline'

export const tracker = (source: string) => (source === 'jira' ? 'Jira' : 'Linear')

// Menu is a small dialog next to the element clicked to open it (rect).
// Escape or a click outside closes it.
export function Menu({ label, rect, onClose, children }: { label: string; rect: DOMRect; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const down = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && onClose()
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', down)
      document.removeEventListener('keydown', key)
    }
  }, [onClose])

  // Opens below the clicked element, or above it when there is no room below.
  const place = rect.bottom + 200 > window.innerHeight ? { bottom: window.innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label={label}
      className="fixed z-50 w-[280px] max-w-[calc(100vw-32px)] rounded-[10px] border border-border bg-bg dark:bg-raised px-3 py-2.5 shadow-xl"
      style={{ left: clamp(rect.left, 16, window.innerWidth - 296), ...place }}
    >
      {children}
    </div>
  )
}

// MarkDone marks a task done or not done; for a ticket it says that this
// moves it in its tracker too. A failed save shows why.
export function MarkDone({ done, ticket, mark, onSaved }: { done: boolean; ticket: { source: string; key: string } | null; mark: () => Promise<void>; onSaved: () => void }) {
  const [error, setError] = useState('')
  return (
    <>
      <button
        type="button"
        onClick={() => mark().then(onSaved, (e: Error) => setError(e.message))}
        className={`w-full rounded-lg px-2.5 py-1.5 text-sm font-bold ${done ? 'border border-border text-fg' : 'bg-done text-bg'}`}
      >
        {done ? 'Mark not done' : 'Mark done'}
      </button>
      {ticket && (
        <p className="mt-1.5 text-xs text-dim">
          {done ? `Moves ${ticket.key} back to in progress in ${tracker(ticket.source)}.` : `Also moves ${ticket.key} to Done in ${tracker(ticket.source)}.`}
        </p>
      )}
      {error && (
        <p role="alert" className="mt-1.5 text-xs text-waiting">
          Could not save: {error}
        </p>
      )}
    </>
  )
}
