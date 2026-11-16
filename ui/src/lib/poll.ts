import { useEffect, useState } from 'react'

// usePoll calls fn immediately and then again intervalMs after each call ends,
// returning the latest value and the last error; reload fetches again now.
// Waiting for the call to end keeps requests from piling up while the daemon
// is slow, and an older answer from landing after a newer one. Pass a stable
// fn (a module-level fetcher) so the effect does not re-subscribe on every
// render.
export function usePoll<T>(
  fn: () => Promise<T>,
  intervalMs: number,
): { data: T | null; error: Error | null; reload: () => void } {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [round, setRound] = useState(0)

  useEffect(() => {
    let alive = true
    let id: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      try {
        const v = await fn()
        if (alive) {
          setData(v)
          setError(null)
        }
      } catch (e) {
        if (alive) setError(e as Error)
      }
      if (alive) id = setTimeout(tick, intervalMs)
    }
    tick()
    return () => {
      alive = false
      clearTimeout(id)
    }
  }, [fn, intervalMs, round])

  return { data, error, reload: () => setRound((n) => n + 1) }
}
