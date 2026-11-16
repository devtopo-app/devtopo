import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import type { AskModel } from '../ask-api'

const toolName = (tool: string) => (tool === 'codex' ? 'Codex' : 'Claude')

// ModelPicker shows the chosen model as plain text that turns into a button on
// hover; clicking it opens a panel of models above it.
export function ModelPicker({ models, value, onChange }: { models: AskModel[]; value: AskModel | undefined; onChange: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const bothTools = new Set(models.map((m) => m.Tool)).size > 1

  useEffect(() => {
    if (!open) return
    panel.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus()
    const outside = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', outside)
    return () => document.removeEventListener('mousedown', outside)
  }, [open])

  function close() {
    setOpen(false)
    trigger.current?.focus()
  }

  function key(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.stopPropagation() // closes the panel, not the window around it
      close()
    } else if (e.key === 'Tab') {
      setOpen(false)
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const options = [...(panel.current?.querySelectorAll<HTMLElement>('[role="option"]') ?? [])]
      const at = options.indexOf(document.activeElement as HTMLElement)
      const step = e.key === 'ArrowDown' ? 1 : -1
      options[(at + step + options.length) % options.length]?.focus()
    }
  }

  return (
    <div ref={root} className="relative" onKeyDown={key}>
      <button
        ref={trigger}
        type="button"
        aria-label={`Model: ${value?.Label ?? 'none'}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className={`flex items-center gap-1 rounded-md px-2 py-1 text-fg hover:bg-panel dark:hover:bg-raised ${open ? 'bg-panel dark:bg-raised' : ''}`}
      >
        {value?.Label}
        <svg aria-hidden="true" viewBox="0 0 12 12" className="h-2.5 w-2.5 text-dim">
          <path d="M2.5 4.5 6 8l3.5-3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {open && (
        <div ref={panel} role="listbox" aria-label="Models" className="absolute bottom-full left-0 z-10 mb-1 max-h-72 w-72 overflow-auto rounded-lg border border-border bg-bg p-1 shadow-xl dark:bg-raised">
          {models.map((m, i) => (
            <div key={m.ID}>
              {bothTools && m.Tool !== models[i - 1]?.Tool && <div className="px-2 pb-0.5 pt-1.5 text-[11px] font-bold text-dim">{toolName(m.Tool)}</div>}
              <button
                type="button"
                role="option"
                aria-selected={m.ID === value?.ID}
                onClick={() => {
                  onChange(m.ID)
                  close()
                }}
                className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left hover:bg-panel focus:bg-panel focus:outline-none dark:hover:bg-panel dark:focus:bg-panel"
              >
                <span className="min-w-0 flex-1">
                  <span className="block text-fg">{m.Label}</span>
                  {m.Description && <span className="block text-[11px] leading-snug text-dim">{m.Description}</span>}
                </span>
                {m.ID === value?.ID && (
                  <span aria-hidden="true" className="shrink-0 text-accent">
                    &#10003;
                  </span>
                )}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
