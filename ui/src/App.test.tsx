import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App, { routeOf } from './App'

// The pages and the status bar fetch from the daemon; here they are stand-ins.
vi.mock('./views/Today', () => ({ Today: () => <p>today page</p> }))
vi.mock('./views/Planner', () => ({ Planner: () => <p>planner page</p> }))
vi.mock('./views/History', () => ({ History: () => null }))
vi.mock('./views/Settings', () => ({ Settings: () => null }))
vi.mock('./views/Help', () => ({ Help: () => null }))
vi.mock('./components/StatusBar', () => ({ StatusBar: () => null }))
vi.mock('./components/Stickers', () => ({
  Stickers: ({ adding }: { adding: boolean }) => <p>stickers {adding ? 'adding' : 'idle'}</p>,
}))

afterEach(() => {
  window.location.hash = ''
})

describe('routeOf', () => {
  it('reads the route without its query', () => {
    expect(routeOf('')).toBe('today')
    expect(routeOf('#/planner')).toBe('planner')
    expect(routeOf('#/planner?plan=p2')).toBe('planner')
  })
})

describe('App', () => {
  it('has stickers and an Add sticker button left of the theme switch on Today only', async () => {
    render(<App />)
    const add = screen.getByRole('button', { name: 'Add sticker' })
    expect(add.nextElementSibling).toBe(screen.getByRole('button', { name: 'Toggle theme' }))
    // a hexagon icon, its words shown only on mouse-over
    expect(add.querySelector('svg')).toHaveAttribute('width', '20')
    expect(screen.getByText('Add sticker')).toHaveClass('hidden', 'group-hover:inline')
    expect(screen.getByText('stickers idle')).toBeInTheDocument()
    await userEvent.click(add)
    expect(screen.getByText('stickers adding')).toBeInTheDocument()

    act(() => {
      window.location.hash = '#planner'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(screen.getByText('planner page')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add sticker' })).toBeNull()
    expect(screen.queryByText(/^stickers/)).toBeNull()
  })

  describe('Ask about today', () => {
    const enc = new TextEncoder()
    let push!: (e: object) => void
    let end!: () => void
    const fetchMock = vi.fn()

    beforeEach(() => {
      fetchMock.mockImplementation(async (url: string) => {
        if (url === '/api/ask/models') return new Response(JSON.stringify({ Models: [{ ID: 'haiku', Tool: 'claude', Label: 'Claude Haiku' }], Off: false }))
        if (url === '/api/today') return new Response(JSON.stringify({ Running: { Cards: null }, Waiting: { Cards: null }, Idle: { Cards: null } }))
        const body = new ReadableStream<Uint8Array>({
          start(c) {
            push = (e) => c.enqueue(enc.encode(JSON.stringify(e) + '\n'))
            end = () => c.close()
          },
        })
        return new Response(body)
      })
      vi.stubGlobal('fetch', fetchMock)
    })
    afterEach(() => {
      fetchMock.mockReset()
      vi.unstubAllGlobals()
    })

    it('Today shows the button and other routes do not', () => {
      render(<App />)
      const ask = screen.getByRole('button', { name: 'Ask about today' })
      // an agent icon, its words shown only on mouse-over
      expect(ask.querySelector('svg')).toHaveAttribute('width', '24')
      expect(screen.getByText('Ask about today')).toHaveClass('hidden', 'group-hover:inline')
      act(() => {
        window.location.hash = '#planner'
        window.dispatchEvent(new HashChangeEvent('hashchange'))
      })
      expect(screen.queryByRole('button', { name: 'Ask about today' })).toBeNull()
    })

    it('opens the window from the button and closes it again', async () => {
      render(<App />)
      expect(screen.queryByRole('dialog')).toBeNull()
      await userEvent.click(screen.getByRole('button', { name: 'Ask about today' }))
      expect(await screen.findByRole('dialog', { name: 'Ask about today' })).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Close' }))
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    it('a run and the chat survive closing and reopening', async () => {
      render(<App />)
      const button = screen.getByRole('button', { name: 'Ask about today' })
      await userEvent.click(button)
      await userEvent.type(await screen.findByLabelText('Question'), 'what happened?{Enter}')
      expect(await screen.findByText('what happened?')).toBeInTheDocument()
      await act(async () => push({ Type: 'checking', Total: 4 }))
      expect(await screen.findByText('Checking 4 sessions')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Close' }))
      expect(screen.queryByRole('dialog')).toBeNull()
      // the run goes on while the window is closed
      await act(async () => {
        push({ Type: 'done', Result: { Answer: 'Plenty.', Sources: null, FromSummaries: true, Total: 4, Read: 0, Shortened: null, Failed: null } })
        end()
      })

      await userEvent.click(button)
      expect(await screen.findByText('what happened?')).toBeInTheDocument()
      expect(screen.getByText('Plenty.')).toBeInTheDocument()
      expect(screen.getByText('Answered from summaries')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Ask' })).toBeInTheDocument()
    })
  })
})
