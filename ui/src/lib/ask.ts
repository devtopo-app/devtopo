import { useCallback, useRef, useState } from 'react'
import { streamAsk, type AskEvent, type AskNote, type AskResult } from '../ask-api'

export interface ChatTurn {
  question: string
  result?: AskResult
  notes: AskNote[]
  stopped?: boolean
  error?: string
}

export interface Progress {
  total: number
  checked: boolean
  relevant: number | null
  done: number
  combining: boolean
}

const freshProgress = (): Progress => ({ total: 0, checked: false, relevant: null, done: 0, combining: false })

// useAsk holds the chat about today. Call it high (in App) so closing the
// window keeps the chat and the run going. The chat lives in memory only.
export function useAsk() {
  const [turns, setTurns] = useState<ChatTurn[]>([])
  const [running, setRunning] = useState(false)
  const [progress, setProgress] = useState<Progress | null>(null)
  const turnsRef = useRef<ChatTurn[]>([])
  const runningRef = useRef(false)
  const ctrlRef = useRef<AbortController | null>(null)

  const commit = (next: ChatTurn[]) => {
    turnsRef.current = next
    setTurns(next)
  }
  const patchLast = (f: (t: ChatTurn) => ChatTurn) => {
    const t = turnsRef.current
    commit([...t.slice(0, -1), f(t[t.length - 1])])
  }
  const finish = () => {
    runningRef.current = false
    setRunning(false)
    setProgress(null)
  }

  const ask = useCallback((question: string, modelID: string) => {
    if (runningRef.current) return
    const memory = turnsRef.current.map((t) => ({
      Question: t.question,
      Answer: t.result?.Answer ?? '',
      Notes: t.notes,
    }))
    const ctrl = new AbortController()
    ctrlRef.current = ctrl
    runningRef.current = true
    setRunning(true)
    setProgress(freshProgress())
    commit([...turnsRef.current, { question, notes: [] }])

    let finished = false
    const onEvent = (e: AskEvent) => {
      if (ctrl.signal.aborted) return
      switch (e.Type) {
        case 'checking':
          setProgress((p) => ({ ...(p ?? freshProgress()), total: e.Total ?? 0 }))
          break
        case 'checked':
          setProgress((p) => ({ ...(p ?? freshProgress()), checked: true }))
          break
        case 'relevant':
          setProgress((p) => ({ ...(p ?? freshProgress()), relevant: e.Count ?? 0 }))
          break
        case 'read':
          setProgress((p) => ({ ...(p ?? freshProgress()), done: e.Done ?? 0 }))
          if (e.Note) {
            const n = e.Note
            patchLast((t) => ({ ...t, notes: [...t.notes, n] }))
          }
          break
        case 'combining':
          setProgress((p) => ({ ...(p ?? freshProgress()), combining: true }))
          break
        case 'done':
          finished = true
          if (e.Result) {
            const r = e.Result
            patchLast((t) => ({ ...t, result: r }))
          }
          break
        case 'error':
          finished = true
          patchLast((t) => ({ ...t, error: e.Error || 'The question failed.' }))
          break
      }
    }

    streamAsk({ ModelID: modelID, Question: question, Memory: memory }, ctrl.signal, onEvent)
      .then(() => {
        if (ctrl.signal.aborted) return
        if (!finished) patchLast((t) => ({ ...t, error: 'The answer stopped before it finished.' }))
        finish()
      })
      .catch((e: unknown) => {
        if (ctrl.signal.aborted) return
        patchLast((t) => ({ ...t, error: e instanceof Error && e.message ? e.message : 'The question failed.' }))
        finish()
      })
  }, [])

  const stop = useCallback(() => {
    if (!runningRef.current) return
    ctrlRef.current?.abort()
    patchLast((t) => ({ ...t, stopped: true }))
    finish()
  }, [])

  return { turns, running, progress, ask, stop }
}
