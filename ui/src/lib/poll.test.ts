import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { usePoll } from './poll'

describe('usePoll', () => {
  afterEach(() => vi.useRealTimers())

  it('asks again only after the last answer came, never two at once', async () => {
    vi.useFakeTimers()
    let inFlight = 0
    let most = 0
    let calls = 0
    const resolvers: (() => void)[] = []
    const fn = () => {
      calls++
      inFlight++
      most = Math.max(most, inFlight)
      return new Promise<number>((res) => resolvers.push(() => { inFlight--; res(calls) }))
    }
    renderHook(() => usePoll(fn, 1000))
    // A slow answer: ten intervals pass, and still one request.
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(calls).toBe(1)
    await act(async () => { resolvers.shift()!() })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(calls).toBe(2)
    expect(most).toBe(1)
  })
})
