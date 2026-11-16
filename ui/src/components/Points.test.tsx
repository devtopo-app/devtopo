import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Points } from './Points'

describe('Points', () => {
  it('shows the time of a point apart from its sentence', () => {
    render(<Points points={['09:12 Fixed the Jira sync.', 'Added a button.']} />)
    const items = screen.getAllByRole('listitem')
    expect(items[0].querySelector('time')).toHaveTextContent('09:12')
    expect(items[0]).toHaveTextContent('09:12Fixed the Jira sync.')
    expect(items[1].querySelector('time')).toBeNull()
    expect(items[1]).toHaveTextContent('Added a button.')
  })
})
