import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { deleteSticker, fetchStickers, putSticker, setStickerCount, type Sticker } from '../api'
import { dateKey } from '../lib/timeline'
import { GRADIENTS, PICS } from './stickerPics'

// A pointy-top hexagon is this much taller than it is wide.
const RATIO = 1.1547

// MIN_W is the smallest sticker width, and a new sticker's.
const MIN_W = 72

// roundedHex is the hexagon's outline in a 100-wide box, inset from its edge
// and with corners rounded by r.
function roundedHex(inset: number, r: number): string {
  const h = 100 * RATIO
  const pts = [[50, 0], [100, h / 4], [100, (h * 3) / 4], [50, h], [0, (h * 3) / 4], [0, h / 4]]
  const p = pts.map(([x, y]) => {
    const dx = x - 50, dy = y - h / 2, d = Math.hypot(dx, dy)
    return [x - (dx / d) * inset, y - (dy / d) * inset]
  })
  const toward = (v: number[], q: number[]) => {
    const dx = q[0] - v[0], dy = q[1] - v[1], d = Math.hypot(dx, dy)
    return [v[0] + (dx / d) * r, v[1] + (dy / d) * r]
  }
  const f = (n: number) => n.toFixed(2)
  let path = ''
  p.forEach((v, i) => {
    const s = toward(v, p[(i + 5) % 6]), e = toward(v, p[(i + 1) % 6])
    path += `${i ? 'L' : 'M'}${f(s[0])} ${f(s[1])}Q${f(v[0])} ${f(v[1])} ${f(e[0])} ${f(e[1])}`
  })
  return path + 'Z'
}
const HEX = roundedHex(2.5, 9)
const HEX_INNER = roundedHex(8, 7)

// RESIZE is a diagonal double arrow, the sign for "drag to resize".
const RESIZE = 'M7 7l10 10M7 7h5M7 7v5M17 17h-5M17 17v-5'

const kinds = [
  { kind: 'counter', label: 'Counter', text: 'Starts at 0 every day. Double-click adds one; right-click to remove one or reset.' },
  { kind: 'done', label: 'Done for today', text: 'Starts undone every day. Double-click marks it done; right-click to undo.' },
] as const

const today = () => dateKey(new Date())

// place keeps a sticker inside the page's width and below its top. It may lie
// anywhere down the page: the page scrolls as far as the lowest sticker.
function place(s: Sticker, pageWidth: number): Sticker {
  return {
    ...s,
    X: Math.round(Math.min(Math.max(4, s.X), Math.max(4, pageWidth - s.W - 4))),
    Y: Math.round(Math.max(4, s.Y)),
  }
}

const label = (s: Sticker) =>
  s.Kind === 'done'
    ? `${s.Name}: ${s.Count ? 'done' : 'not done'} today. Double-click to mark done, right-click for options.`
    : `${s.Name}: ${s.Count} today. Double-click to add one, right-click for options.`

// Picture draws one of the sticker pictures (this app's own SVG markup).
function Picture({ pic }: { pic: string }) {
  return <svg viewBox="0 0 64 64" aria-hidden dangerouslySetInnerHTML={{ __html: (PICS[pic] ?? PICS.water).svg }} />
}

type Menu = { id: string; x: number; y: number; view: 'main' | 'pic' | 'rename' | 'delete' }

// Stickers is the layer of counters and done marks on the Today page, each
// pinned to the part of the page where it was put, so it scrolls with the page:
// double-click counts, right-click opens the menu, drag moves, the corner dot
// resizes. Each day starts at 0.
export function Stickers({ adding, onAddClose }: { adding: boolean; onAddClose: () => void }) {
  const [items, setItems] = useState<Sticker[]>([])
  const [menu, setMenu] = useState<Menu | null>(null)
  const [pop, setPop] = useState({ id: '', n: 0 })
  const [active, setActive] = useState('') // the sticker being dragged or resized
  const [front, setFront] = useState<Record<string, number>>({})
  const top = useRef(1)
  const day = useRef(today())
  const layer = useRef<HTMLDivElement>(null)
  const [, redraw] = useState(0)
  const page = () => layer.current?.closest('main') ?? null
  const pageWidth = () => page()?.clientWidth || window.innerWidth

  const load = () => {
    day.current = today()
    fetchStickers(day.current).then(setItems, () => {})
  }
  useEffect(() => {
    load()
    // A new day starts every sticker at 0; the page's width keeps them inside it.
    const t = setInterval(() => today() !== day.current && load(), 60_000)
    const resize = () => redraw((n) => n + 1)
    window.addEventListener('resize', resize)
    return () => {
      clearInterval(t)
      window.removeEventListener('resize', resize)
    }
  }, [])

  useEffect(() => {
    if (!menu) return
    const down = (e: Event) => !(e.target as HTMLElement).closest?.('[role="menu"]') && setMenu(null)
    const key = (e: globalThis.KeyboardEvent) => e.key === 'Escape' && setMenu(null)
    document.addEventListener('pointerdown', down)
    document.addEventListener('keydown', key)
    // The menu stays put while the sticker scrolls away from it. Scroll events do
    // not bubble, so this listens on the way down.
    document.addEventListener('scroll', down, true)
    return () => {
      document.removeEventListener('pointerdown', down)
      document.removeEventListener('keydown', key)
      document.removeEventListener('scroll', down, true)
    }
  }, [menu])

  const replace = (s: Sticker) => setItems((xs) => xs.map((x) => (x.ID === s.ID ? s : x)))
  const save = (s: Sticker) => {
    replace(s)
    putSticker(s).catch(load)
  }
  const count = (s: Sticker, n: number) => {
    const c = s.Kind === 'done' ? Math.min(1, Math.max(0, n)) : Math.max(0, n)
    replace({ ...s, Count: c })
    setStickerCount(s.ID, today(), c).catch(load)
    if (c > s.Count) setPop((p) => ({ id: s.ID, n: p.n + 1 }))
  }

  // drag moves a sticker, or resizes it from the corner dot, and saves where it ended.
  const drag = (e: PointerEvent, s: Sticker, resize: boolean) => {
    if (e.button !== 0) return
    e.preventDefault()
    e.stopPropagation()
    setMenu(null)
    setFront((f) => ({ ...f, [s.ID]: ++top.current }))
    const x0 = e.clientX, y0 = e.clientY
    const scroll0 = page()?.scrollTop ?? 0
    let moved = false
    let cur = s
    const move = (ev: MouseEvent) => {
      const dx = ev.clientX - x0, dy = ev.clientY - y0 + (page()?.scrollTop ?? 0) - scroll0
      if (!moved && Math.hypot(dx, dy) < 4) return
      moved = true
      setActive(s.ID)
      cur = resize
        ? { ...s, W: Math.round(Math.min(220, Math.max(MIN_W, s.W + Math.max(dx, dy / RATIO)))) }
        : place({ ...s, X: s.X + dx, Y: s.Y + dy }, pageWidth())
      replace(cur)
    }
    const up = () => {
      document.removeEventListener('pointermove', move)
      document.removeEventListener('pointerup', up)
      setActive('')
      if (moved) save(place(cur, pageWidth()))
    }
    document.addEventListener('pointermove', move)
    document.addEventListener('pointerup', up)
  }

  const keys = (e: KeyboardEvent, s: Sticker) => {
    if (e.key === 'Enter' || e.key === '+') count(s, s.Count + 1)
    else if (e.key === '-') count(s, s.Count - 1)
    else if (e.key === 'ContextMenu') {
      const r = e.currentTarget.getBoundingClientRect()
      setMenu({ id: s.ID, x: r.right - 10, y: r.top + 20, view: 'main' })
    } else return
    e.preventDefault()
  }

  const open = items.find((s) => s.ID === menu?.id)

  return (
    <>
      <svg width="0" height="0" className="absolute" aria-hidden dangerouslySetInnerHTML={{ __html: GRADIENTS }} />
      <div ref={layer} className="pointer-events-none absolute left-0 top-0 z-40 w-full" aria-label="Stickers">
        {items.map((raw) => {
          const s = place(raw, pageWidth())
          const done = s.Kind === 'done' && s.Count > 0
          return (
            <div
              key={s.ID}
              role="button"
              tabIndex={0}
              aria-label={label(s)}
              data-done={s.Kind === 'done' ? String(done) : undefined}
              className={`sticker pointer-events-auto ${active === s.ID ? 'sticker-active' : ''}`}
              style={{
                left: s.X,
                top: s.Y,
                width: s.W,
                height: s.W * RATIO,
                zIndex: front[s.ID] ?? 0,
                ['--rim' as string]: PICS[s.Pic]?.rim ?? 'var(--accent)',
                ['--sw' as string]: `${s.W}px`, // the pill's text scales with it
              }}
              onPointerDown={(e) => drag(e, s, false)}
              onDoubleClick={() => count(s, s.Count + 1)}
              onContextMenu={(e) => {
                e.preventDefault()
                setMenu({ id: s.ID, x: e.clientX, y: e.clientY, view: 'main' })
              }}
              onKeyDown={(e) => keys(e, s)}
            >
              <div className="sticker-hex">
                <svg viewBox={`0 0 100 ${100 * RATIO}`} aria-hidden>
                  <path className="sticker-fill" d={HEX} />
                  <path className="sticker-inner" d={HEX_INNER} />
                  <path className="sticker-rim" d={HEX} />
                </svg>
                <div key={pop.id === s.ID ? pop.n : 0} className={`sticker-pic ${pop.id === s.ID ? 'sticker-pop' : ''}`}>
                  <Picture pic={s.Pic} />
                </div>
              </div>
              <div key={pop.id === s.ID ? `c${pop.n}` : 'c'} className={`sticker-count ${pop.id === s.ID ? 'sticker-pop' : ''}`}>
                {s.Kind === 'counter' ? s.Count : done ? <Check /> : 'to do'}
              </div>
              {pop.id === s.ID && pop.n > 0 && (
                <div key={`p${pop.n}`} className="sticker-plus" aria-hidden>
                  {s.Kind === 'counter' ? '+1' : 'done'}
                </div>
              )}
              <div className="sticker-name" aria-hidden>
                {s.Name}
              </div>
              <div className="sticker-resize" title="Drag to resize" onPointerDown={(e) => drag(e, s, true)}>
                <svg viewBox="0 0 24 24" aria-hidden>
                  <path className="sticker-resize-halo" d={RESIZE} />
                  <path d={RESIZE} />
                </svg>
              </div>
            </div>
          )
        })}
      </div>
      {menu && open && <StickerMenu s={open} m={menu} setMenu={setMenu} count={count} save={save} remove={() => {
        setItems((xs) => xs.filter((x) => x.ID !== open.ID))
        setMenu(null)
        deleteSticker(open.ID).catch(load)
      }} />}
      {adding && (
        <AddSticker
          onClose={onAddClose}
          onAdd={(kind, pic, name) => {
            const n = items.length
            // It starts in the part of the page being looked at.
            const s = place({ ID: crypto.randomUUID(), Kind: kind, Pic: pic, Name: name, W: MIN_W, X: pageWidth() - 140 - (n % 4) * 30, Y: (page()?.scrollTop ?? 0) + 120 + (n % 4) * 34, Count: 0 }, pageWidth())
            setItems((xs) => [...xs, s])
            putSticker(s).catch(load)
            onAddClose()
          }}
        />
      )}
    </>
  )
}

function Check() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden className="mx-auto mt-1">
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </svg>
  )
}

const mi = 'flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-panel disabled:opacity-50 disabled:hover:bg-transparent'

// StickerMenu is a sticker's right-click menu.
function StickerMenu(props: {
  s: Sticker
  m: Menu
  setMenu: (m: Menu | null) => void
  count: (s: Sticker, n: number) => void
  save: (s: Sticker) => void
  remove: () => void
}) {
  const { s, m, setMenu, count, save } = props
  const go = (view: Menu['view']) => setMenu({ ...m, view })
  const act = (f: () => void) => () => {
    f()
    setMenu(null)
  }
  const [name, setName] = useState(s.Name)
  const rename = (e: FormEvent) => {
    e.preventDefault()
    if (name.trim()) save({ ...s, Name: name.trim() })
    setMenu(null)
  }
  return (
    <div
      role="menu"
      aria-label={s.Name}
      className="fixed z-50 w-[232px] rounded-[10px] border border-border bg-bg p-1.5 shadow-xl dark:bg-raised"
      style={{ left: Math.min(m.x, window.innerWidth - 240), top: Math.min(m.y, window.innerHeight - 330) }}
    >
      <div className="mb-1 flex items-center gap-2 border-b border-border px-1.5 pb-2 pt-1">
        <div className="h-7 w-7 shrink-0">
          <Picture pic={s.Pic} />
        </div>
        <div className="min-w-0">
          <b className="block truncate text-sm leading-4">{s.Name}</b>
          <small className="text-xs text-dim">{s.Kind === 'done' ? (s.Count ? 'Done today' : 'Not done today') : `${s.Count} today`}</small>
        </div>
      </div>
      {m.view === 'main' && (
        <>
          {s.Kind === 'counter' ? (
            <>
              <button role="menuitem" className={mi} onClick={act(() => count(s, s.Count + 1))}>
                Add one <kbd className="font-sans text-2xs text-dim">double-click</kbd>
              </button>
              <button role="menuitem" className={mi} disabled={!s.Count} onClick={act(() => count(s, s.Count - 1))}>
                Remove one
              </button>
              <button role="menuitem" className={mi} disabled={!s.Count} onClick={act(() => count(s, 0))}>
                Reset today to 0
              </button>
            </>
          ) : (
            <button role="menuitem" className={mi} onClick={act(() => count(s, s.Count ? 0 : 1))}>
              {s.Count ? 'Mark not done' : 'Mark done'}
            </button>
          )}
          <div className="mx-0.5 my-1 h-px bg-border" />
          <button role="menuitem" className={mi} onClick={() => go('pic')}>
            Change picture <span className="text-dim">›</span>
          </button>
          <button role="menuitem" className={mi} onClick={() => go('rename')}>
            Rename <span className="text-dim">›</span>
          </button>
          <div className="mx-0.5 my-1 h-px bg-border" />
          <button role="menuitem" className={`${mi} text-danger`} onClick={() => go('delete')}>
            Delete sticker
          </button>
        </>
      )}
      {m.view === 'pic' && (
        <PicGrid
          value={s.Pic}
          onPick={(pic) => {
            // a name that was only the old picture's follows the new one
            const named = Object.values(PICS).some((p) => p.name === s.Name)
            save({ ...s, Pic: pic, Name: named ? PICS[pic].name : s.Name })
            setMenu(null)
          }}
        />
      )}
      {m.view === 'rename' && (
        <form onSubmit={rename} className="flex gap-1.5 p-1">
          <input autoFocus value={name} maxLength={40} aria-label="Sticker name" onChange={(e) => setName(e.target.value)} className="min-w-0 flex-1 rounded-md border border-border bg-bg px-2 py-1 text-sm" />
          <button className="rounded-md bg-accent px-2.5 py-1 text-sm font-bold text-bg">Save</button>
        </form>
      )}
      {m.view === 'delete' && (
        <div className="p-1">
          <p className="mb-2 text-sm">Delete {s.Name} and its counts?</p>
          <div className="flex gap-1.5">
            <button className="rounded-md bg-danger px-2.5 py-1 text-sm font-bold text-white" onClick={props.remove}>
              Delete
            </button>
            <button className="rounded-md border border-border px-2.5 py-1 text-sm font-bold" onClick={() => go('main')}>
              Keep it
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function PicGrid({ value, onPick }: { value: string; onPick: (pic: string) => void }) {
  return (
    <div className="grid grid-cols-4 gap-1.5 p-1">
      {Object.entries(PICS).map(([key, p]) => (
        <button
          key={key}
          type="button"
          aria-label={p.name}
          title={p.name}
          aria-pressed={key === value}
          onClick={() => onPick(key)}
          className={`aspect-square rounded-lg border bg-bg p-1 ${key === value ? 'border-accent ring-2 ring-accent' : 'border-border hover:border-accent'}`}
        >
          <Picture pic={key} />
        </button>
      ))}
    </div>
  )
}

// AddSticker picks a new sticker's kind, picture and name. The name follows
// the picture until the user types their own, and the kind follows the
// picture's suggestion until the user picks one.
function AddSticker({ onClose, onAdd }: { onClose: () => void; onAdd: (kind: Sticker['Kind'], pic: string, name: string) => void }) {
  const [kind, setKind] = useState<Sticker['Kind']>('counter')
  const [pic, setPic] = useState('water')
  const [name, setName] = useState(PICS.water.name)
  const [typed, setTyped] = useState(false)
  const [chose, setChose] = useState(false)
  useEffect(() => {
    const key = (e: globalThis.KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [onClose])
  return (
    <div role="dialog" aria-label="Add a sticker" className="fixed right-6 top-16 z-50 w-[320px] max-w-[calc(100vw-32px)] rounded-xl border border-border bg-bg p-3 shadow-xl dark:bg-raised">
      <h2 className="text-base font-bold">Add a sticker</h2>
      <p className="mb-2 text-xs text-dim">It floats over the page; drag it anywhere, resize it from its corner.</p>
      <div role="radiogroup" aria-label="Kind" className="mb-3 flex flex-col gap-1.5">
        {kinds.map((k) => (
          <label key={k.kind} className={`flex cursor-pointer gap-2 rounded-lg border p-2 ${kind === k.kind ? 'border-accent' : 'border-border'}`}>
            <input
              type="radio"
              name="sticker-kind"
              checked={kind === k.kind}
              onChange={() => {
                setKind(k.kind)
                setChose(true)
              }}
              className="mt-1 accent-accent"
            />
            <span>
              <b className="block text-sm">{k.label}</b>
              <span className="text-xs text-dim">{k.text}</span>
            </span>
          </label>
        ))}
      </div>
      <div className="mb-1 text-xs font-bold text-dim">Picture</div>
      <PicGrid
        value={pic}
        onPick={(p) => {
          setPic(p)
          if (!typed) setName(PICS[p].name)
          if (!chose) setKind(PICS[p].kind ?? 'counter')
        }}
      />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onAdd(kind, pic, name.trim() || PICS[pic].name)
        }}
        className="mt-2"
      >
        <label htmlFor="sticker-name" className="mb-1 block text-xs font-bold text-dim">
          Name
        </label>
        <div className="flex gap-1.5">
          <input
            id="sticker-name"
            value={name}
            maxLength={40}
            onChange={(e) => {
              setName(e.target.value)
              setTyped(true)
            }}
            className="min-w-0 flex-1 rounded-md border border-border bg-bg px-2 py-1 text-sm"
          />
          <button className="rounded-md bg-accent px-2.5 py-1 text-sm font-bold text-bg">Add sticker</button>
        </div>
        <button type="button" onClick={onClose} className="mt-2 text-xs text-dim hover:text-fg">
          Cancel
        </button>
      </form>
    </div>
  )
}
