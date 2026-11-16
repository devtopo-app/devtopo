import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeToggle } from './ThemeToggle'

beforeEach(() => {
  try {
    localStorage.clear()
  } catch {
    // no storage: the default theme
  }
})

describe('ThemeToggle', () => {
  it('draws a larger outline icon, a moon in light mode and a sun in dark mode', async () => {
    render(<ThemeToggle />)
    const button = screen.getByRole('button', { name: 'Toggle theme' })
    const before = button.querySelector('svg')!
    expect(before).toHaveAttribute('width', '20')
    expect(before).toHaveAttribute('fill', 'none') // lines only, no filled shape
    const first = before.innerHTML
    await userEvent.click(button)
    expect(button.querySelector('svg')!.innerHTML).not.toBe(first) // the other icon
    expect(button.textContent).toBe('') // no ☀ or ☾ text glyphs
  })
})
