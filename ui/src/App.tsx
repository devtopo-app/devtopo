import { useCallback, useEffect, useState } from 'react'
import { Nav } from './components/Nav'
import { StatusBar } from './components/StatusBar'
import { ThemeToggle } from './components/ThemeToggle'
import { Icon } from './components/Icon'
import { Stickers } from './components/Stickers'
import { Today } from './views/Today'
import { Planner } from './views/Planner'
import { History } from './views/History'
import { Settings } from './views/Settings'
import { Help } from './views/Help'
import { AskWindow } from './components/AskWindow'
import { useAsk } from './lib/ask'
import { Logo } from './components/Logo'

// routeOf reads the route from a hash, without its query (#/planner?plan=id).
export const routeOf = (hash: string) => hash.replace(/^#\/?/, '').split('?')[0] || 'today'

function useHashRoute(): string {
  const parse = () => routeOf(window.location.hash)
  const [route, setRoute] = useState(parse)
  useEffect(() => {
    const on = () => setRoute(parse())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return route
}

// App is the shell: left nav, a header with the theme toggle, the routed view in
// the main area, and the bottom status bar.
export default function App() {
  const route = useHashRoute()
  const [adding, setAdding] = useState(false)
  const [asking, setAsking] = useState(false)
  const chat = useAsk()
  const today = route === 'today'
  const closeAsk = useCallback(() => setAsking(false), [])
  return (
    <div className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1">
        <Nav route={route} />
        <main className="relative flex-1 overflow-auto px-5 py-6 md:px-8">
          <header className="mb-5 flex items-center justify-between">
            <h1 className="flex items-center gap-2 text-sm font-semibold uppercase tracking-[0.2em] text-dim">
              <Logo />
              DevTopo
            </h1>
            <div className="flex items-center gap-2">
              {today && (
                <button
                  type="button"
                  aria-label="Ask about today"
                  aria-haspopup="dialog"
                  aria-expanded={asking}
                  onClick={() => setAsking((a) => !a)}
                  className={`group flex items-center gap-1.5 rounded p-1.5 text-sm font-bold hover:text-fg ${asking ? 'text-accent' : 'text-dim'}`}
                >
                  <Icon size={24}>
                    <rect x="4.5" y="8" width="15" height="11.5" rx="3" />
                    <path d="M12 8V5" />
                    <circle cx="12" cy="3.75" r="1.25" />
                    <path d="M9 12.75v1.75M15 12.75v1.75" />
                  </Icon>
                  <span className="hidden group-hover:inline">Ask about today</span>
                </button>
              )}
              {today && (
                <button
                  type="button"
                  aria-label="Add sticker"
                  aria-haspopup="dialog"
                  aria-expanded={adding}
                  onClick={() => setAdding((a) => !a)}
                  className={`group flex items-center gap-1.5 rounded p-1.5 text-sm font-bold hover:text-fg ${adding ? 'text-accent' : 'text-dim'}`}
                >
                  <Icon size={20}>
                    <path d="M12 2.5l8.2 4.75v9.5L12 21.5l-8.2-4.75v-9.5Z" />
                    <path d="M12 8.5v7M8.5 12h7" />
                  </Icon>
                  <span className="hidden group-hover:inline">Add sticker</span>
                </button>
              )}
              <ThemeToggle />
            </div>
          </header>
          {today && asking && <AskWindow chat={chat} onClose={closeAsk} />}
          {today && <Stickers adding={adding} onAddClose={() => setAdding(false)} />}
          {route === 'planner' ? <Planner /> : route === 'history' ? <History /> : route === 'settings' ? <Settings /> : route === 'help' ? <Help /> : <Today />}
        </main>
      </div>
      <StatusBar />
    </div>
  )
}
