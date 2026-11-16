import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
// @ts-expect-error the ui has no node types; vitest runs on node, and a ?raw css import comes back empty
import { readFileSync } from 'node:fs'
import { fireEvent, render, screen } from '@testing-library/react'
import { Timeline, ticketColor } from './Timeline'

// DEV-18: a ticket's colour says where it stands.
describe('ticketColor', () => {
  it('is orange in progress, green in review, teal to do, faded when done, quiet otherwise', () => {
    expect(ticketColor('started', 'In Progress')).toBe('var(--waiting)')
    expect(ticketColor('started', 'In Review')).toBe('var(--done)')
    expect(ticketColor('started', 'Ready for QA')).toBe('var(--done)')
    expect(ticketColor('unstarted', 'Todo')).toBe('var(--accent)')
    expect(ticketColor('completed', 'Done')).toBe('var(--idle)')
    expect(ticketColor('canceled', 'Canceled')).toBe('var(--idle)')
    expect(ticketColor('backlog', 'Backlog')).toBe('var(--dim)')
  })
})

const css: string = readFileSync('src/index.css', 'utf8') // vitest runs from ui/

const iso = (h: number) => new Date(new Date('2026-10-29T00:00').getTime() + h * 3600 * 1000).toISOString()
const noop = () => {}

describe('Timeline', () => {
  // jsdom applies no stylesheet, so the CSS half is read as text: no hover rule
  // may give a block another width, or its right edge stops being its end.
  it('keeps a hovered block at its own width and names it in a label instead', () => {
    expect(css).not.toMatch(/\.(blk|narrow)[^{]*:hover[^{]*\{[^}]*width/)
    render(
      <Timeline
        date="2026-10-29"
        view={{ from: 8, to: 19 }}
        win={{ start: 9, end: 18 }}
        rows={[{ ID: 'r1', PlanID: 'p1', Name: 'Work', Order: 0 }]}
        items={[{ ID: 'i1', PlanID: 'p1', RowID: 'r1', RefSource: '', RefID: '', Text: 'Long focus block', Start: iso(10), IntendedHours: 3, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null }]}
        meetings={[{ Title: 'Standup', Start: iso(9), End: iso(9.5) }]}
        over={null}
        editing={null}
        focusRow={null}
        onOver={noop}
        onWindowDrag={noop}
        onWindowDrop={noop}
        onMove={noop}
        onResize={noop}
        onRemove={noop}
        onAddActivity={noop}
        onEdit={noop}
        onRename={noop}
        onRenameRow={noop}
        onRemoveRow={noop}
        onAddRow={noop}
      />,
    )
    for (const [name, range] of [['Long focus block', '10:00-13:00 · 3h'], ['Standup', '9:00-9:30 · 30m']]) {
      const blk = screen.getByText(name).closest('.blk') as HTMLElement
      const width = blk.style.width
      fireEvent.pointerOver(blk)
      expect(blk.style.width).toBe(width)
      const label = screen.getByRole('tooltip')
      expect(label).toHaveTextContent(name)
      expect(label).toHaveTextContent(range)
      expect(label).toHaveClass('pointer-events-none')
      fireEvent.pointerOut(blk)
      expect(screen.queryByRole('tooltip')).toBeNull()
    }
  })

  // A block no longer widens on hover, so a short one is too small to hold its
  // remove button: it sits on the block's corner, outside its clipped box.
  it('shows the remove button outside a short hovered block', () => {
    render(
      <Timeline
        date="2026-10-29"
        view={{ from: 8, to: 19 }}
        win={{ start: 9, end: 18 }}
        rows={[{ ID: 'r1', PlanID: 'p1', Name: 'Work', Order: 0 }]}
        items={[{ ID: 'i1', PlanID: 'p1', RowID: 'r1', RefSource: '', RefID: '', Text: 'Coffee', Start: iso(10), IntendedHours: 0.25, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null }]}
        meetings={[]}
        over={null}
        onOver={noop}
        onMove={noop}
        onResize={noop}
        onRemove={noop}
      />,
    )
    const blk = screen.getByText('Coffee').closest('.blk')!
    const remove = screen.getByRole('button', { name: 'Delete Coffee' })
    expect(blk.contains(remove)).toBe(false)
    expect(remove).toHaveClass('hidden')
    fireEvent.pointerOver(blk)
    expect(remove).not.toHaveClass('hidden')
  })

  describe('resize', () => {
    const ev = (type: string, x: number) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: 10 })
    const day = () =>
      render(
        <Timeline
          date="2026-10-29"
          view={{ from: 8, to: 19 }}
          win={{ start: 9, end: 18 }}
          rows={[{ ID: 'r1', PlanID: 'p1', Name: 'Work', Order: 0 }]}
          items={[{ ID: 'i1', PlanID: 'p1', RowID: 'r1', RefSource: '', RefID: '', Text: 'Write the spec', Start: iso(10), IntendedHours: 1, RefState: '', RefStage: '', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null }]}
          meetings={[]}
          over={null}
          onOver={noop}
          onMove={noop}
          onResize={noop}
        />,
      )

    it('has a visible grip at the block end', () => {
      day()
      const grab = screen.getByText('Write the spec').closest('.blk')!.querySelector('[data-grab]')!
      expect(grab).toHaveClass('right-0', 'cursor-ew-resize')
      expect(grab.firstElementChild).toHaveClass('group-hover:opacity-100')
    })

    it('shows the live end and length while resizing', () => {
      day()
      const grab = screen.getByText('Write the spec').closest('.blk')!.querySelector('[data-grab]')!
      fireEvent(grab, ev('pointerdown', 168))
      fireEvent(document, ev('pointermove', 252)) // 252px is 12:30 at 56px an hour from 8:00
      expect(screen.getByRole('tooltip')).toHaveTextContent('until 12:30 · 2h 30m')
      fireEvent(document, ev('pointerup', 252))
      expect(screen.queryByRole('tooltip')).toBeNull()
    })

    it('shows the live range while moving', () => {
      day()
      const blk = screen.getByText('Write the spec').closest('.blk')!
      fireEvent(blk, ev('pointerdown', 120))
      fireEvent(document, ev('pointermove', 176)) // one hour later
      expect(screen.getByRole('tooltip')).toHaveTextContent('11:00-12:00 · 1h')
      fireEvent(document, ev('pointerup', 176))
    })
  })

  it('marks a block whose ticket is done as finished', () => {
    render(
      <Timeline
        date="2026-10-29"
        view={{ from: 8, to: 19 }}
        win={{ start: 9, end: 18 }}
        rows={[{ ID: 'r1', PlanID: 'p1', Name: 'Work', Order: 0 }]}
        items={[
          { ID: 'i1', PlanID: 'p1', RowID: 'r1', RefSource: 'linear', RefID: 'L1', Text: 'Ship it', Start: iso(10), IntendedHours: 2, RefState: 'Done', RefStage: 'completed', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null },
          { ID: 'i2', PlanID: 'p1', RowID: 'r1', RefSource: 'linear', RefID: 'L2', Text: 'Still open', Start: iso(13), IntendedHours: 2, RefState: 'Todo', RefStage: 'unstarted', RefRepo: '', RefURL: '', RefProject: '', RefLabels: [], DoneAt: null },
        ]}
        meetings={[]}
        over={null}
        editing={null}
        focusRow={null}
        onOver={noop}
        onWindowDrag={noop}
        onWindowDrop={noop}
        onMove={noop}
        onResize={noop}
        onRemove={noop}
        onAddActivity={noop}
        onEdit={noop}
        onRename={noop}
        onRenameRow={noop}
        onRemoveRow={noop}
        onAddRow={noop}
      />,
    )
    const done = screen.getByText(/Ship it/)
    expect(done.textContent).toBe('✓ Ship it')
    expect(done).toHaveClass('line-through')
    expect(screen.getByText('Still open')).not.toHaveClass('line-through')
  })

  describe('now line', () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ['Date'] })
      vi.setSystemTime(new Date('2026-10-29T11:30'))
    })
    afterEach(() => vi.useRealTimers())

    const day = (date: string) =>
      render(
        <Timeline
          date={date}
          view={{ from: 8, to: 19 }}
          win={{ start: 9, end: 18 }}
          rows={[{ ID: 'r1', PlanID: 'p1', Name: 'Work', Order: 0 }]}
          items={[]}
          meetings={[]}
          over={null}
          editing={null}
          focusRow={null}
          onOver={noop}
          onWindowDrag={noop}
          onWindowDrop={noop}
          onMove={noop}
          onResize={noop}
          onRemove={noop}
          onAddActivity={noop}
          onEdit={noop}
          onRename={noop}
          onRenameRow={noop}
          onRemoveRow={noop}
          onAddRow={noop}
        />,
      )

    it('marks the current time on a plan for today', () => {
      day('2026-10-29')
      expect(screen.getByTestId('now-line')).toHaveTextContent('11:30')
    })

    it('is absent on a plan for another day', () => {
      day('2026-10-30')
      expect(screen.queryByTestId('now-line')).toBeNull()
    })
  })
})
