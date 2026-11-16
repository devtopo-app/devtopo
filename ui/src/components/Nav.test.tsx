import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Nav } from './Nav'
import { checkUpdate, fetchUpdate, type UpdateView } from '../api'

vi.mock('../api', () => ({ fetchUpdate: vi.fn(), checkUpdate: vi.fn() }))

const noUpdate = { Current: 'v0.1.0', Latest: 'v0.1.0', Available: false, Supported: true, Error: '' } as UpdateView

beforeEach(() => {
  vi.mocked(fetchUpdate).mockResolvedValue(noUpdate)
  vi.mocked(checkUpdate).mockReset()
})

async function openMenu() {
  render(<Nav route="today" />)
  await userEvent.click(screen.getByRole('button', { name: 'Help and about' }))
  return within(screen.getByRole('menu', { name: 'Help and about' }))
}

describe('Nav', () => {
  it('the tabs are links (Today active), with no "soon" placeholders', () => {
    render(<Nav route="today" />)
    const today = screen.getByRole('link', { name: /Today/ })
    expect(today).toHaveAttribute('aria-current', 'page')
    expect(today).toHaveAttribute('href', '#/')
    expect(screen.getByRole('link', { name: /Planner/ })).toHaveAttribute('href', '#planner')
    expect(screen.getByRole('link', { name: /History/ })).toHaveAttribute('href', '#history')
    expect(screen.queryByText(/Projects/)).toBeNull()
    expect(screen.queryByText(/soon/i)).toBeNull()
  })

  it('marks the active route', () => {
    render(<Nav route="planner" />)
    expect(screen.getByRole('link', { name: /Planner/ })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: /Today/ })).not.toHaveAttribute('aria-current')
  })

  it('shows an icon left of each tab name', () => {
    render(<Nav route="today" />)
    for (const name of [/Today/, /Planner/, /History/]) {
      const link = screen.getByRole('link', { name })
      expect(link.firstElementChild?.tagName.toLowerCase()).toBe('svg')
    }
  })

  it('puts Settings and the help menu at the bottom as icons, named on mouse-over', () => {
    render(<Nav route="help" />)
    const bottom = within(screen.getByRole('group', { name: 'Settings and help' }))
    const settings = bottom.getByRole('link', { name: 'Settings' })
    const help = bottom.getByRole('button', { name: 'Help and about' })
    expect(settings).toHaveAttribute('href', '#settings')
    expect(help).toHaveAttribute('aria-haspopup', 'menu')
    expect(help).toHaveClass('text-accent') // on the Help page
    for (const l of [settings, help]) {
      expect(l.querySelector('svg')).not.toBeNull()
      // the name is a tooltip, shown only on hover
      expect(within(l).getByRole('tooltip')).toHaveClass('hidden', 'group-hover:block')
    }
  })

  it('the question mark opens Help, About and Check for updates', async () => {
    render(<Nav route="today" />)
    const help = screen.getByRole('button', { name: 'Help and about' })
    expect(screen.queryByRole('menu')).toBeNull()
    await userEvent.click(help)
    expect(help).toHaveAttribute('aria-expanded', 'true')
    const menu = within(screen.getByRole('menu', { name: 'Help and about' }))
    expect(menu.getByRole('menuitem', { name: 'Help' })).toHaveAttribute('href', '#help')
    expect(menu.getByRole('menuitem', { name: 'About' })).toHaveAttribute('href', '#settings?section=about')
    expect(menu.getByRole('menuitem', { name: /Check for updates/ }).tagName).toBe('BUTTON')
    expect(screen.queryByText(/About & updates/)).toBeNull()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('a dot on the question mark and the version next to Check for updates say an update is out', async () => {
    vi.mocked(fetchUpdate).mockResolvedValue({ ...noUpdate, Latest: 'v0.2.0', Available: true })
    render(<Nav route="today" />)
    const help = screen.getByRole('button', { name: 'Help and about' })
    await waitFor(() => expect(help.querySelector('.rounded-full')).not.toBeNull())
    await userEvent.click(help)
    expect(screen.getByRole('menuitem', { name: /Check for updates/ })).toHaveTextContent('v0.2.0')
  })

  it('checks for updates in the menu, without leaving the page, and says it is up to date', async () => {
    let resolve!: (v: UpdateView) => void
    vi.mocked(checkUpdate).mockReturnValue(new Promise((r) => (resolve = r)))
    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: /Check for updates/ }))
    expect(checkUpdate).toHaveBeenCalledTimes(1)
    const live = screen.getByRole('status')
    expect(live).toHaveAttribute('aria-live', 'polite')
    expect(live).toHaveTextContent(/Checking/)
    expect(menu.getByRole('menuitem', { name: /Check/ })).toBeDisabled()
    resolve({ ...noUpdate, Latest: 'v0.1.0' })
    await waitFor(() => expect(live).toHaveTextContent("You're up to date (v0.1.0)"))
    expect(screen.getByRole('menu')).toBeInTheDocument() // stays open
    await userEvent.click(document.body)
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('a found update shows its version, its release notes and how to update', async () => {
    vi.mocked(checkUpdate).mockResolvedValue({
      ...noUpdate,
      Latest: 'v0.3.0',
      Available: true,
      URL: 'https://example.invalid/releases/v0.3.0',
    })
    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: /Check for updates/ }))
    const live = screen.getByRole('status')
    await waitFor(() => expect(live).toHaveTextContent('Update available: v0.3.0'))
    const notes = within(live).getByRole('link', { name: /release notes/i })
    expect(notes).toHaveAttribute('href', 'https://example.invalid/releases/v0.3.0')
    expect(notes).toHaveAttribute('target', '_blank')
    expect(notes).toHaveAttribute('rel', 'noreferrer')
    expect(within(live).getByRole('link', { name: /how to update/i })).toHaveAttribute('href', '#settings?section=updates')
    // the dot follows the check
    expect(screen.getByRole('button', { name: 'Help and about' }).querySelector('.rounded-full')).not.toBeNull()
  })

  it('shows a failed check', async () => {
    vi.mocked(checkUpdate).mockResolvedValue({ ...noUpdate, Latest: '', Error: 'could not reach GitHub' })
    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: /Check for updates/ }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(/could not reach GitHub/))
  })

  it('shows when the daemon cannot be reached', async () => {
    vi.mocked(checkUpdate).mockRejectedValue(new Error('500'))
    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: /Check for updates/ }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(/could not check/i))
  })

  it('a build from source says checks run in release builds', async () => {
    vi.mocked(checkUpdate).mockResolvedValue({ ...noUpdate, Current: 'dev', Latest: '', Supported: false })
    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: /Check for updates/ }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Update checks run in release builds (this is dev)'),
    )
  })

  it('collapsed, the tabs show only their icons', async () => {
    render(<Nav route="today" />)
    const toggle = screen.getByLabelText('Toggle navigation')
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    // the toggle is an icon the size of the tab icons, not a « character
    expect(toggle.querySelector('svg')).toHaveAttribute('width', '18')
    expect(toggle.textContent).toBe('')
    expect(screen.getByText('Planner')).not.toHaveClass('sr-only')
    await userEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText('Planner')).toHaveClass('sr-only')
    expect(screen.getByRole('link', { name: /Planner/ }).querySelector('svg')).not.toBeNull()
    expect(screen.queryByText('P')).toBeNull() // no first-letter stand-in
  })
})

// Today is the dashboard, so its icon is a gauge: a dial and its needle.
describe('the Today icon', () => {
  it('is a gauge', () => {
    render(<Nav route="today" />)
    const svg = screen.getByRole('link', { name: /Today/ }).querySelector('svg')!
    expect(svg.querySelectorAll('rect')).toHaveLength(0)
    expect(svg.querySelector('[data-part="dial"]')).not.toBeNull()
    expect(svg.querySelector('[data-part="needle"]')).not.toBeNull()
  })
})
