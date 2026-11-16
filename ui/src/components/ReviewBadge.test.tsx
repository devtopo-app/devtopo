import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ReviewBadge } from './ReviewBadge'

describe('ReviewBadge', () => {
  it.each([
    ['changes_requested', 'Changes requested'],
    ['commented', 'Commented'],
    ['approved', 'Approved'],
  ])('names %s', (state, label) => {
    render(<ReviewBadge state={state} />)
    expect(screen.getByText(label)).toBeInTheDocument()
  })

  it('shows nothing without a known state', () => {
    const { container } = render(<ReviewBadge state="" />)
    expect(container).toBeEmptyDOMElement()
  })
})
