import { renderHook, act, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAsk } from './ask'
import { streamAsk, type AskEvent } from '../ask-api'

const enc = new TextEncoder()

// A response whose body the test feeds chunk by chunk.
function liveResponse() {
  let ctrl!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      ctrl = c
    },
  })
  return {
    res: new Response(body, { status: 200 }),
    send: (s: string) => ctrl.enqueue(enc.encode(s)),
    end: () => ctrl.close(),
  }
}

const line = (e: Partial<AskEvent>) => JSON.stringify(e) + '\n'
const done = (answer: string) =>
  line({ Type: 'done', Result: { Answer: answer, Sources: null, FromSummaries: true, Total: 1, Read: 0, Shortened: null, Failed: null } })
const note = { SessionID: 's1', Label: 'L', Text: 'found' }

let fetchMock: ReturnType<typeof vi.fn>
beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => vi.unstubAllGlobals())

// The next fetch returns these chunks and then ends the stream.
function replyWith(...chunks: string[]) {
  fetchMock.mockImplementationOnce(async () => {
    const l = liveResponse()
    chunks.forEach(l.send)
    l.end()
    return l.res
  })
}

// The next fetch returns a stream the test keeps open; aborting ends it.
function replyLive() {
  const l = liveResponse()
  let signal!: AbortSignal
  fetchMock.mockImplementationOnce(async (_u: string, init: RequestInit) => {
    signal = init.signal as AbortSignal
    signal.addEventListener('abort', () => {
      try {
        l.end()
      } catch {
        /* already closed */
      }
    })
    return l.res
  })
  return { ...l, signal: () => signal }
}

describe('ask', () => {
  it('parses events split across chunks', async () => {
    const full = line({ Type: 'checking', Total: 4 })
    replyWith(full.slice(0, 10), full.slice(10))
    const got: AskEvent[] = []
    await streamAsk({ ModelID: 'm', Question: 'q', Memory: [] }, new AbortController().signal, (e) => got.push(e))
    expect(got).toEqual([{ Type: 'checking', Total: 4 }])
  })

  it('sends earlier turns as Memory', async () => {
    replyWith(line({ Type: 'read', Done: 1, Total: 1, Note: note }), done('first answer'))
    replyWith(done('second'))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('one?', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    act(() => result.current.ask('two?', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    const body = JSON.parse(fetchMock.mock.calls[1][1].body)
    expect(body.Question).toBe('two?')
    expect(body.Memory).toEqual([{ Question: 'one?', Answer: 'first answer', Notes: [note] }])
  })

  it('keeps notes from read events on the turn', async () => {
    replyWith(line({ Type: 'read', Done: 1, Total: 2, Note: note }), line({ Type: 'read', Done: 2, Total: 2 }), done('a'))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    expect(result.current.turns[0].notes).toEqual([note])
    expect(result.current.turns[0].result?.Answer).toBe('a')
    expect(result.current.progress).toBeNull()
  })

  it('maps events to progress', async () => {
    const l = replyLive()
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    l.send(
      line({ Type: 'checking', Total: 5 }) +
        line({ Type: 'checked' }) +
        line({ Type: 'relevant', Count: 2 }) +
        line({ Type: 'read', Done: 1, Total: 2 }) +
        line({ Type: 'combining' }),
    )
    await waitFor(() =>
      expect(result.current.progress).toEqual({ total: 5, checked: true, relevant: 2, done: 1, combining: true }),
    )
    l.send(done('x'))
    l.end()
    await waitFor(() => expect(result.current.progress).toBeNull())
  })

  it('stop aborts, marks the turn stopped and keeps its notes', async () => {
    const l = replyLive()
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    l.send(line({ Type: 'read', Done: 1, Total: 3, Note: note }))
    await waitFor(() => expect(result.current.turns[0].notes).toHaveLength(1))
    act(() => result.current.stop())
    expect(l.signal().aborted).toBe(true)
    expect(result.current.running).toBe(false)
    expect(result.current.progress).toBeNull()
    expect(result.current.turns[0]).toMatchObject({ stopped: true, notes: [note] })
    expect(result.current.turns[0].error).toBeUndefined()
    await new Promise((r) => setTimeout(r, 10))
    expect(result.current.turns[0].error).toBeUndefined()
    expect(result.current.turns[0].stopped).toBe(true)
  })

  it("sends a stopped turn's notes with the next question", async () => {
    const l = replyLive()
    replyWith(done('b'))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('one?', 'm'))
    l.send(line({ Type: 'read', Done: 1, Total: 3, Note: note }))
    await waitFor(() => expect(result.current.turns[0].notes).toHaveLength(1))
    act(() => result.current.stop())
    act(() => result.current.ask('two?', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    const body = JSON.parse(fetchMock.mock.calls[1][1].body)
    expect(body.Memory).toEqual([{ Question: 'one?', Answer: '', Notes: [note] }])
  })

  it('shows the server text on 409 and adds no answer', async () => {
    fetchMock.mockResolvedValueOnce(new Response('a question is already running\n', { status: 409 }))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    expect(result.current.turns[0].error).toBe('a question is already running')
    expect(result.current.turns[0].result).toBeUndefined()
  })

  it('an error event sets the turn error and keeps its notes', async () => {
    replyWith(line({ Type: 'read', Done: 1, Total: 2, Note: note }), line({ Type: 'error', Error: 'tool failed' }))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    expect(result.current.turns[0]).toMatchObject({ error: 'tool failed', notes: [note] })
    expect(result.current.turns[0].result).toBeUndefined()
  })

  it('a stream that ends with no done event is an error, not a silent stop', async () => {
    replyWith(line({ Type: 'checking', Total: 2 }))
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('q', 'm'))
    await waitFor(() => expect(result.current.running).toBe(false))
    expect(result.current.turns[0].error).toBeTruthy()
    expect(result.current.turns[0].stopped).toBeUndefined()
  })

  it('ignores ask while running', async () => {
    const l = replyLive()
    const { result } = renderHook(() => useAsk())
    act(() => result.current.ask('one', 'm'))
    act(() => result.current.ask('two', 'm'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(result.current.turns).toHaveLength(1)
    l.send(done('x'))
    l.end()
    await waitFor(() => expect(result.current.running).toBe(false))
  })
})
