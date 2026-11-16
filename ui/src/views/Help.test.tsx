import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Help } from './Help'

afterEach(() => vi.unstubAllGlobals())

describe('Help', () => {
  it('shows the setup guide, with outside links opening in a new tab', async () => {
    const md = '# DevTopo\n\n## Jira\n\n1. Go to [the token page](https://id.atlassian.com/x).\n2. Tick `read:jira-work`.\n'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(md, { status: 200 })))
    render(<Help />)
    expect(await screen.findByRole('heading', { name: 'Jira' })).toBeInTheDocument()
    expect(screen.getByText('read:jira-work').tagName).toBe('CODE')
    const link = screen.getByRole('link', { name: 'the token page' })
    expect(link).toHaveAttribute('href', 'https://id.atlassian.com/x')
    expect(link).toHaveAttribute('target', '_blank')
  })
})
