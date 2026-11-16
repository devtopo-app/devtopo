import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Stickers } from './Stickers'
import * as api from '../api'
import type { Sticker } from '../api'
import { dateKey } from '../lib/timeline'

vi.mock('../api', async (orig) => ({
  ...(await orig<typeof import('../api')>()),
  fetchStickers: vi.fn(),
  putSticker: vi.fn(),
  setStickerCount: vi.fn(),
  deleteSticker: vi.fn(),
}))
const fetchStickers = vi.mocked(api.fetchStickers)
const putSticker = vi.mocked(api.putSticker)
const setStickerCount = vi.mocked(api.setStickerCount)
const deleteSticker = vi.mocked(api.deleteSticker)

const water: Sticker = { ID: 'w', Kind: 'counter', Pic: 'water', Name: 'Water', X: 400, Y: 100, W: 104, Count: 5 }
const pills: Sticker = { ID: 'p', Kind: 'done', Pic: 'pills', Name: 'Vitamins', X: 600, Y: 100, W: 96, Count: 0 }
const today = () => dateKey(new Date())

beforeEach(() => {
  vi.clearAllMocks()
  fetchStickers.mockResolvedValue([water, pills])
  putSticker.mockResolvedValue()
  setStickerCount.mockResolvedValue()
  deleteSticker.mockResolvedValue()
})

const sticker = (name: RegExp) => screen.findByRole('button', { name })

describe('Stickers', () => {
  it("shows each sticker with today's count, a done one as to do", async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    expect(fetchStickers).toHaveBeenCalledWith(today())
    expect(await sticker(/^Water: 5 today/)).toHaveTextContent('5')
    expect(await sticker(/^Vitamins: not done today/)).toBeInTheDocument()
  })

  it('gives the CSS its width, so a small sticker gets a smaller one-line pill', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    expect(await sticker(/^Vitamins/)).toHaveStyle({ '--sw': '96px' })
  })

  it('double-click adds one to a counter', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    fireEvent.doubleClick(await sticker(/^Water/))
    expect(setStickerCount).toHaveBeenCalledWith('w', today(), 6)
    expect(await sticker(/^Water: 6 today/)).toHaveTextContent('6')
  })

  it('double-click marks a done sticker done', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    fireEvent.doubleClick(await sticker(/^Vitamins/))
    expect(setStickerCount).toHaveBeenCalledWith('p', today(), 1)
    expect(await sticker(/^Vitamins: done today/)).toBeInTheDocument()
  })

  it('right-click opens the menu: a counter removes one, a done sticker is marked not done', async () => {
    fetchStickers.mockResolvedValue([water, { ...pills, Count: 1 }])
    render(<Stickers adding={false} onAddClose={() => {}} />)
    fireEvent.contextMenu(await sticker(/^Water/))
    await userEvent.click(within(screen.getByRole('menu', { name: 'Water' })).getByRole('menuitem', { name: 'Remove one' }))
    expect(setStickerCount).toHaveBeenCalledWith('w', today(), 4)

    fireEvent.contextMenu(await sticker(/^Vitamins/))
    const menu = within(screen.getByRole('menu', { name: 'Vitamins' }))
    expect(menu.queryByRole('menuitem', { name: 'Remove one' })).toBeNull()
    await userEvent.click(menu.getByRole('menuitem', { name: 'Mark not done' }))
    expect(setStickerCount).toHaveBeenCalledWith('p', today(), 0)
  })

  it('deletes only after a second click', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    fireEvent.contextMenu(await sticker(/^Water/))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete sticker' }))
    expect(deleteSticker).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(deleteSticker).toHaveBeenCalledWith('w')
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Water/ })).toBeNull())
  })

  it('resizes from a corner handle drawn as a diagonal double arrow', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    await sticker(/^Water/)
    const handle = screen.getAllByTitle('Drag to resize')[0]
    expect(handle.querySelector('svg path')?.getAttribute('d')).toMatch(/M7 7.*17 17/)
    const ev = (type: string, x: number, y: number) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 })
    fireEvent(handle, ev('pointerdown', 500, 200))
    fireEvent(document, ev('pointermove', 540, 200))
    fireEvent(document, ev('pointerup', 540, 200))
    expect(putSticker).toHaveBeenCalledWith(expect.objectContaining({ ID: 'w', W: 144 }))
  })

  it('moves a sticker where it is dragged and saves the place', async () => {
    render(<Stickers adding={false} onAddClose={() => {}} />)
    const el = await sticker(/^Water/)
    const ev = (type: string, x: number, y: number) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 })
    fireEvent(el, ev('pointerdown', 410, 110))
    fireEvent(document, ev('pointermove', 310, 210))
    fireEvent(document, ev('pointerup', 310, 210))
    expect(putSticker).toHaveBeenCalledWith(expect.objectContaining({ ID: 'w', X: 300, Y: 200 }))
  })

  describe('pinned to the part of the page where it was put', () => {
    const ev = (type: string, x: number, y: number) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 })
    const inPage = () => {
      const { container } = render(
        <main>
          <Stickers adding={false} onAddClose={() => {}} />
        </main>,
      )
      return container.querySelector('main') as HTMLElement
    }

    it('lies in the page, not fixed to the window', async () => {
      inPage()
      await sticker(/^Water/)
      const layer = screen.getByLabelText('Stickers')
      expect(layer).toHaveClass('absolute')
      expect(layer).not.toHaveClass('fixed')
    })

    it('stays far down a long page, below the height of the window', async () => {
      fetchStickers.mockResolvedValue([{ ...water, Y: 3000 }])
      inPage()
      expect(await sticker(/^Water/)).toHaveStyle({ top: '3000px' })
    })

    it('counts the page scrolled during a drag', async () => {
      const main = inPage()
      const el = await sticker(/^Water/)
      fireEvent(el, ev('pointerdown', 410, 110))
      main.scrollTop = 300
      fireEvent(document, ev('pointermove', 420, 110))
      fireEvent(document, ev('pointerup', 420, 110))
      expect(putSticker).toHaveBeenCalledWith(expect.objectContaining({ ID: 'w', X: 410, Y: 400 }))
    })

    it('keeps inside the width of the page, not of the window', async () => {
      const main = inPage()
      Object.defineProperty(main, 'clientWidth', { value: 500 })
      window.dispatchEvent(new Event('resize'))
      await waitFor(async () => expect(await sticker(/^Vitamins/)).toHaveStyle({ left: '400px' }))
    })

    it('starts a new sticker in the part of the page being looked at', async () => {
      fetchStickers.mockResolvedValue([])
      const { container, rerender } = render(
        <main>
          <Stickers adding={false} onAddClose={() => {}} />
        </main>,
      )
      const main = container.querySelector('main') as HTMLElement
      main.scrollTop = 500
      rerender(
        <main>
          <Stickers adding={true} onAddClose={() => {}} />
        </main>,
      )
      await userEvent.click(within(await screen.findByRole('dialog', { name: 'Add a sticker' })).getByRole('button', { name: 'Add sticker' }))
      expect(putSticker).toHaveBeenCalledWith(expect.objectContaining({ Y: 620 }))
    })

    it('closes its menu when the page scrolls, as the sticker moves away from it', async () => {
      const main = inPage()
      fireEvent.contextMenu(await sticker(/^Water/))
      expect(screen.getByRole('menu', { name: 'Water' })).toBeInTheDocument()
      fireEvent.scroll(main)
      await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    })
  })

  it('adds a sticker: the kind with what double-click does, the picture and the name', async () => {
    fetchStickers.mockResolvedValue([])
    const closed = vi.fn()
    render(<Stickers adding={true} onAddClose={closed} />)
    const dialog = within(await screen.findByRole('dialog', { name: 'Add a sticker' }))
    expect(dialog.getByText(/Double-click adds one/)).toBeInTheDocument()
    expect(dialog.getByText(/Double-click marks it done/)).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('radio', { name: /Done for today/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Pills' }))
    expect(dialog.getByLabelText('Name')).toHaveValue('Pills')
    await userEvent.click(dialog.getByRole('button', { name: 'Add sticker' }))
    // a new sticker starts at the smallest size
    expect(putSticker).toHaveBeenCalledWith(expect.objectContaining({ Kind: 'done', Pic: 'pills', Name: 'Pills', Count: 0, W: 72 }))
    expect(closed).toHaveBeenCalled()
    expect(await sticker(/^Pills: not done today/)).toBeInTheDocument()
  })

  it('offers sixteen pictures; a reminder picture suggests Done for today until the kind is chosen', async () => {
    fetchStickers.mockResolvedValue([])
    render(<Stickers adding={true} onAddClose={() => {}} />)
    const dialog = within(await screen.findByRole('dialog', { name: 'Add a sticker' }))
    for (const name of ['Call', 'Message', 'Count', 'Done', 'Sleep', 'Eye break', 'Meditate', 'Meal']) {
      expect(dialog.getByRole('button', { name })).toBeInTheDocument()
    }
    expect(dialog.getAllByRole('button', { pressed: false }).length + 1).toBe(16)
    const done = dialog.getByRole('radio', { name: /Done for today/ })
    await userEvent.click(dialog.getByRole('button', { name: 'Call' }))
    expect(done).toBeChecked()
    await userEvent.click(dialog.getByRole('button', { name: 'Count' }))
    expect(dialog.getByRole('radio', { name: /Counter/ })).toBeChecked()
    // once the kind is picked by hand, a picture no longer changes it
    await userEvent.click(done)
    await userEvent.click(dialog.getByRole('button', { name: 'Meal' }))
    expect(done).toBeChecked()
  })
})
