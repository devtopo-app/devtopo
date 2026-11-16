// Types mirror the Go ask package (PascalCase, no json tags). Go encodes empty
// slices as null, so every slice may be null.

export interface AskModel {
  ID: string
  Tool: string
  Label: string
  Description?: string
  Default?: boolean
}

export interface AskNote {
  SessionID: string
  Label: string
  Text: string
}

export interface AskSource {
  SessionID: string
  Label: string
  Agent: string
  At: string
  Why: string
}

export interface AskResult {
  Answer: string
  Sources: AskSource[] | null
  FromSummaries: boolean
  Total: number
  Read: number
  Shortened: string[] | null
  Failed: string[] | null
}

export interface AskEvent {
  Type: 'checking' | 'checked' | 'relevant' | 'read' | 'combining' | 'done' | 'error'
  Total?: number
  Count?: number
  Done?: number
  Note?: AskNote | null
  Result?: AskResult | null
  Error?: string
}

export interface AskTurn {
  Question: string
  Answer: string
  Notes: AskNote[]
}

export interface AskBody {
  ModelID: string
  Question: string
  Memory: AskTurn[]
}

export async function fetchAskModels(): Promise<{ Models: AskModel[]; Off: boolean }> {
  const r = await fetch('/api/ask/models')
  if (!r.ok) throw new Error(`GET /api/ask/models: ${r.status}`)
  return r.json()
}

// streamAsk posts a question and calls onEvent for each event line as it
// arrives. A refusal (non-2xx) throws an Error carrying the server's text.
export async function streamAsk(body: AskBody, signal: AbortSignal, onEvent: (e: AskEvent) => void): Promise<void> {
  const r = await fetch('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!r.ok) {
    const text = (await r.text().catch(() => '')).trim()
    throw new Error(text || `The question could not be asked (${r.status}).`)
  }
  if (!r.body) throw new Error('The server sent no answer stream.')
  const reader = r.body.getReader()
  const dec = new TextDecoder()
  let buf = ''
  const flush = (s: string) => {
    if (s.trim()) onEvent(JSON.parse(s) as AskEvent)
  }
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += dec.decode(value, { stream: true })
    let i: number
    while ((i = buf.indexOf('\n')) >= 0) {
      flush(buf.slice(0, i))
      buf = buf.slice(i + 1)
    }
  }
  buf += dec.decode()
  flush(buf)
}
