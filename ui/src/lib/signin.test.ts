import { afterEach, describe, expect, it, vi } from 'vitest'
import { watchSignIn } from './signin'

describe('watchSignIn', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  it('tells a tab without the key how to sign in, once', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('no', { status: 401 })))
    watchSignIn()
    await fetch('/api/today')
    await fetch('/api/status')
    const alerts = document.querySelectorAll('[role="alert"]')
    expect(alerts).toHaveLength(1)
    expect(alerts[0].textContent).toContain('Open DevTopo')
  })

  it('leaves a signed-in tab alone', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })))
    watchSignIn()
    await fetch('/api/today')
    expect(document.querySelector('[role="alert"]')).toBeNull()
  })
})
