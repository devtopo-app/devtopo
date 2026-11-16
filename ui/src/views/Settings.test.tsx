import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Settings } from './Settings'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let fetchMock: Mock<(url: string, init?: RequestInit) => Promise<any>>
let calendarStatus: string
let jiraStatus: string
let githubStatus: string
let gitlabStatus: string
let gitlabError: string | undefined
let githubAccount: string | undefined
let calendars: { ID: string; Label: string; Name?: string; Host: string; Status: string; Error?: string }[]
let updateView: Record<string, unknown>

beforeEach(() => {
  calendarStatus = 'not configured'
  jiraStatus = 'not configured'
  githubStatus = 'connected'
  gitlabStatus = 'not configured'
  gitlabError = undefined
  githubAccount = 'octocat'
  calendars = []
  updateView = { Current: 'dev', Supported: false, Enabled: true, Available: false, Install: 'direct', Hint: 'download from the release page' }
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/update' || url === '/api/update/check') {
      return { ok: true, json: async () => updateView }
    }
    if (url === '/api/update/enabled') {
      updateView = { ...updateView, Enabled: JSON.parse(String(init?.body)).enabled }
      return { ok: true, json: async () => updateView }
    }
    if (url === '/api/calendars') {
      return { ok: true, json: async () => calendars }
    }
    if (url === '/api/connectors') {
      return {
        ok: true,
        json: async () => [
          { Name: 'GitHub', Status: githubStatus, Detail: 'reviews + PRs', Account: githubAccount },
          { Name: 'Linear', Status: 'not configured', Detail: 'tasks' },
          { Name: 'Calendar', Status: calendarStatus, Detail: 'Calendars from Google, Outlook and Microsoft Teams, Apple or any other, read through their private iCal address' },
          {
            Name: 'Jira',
            Status: jiraStatus,
            Detail: 'tasks',
            Values: { site: 'https://example.atlassian.net', email: 'me@example.com' },
          },
          { Name: 'GitLab', Status: gitlabStatus, Detail: 'merge requests', Values: { site: '' }, SyncError: gitlabError },
          { Name: 'Azure DevOps', Status: 'not configured', Detail: 'pull requests', Values: { organization: '' } },
        ],
      }
    }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
})

function postedTo(endpoint: string) {
  return fetchMock.mock.calls.find((call) => call[0] === endpoint && call[1]?.method === 'POST')
}

// open expands a service's card by clicking its header, which names it first.
const open = async (name: string) => {
  const header = await screen.findByRole('button', { name: new RegExp(`^${name}\\b`) })
  if (header.getAttribute('aria-expanded') !== 'true') await userEvent.click(header)
  return header
}
// card is the whole card of a service, found from its header.
const card = (header: HTMLElement) => within(header.closest('li')!)

describe('Settings', () => {
  it('groups the services by type, each card closed to its name and status', async () => {
    render(<Settings />)
    const hosts = within(await screen.findByRole('region', { name: 'Code hosts' }))
    expect(hosts.getAllByRole('button', { expanded: false }).map((b) => b.textContent)).toEqual([
      'GitHubconnected',
      'GitLabnot configured',
      'Azure DevOpsnot configured',
    ])
    const trackers = within(screen.getByRole('region', { name: 'Issue trackers' }))
    expect(trackers.getAllByRole('button', { expanded: false }).map((b) => b.textContent)).toEqual(['Linearnot configured', 'Jiranot configured'])
    expect(within(screen.getByRole('region', { name: 'Calendars' })).getByRole('button', { name: /^Calendar\b/ })).toBeInTheDocument()
    // Closed, a card shows no fields, help or description.
    expect(screen.queryByRole('button', { name: /^connect$/i })).toBeNull()
    expect(screen.queryByLabelText(/linear api key/i)).toBeNull()
    expect(screen.queryByText('tasks')).toBeNull()
  })

  it('gives each service an icon', async () => {
    render(<Settings />)
    for (const name of ['GitHub', 'GitLab', 'Azure DevOps', 'Linear', 'Jira', 'Calendar']) {
      expect((await open(name)).querySelector('svg')).not.toBeNull()
    }
  })

  it('opens a card on a click, with its description, its own help and its fields, and closes it again', async () => {
    render(<Settings />)
    const header = await open('Linear')
    expect(header).toHaveAttribute('aria-expanded', 'true')
    const linear = card(header)
    expect(linear.getByText('tasks')).toBeInTheDocument()
    expect(linear.getByText(/Personal API keys/)).toBeInTheDocument()
    expect(linear.getByLabelText(/linear api key/i)).toBeInTheDocument()
    // Another service's help stays in its own card.
    expect(screen.queryByText(/read_api/)).toBeNull()
    await userEvent.click(header)
    expect(header).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByLabelText(/linear api key/i)).toBeNull()
  })

  it('keeps the setup notes in each card, not in one block after them', async () => {
    render(<Settings />)
    await screen.findByRole('region', { name: 'Code hosts' })
    expect(screen.queryByText(/Linear: Settings - Security/)).toBeNull()
    expect(screen.getByText(/stored only on this machine/)).toBeInTheDocument()
  })

  it('connects Jira by posting site, email and token; only the token is hidden', async () => {
    render(<Settings />)
    const jira = card(await open('Jira'))
    expect(jira.getByLabelText(/jira site/i)).toHaveAttribute('type', 'text')
    expect(jira.getByLabelText(/jira api token/i)).toHaveAttribute('type', 'password')
    expect(jira.getByRole('link', { name: /api token/i })).toHaveAttribute('href', 'https://id.atlassian.com/manage-profile/security/api-tokens')
    await userEvent.type(jira.getByLabelText(/jira site/i), 'example.atlassian.net')
    await userEvent.type(jira.getByLabelText(/email/i), 'me@example.com')
    await userEvent.type(jira.getByLabelText(/jira api token/i), 'jira_test_token')
    await userEvent.click(jira.getByRole('button', { name: /^connect$/i }))
    await waitFor(() => {
      const posted = postedTo('/api/connectors/jira')
      expect(posted).toBeTruthy()
      expect(JSON.parse(String(posted![1]!.body))).toEqual({ site: 'example.atlassian.net', email: 'me@example.com', apiToken: 'jira_test_token' })
    })
  })

  it('connects Linear by posting the key', async () => {
    render(<Settings />)
    const linear = card(await open('Linear'))
    await userEvent.type(linear.getByLabelText(/linear api key/i), 'lin_api_test')
    await userEvent.click(linear.getByRole('button', { name: /^connect$/i }))
    await waitFor(() => expect(String(postedTo('/api/connectors/linear')![1]!.body)).toContain('lin_api_test'))
  })

  it.each([
    ['GitLab', 'GitLab address (gitlab.com, or your own server)', 'GitLab personal access token', 'site', 'gitlab.com', '/api/connectors/gitlab', /read_api/],
    ['Azure DevOps', 'Organization (the name in dev.azure.com/your-organization)', 'Azure DevOps personal access token', 'organization', 'acme', '/api/connectors/azure', /Code \(Read\)/],
  ])('connects %s by posting its address and a hidden token', async (name, addressLabel, tokenLabel, field, address, endpoint, help) => {
    render(<Settings />)
    const c = card(await open(name))
    expect(c.getByText(help)).toBeInTheDocument()
    const token = c.getByLabelText(tokenLabel)
    expect(token).toHaveAttribute('type', 'password')
    const addressInput = c.getByLabelText(addressLabel)
    expect(addressInput).toHaveAttribute('type', 'text')
    await userEvent.type(addressInput, address)
    await userEvent.type(token, 'secret')
    await userEvent.click(c.getByRole('button', { name: 'Connect' }))
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([u, i]) => u === endpoint && (i as RequestInit)?.method === 'POST')
      expect(JSON.parse(String((call![1] as RequestInit).body))).toEqual({ [field]: address, token: 'secret' })
    })
  })

  it('says a sync failed on the closed card, and why inside it', async () => {
    gitlabError = 'gitlab: the token was refused'
    gitlabStatus = 'connected'
    render(<Settings />)
    const header = await screen.findByRole('button', { name: /^GitLab/ })
    expect(header).toHaveTextContent('sync failed')
    await userEvent.click(header)
    expect(card(header).getByText('Last sync failed: gitlab: the token was refused')).toBeInTheDocument()
  })

  it('adds a calendar by posting its name and the iCal URL in a hidden field', async () => {
    render(<Settings />)
    const cal = card(await open('Calendar'))
    expect(await cal.findByLabelText(/ical url/i)).toHaveAttribute('type', 'password')
    expect(cal.getByLabelText(/calendar name/i)).toHaveAttribute('type', 'text')
    expect(cal.getByText(/Secret address in iCal format/)).toBeInTheDocument()
    // Other providers' steps are there too.
    expect(cal.getByText(/Publish a calendar/)).toBeInTheDocument()
    expect(cal.getByText(/Teams meetings are in your Outlook calendar/)).toBeInTheDocument()
    expect(cal.getByText(/Public Calendar/)).toBeInTheDocument()
    await userEvent.type(cal.getByLabelText(/calendar name/i), 'Work')
    await userEvent.type(cal.getByLabelText(/ical url/i), 'https://cal.example.com/secret.ics')
    await userEvent.click(cal.getByRole('button', { name: /^add calendar$/i }))
    await waitFor(() => expect(JSON.parse(String(postedTo('/api/calendars')![1]!.body))).toEqual({ label: 'Work', icalUrl: 'https://cal.example.com/secret.ics' }))
  })

  it('lists connected calendars by name and status, and adds another behind a button', async () => {
    calendarStatus = 'connected'
    calendars = [
      { ID: 'cal-1', Label: 'Work', Host: 'calendar.google.com/…', Status: 'ok' },
      { ID: 'cal-2', Label: 'Personal', Host: 'calendar.google.com/…', Status: 'error', Error: 'unexpected status 404' },
    ]
    render(<Settings />)
    await open('Calendar')
    await waitFor(() => expect(screen.getByText('Work')).toBeInTheDocument())
    expect(screen.getByText('Personal')).toBeInTheDocument()
    expect(screen.getByText('synced')).toBeInTheDocument()
    expect(screen.getByText(/sync failed: unexpected status 404/)).toBeInTheDocument()
    expect(screen.queryByLabelText(/ical url/i)).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /add another calendar/i }))
    expect(screen.getByLabelText(/ical url/i)).toHaveValue('')
  })

  it('removes one calendar only after a second click', async () => {
    calendarStatus = 'connected'
    calendars = [
      { ID: 'cal-1', Label: 'Work', Host: 'calendar.google.com/…', Status: 'ok' },
      { ID: 'cal-2', Label: 'Personal', Host: 'calendar.google.com/…', Status: 'ok' },
    ]
    render(<Settings />)
    await open('Calendar')
    await waitFor(() => expect(screen.getByText('Personal')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /remove personal/i }))
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === 'DELETE')).toBe(false)
    await userEvent.click(screen.getByRole('button', { name: /yes, remove/i }))
    await waitFor(() => expect(fetchMock.mock.calls.find((c) => c[1]?.method === 'DELETE')?.[0]).toBe('/api/calendars/cal-2'))
  })

  it('a connected Jira can be changed, with its site and email filled in', async () => {
    jiraStatus = 'connected'
    render(<Settings />)
    const jira = card(await open('Jira'))
    expect(jira.queryByLabelText(/jira site/i)).toBeNull()
    await userEvent.click(jira.getByRole('button', { name: /^change$/i }))
    expect(jira.getByLabelText(/jira site/i)).toHaveValue('https://example.atlassian.net')
    expect(jira.getByLabelText(/email/i)).toHaveValue('me@example.com')
    expect(jira.getByLabelText(/jira api token/i)).toHaveValue('')
  })

  it('shows why a connect failed', async () => {
    const base = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) =>
      init?.method === 'POST' ? { ok: false, text: async () => 'Jira refused the email and token\n' } : base(url),
    )
    render(<Settings />)
    const jira = card(await open('Jira'))
    await userEvent.type(jira.getByLabelText(/jira site/i), 'example.atlassian.net')
    await userEvent.type(jira.getByLabelText(/email/i), 'me@example.com')
    await userEvent.type(jira.getByLabelText(/jira api token/i), 't')
    await userEvent.click(jira.getByRole('button', { name: /^connect$/i }))
    await waitFor(() => expect(jira.getByText('Jira refused the email and token')).toBeInTheDocument())
  })

  it('disconnects only after a second click', async () => {
    jiraStatus = 'connected'
    render(<Settings />)
    const jira = card(await open('Jira'))
    await userEvent.click(jira.getByRole('button', { name: /^disconnect$/i }))
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === 'DELETE')).toBe(false)
    await userEvent.click(jira.getByRole('button', { name: /yes, disconnect/i }))
    await waitFor(() => expect(fetchMock.mock.calls.find((c) => c[1]?.method === 'DELETE')?.[0]).toBe('/api/connectors/jira'))
  })

  it('shows who GitHub is connected as, and no dead Configure button', async () => {
    render(<Settings />)
    const gh = card(await open('GitHub'))
    expect(gh.getByText('octocat')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^configure$/i })).toBeNull()
    expect(gh.getByRole('button', { name: /check again/i })).toBeEnabled()
  })

  it('tells how to set up gh when it is missing, and checks again on click', async () => {
    githubStatus = 'gh not installed'
    githubAccount = undefined
    render(<Settings />)
    const gh = card(await open('GitHub'))
    expect(gh.getByRole('link', { name: /github cli/i })).toHaveAttribute('href', 'https://cli.github.com')
    githubStatus = 'connected'
    githubAccount = 'octocat'
    await userEvent.click(gh.getByRole('button', { name: /check again/i }))
    expect(postedTo('/api/connectors/github/check')).toBeTruthy()
    await waitFor(() => expect(gh.getByText('octocat')).toBeInTheDocument())
  })

  it('tells a logged-out user to run gh auth login', async () => {
    githubStatus = 'not logged in'
    githubAccount = undefined
    render(<Settings />)
    expect(card(await open('GitHub')).getByText('gh auth login')).toBeInTheDocument()
  })

  it("shows a calendar by Google's own name when it has none of yours, never by its address", async () => {
    calendarStatus = 'connected'
    calendars = [{ ID: 'cal-1', Label: '', Name: 'Team calendar', Host: 'calendar.google.com/…', Status: 'ok' }]
    render(<Settings />)
    await open('Calendar')
    await waitFor(() => expect(screen.getByText('Team calendar')).toBeInTheDocument())
    expect(screen.getByRole('listitem', { name: 'Calendar Team calendar' })).toBeInTheDocument()
  })

  it('renames a calendar', async () => {
    calendarStatus = 'connected'
    calendars = [{ ID: 'cal-1', Label: '', Name: 'Team calendar', Host: 'calendar.google.com/…', Status: 'ok' }]
    render(<Settings />)
    await open('Calendar')
    await waitFor(() => expect(screen.getByText('Team calendar')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /rename team calendar/i }))
    const input = screen.getByLabelText(/new calendar name/i)
    expect(input).toHaveValue('Team calendar')
    await userEvent.clear(input)
    await userEvent.type(input, 'Work{Enter}')
    await waitFor(() => {
      const put = fetchMock.mock.calls.find((c) => c[1]?.method === 'PUT')
      expect(put?.[0]).toBe('/api/calendars/cal-1/label')
      expect(JSON.parse(String(put![1]!.body))).toEqual({ label: 'Work' })
    })
  })
})

describe('Settings About and Updates', () => {
  const release = {
    Current: 'v1.0.0',
    Latest: 'v1.2.0',
    Available: true,
    Supported: true,
    Enabled: true,
    URL: 'https://example.invalid/releases/v1.2.0',
    PublishedAt: '2026-10-20T10:00:00Z',
    CheckedAt: '2026-10-31T09:00:00Z',
    Notes: 'Fixes',
    Error: '',
    Install: 'homebrew',
    Hint: 'brew upgrade pulsetool',
  }

  it('About and Updates are separate cards, each with its own anchor', async () => {
    render(<Settings />)
    const about = await screen.findByRole('region', { name: 'About' })
    const updates = screen.getByRole('region', { name: 'Updates' })
    expect(about).toHaveAttribute('id', 'about')
    expect(updates).toHaveAttribute('id', 'updates')
    expect(screen.queryByText(/about & updates/i)).not.toBeInTheDocument()
  })

  it('About shows the version, the license, where data lives and a link to Help', async () => {
    render(<Settings />)
    const about = await screen.findByRole('region', { name: 'About' })
    await waitFor(() => expect(about).toHaveTextContent('DevTopo dev'))
    expect(about).toHaveTextContent(/MIT/)
    expect(about).toHaveTextContent(/open source/i)
    expect(about).toHaveTextContent('~/.devtopo')
    expect(within(about).getByRole('link', { name: /help/i })).toHaveAttribute('href', '#help')
    expect(about).not.toHaveTextContent(/check for updates/i)
  })

  it('in a build that cannot check, Updates says checks run in release builds', async () => {
    render(<Settings />)
    const updates = await screen.findByRole('region', { name: 'Updates' })
    await waitFor(() => expect(updates).toHaveTextContent(/update checks run in release builds/i))
    expect(screen.queryByRole('button', { name: /check for updates/i })).not.toBeInTheDocument()
  })

  it('scrolls to the card the link names, also when the hash changes', async () => {
    const seen: string[] = []
    const scroll = vi.fn(function (this: HTMLElement) {
      seen.push(this.id)
    })
    const before = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scroll
    window.location.hash = '#settings?section=updates'
    render(<Settings />)
    await waitFor(() => expect(seen).toContain('updates'))
    expect(seen).not.toContain('about')
    window.location.hash = '#settings?section=about'
    window.dispatchEvent(new HashChangeEvent('hashchange'))
    await waitFor(() => expect(seen).toContain('about'))
    window.location.hash = ''
    Element.prototype.scrollIntoView = before
  })

  it('shows a newer release with its notes link and the upgrade command', async () => {
    updateView = release
    render(<Settings />)
    const updates = await screen.findByRole('region', { name: 'Updates' })
    await waitFor(() => expect(updates).toHaveTextContent('v1.2.0'))
    expect(screen.getByRole('region', { name: 'About' })).toHaveTextContent('v1.0.0')
    const notes = screen.getByRole('link', { name: /release notes/i })
    expect(notes).toHaveAttribute('href', release.URL)
    expect(notes).toHaveAttribute('target', '_blank')
    expect(notes).toHaveAttribute('rel', 'noreferrer')
    expect(screen.getByText('brew upgrade pulsetool').tagName).toBe('CODE')
  })

  it('says when it is up to date, and a direct install points at the release page', async () => {
    updateView = { ...release, Latest: 'v1.0.0', Available: false, Install: 'direct', Hint: 'download from the release page' }
    render(<Settings />)
    const updates = await screen.findByRole('region', { name: 'Updates' })
    await waitFor(() => expect(updates).toHaveTextContent(/latest release/i))
    expect(screen.queryByText('brew upgrade pulsetool')).not.toBeInTheDocument()
  })

  it('checks now, and turns the automatic check off', async () => {
    updateView = { ...release, Available: false, Latest: '' }
    render(<Settings />)
    const button = await screen.findByRole('button', { name: /^check for updates$/i })
    updateView = release
    await userEvent.click(button)
    await waitFor(() => expect(postedTo('/api/update/check')).toBeTruthy())
    await waitFor(() => expect(screen.getByRole('region', { name: 'Updates' })).toHaveTextContent('v1.2.0'))

    const box = screen.getByRole('checkbox', { name: /check for updates automatically/i })
    expect(box).toBeChecked()
    await userEvent.click(box)
    await waitFor(() => {
      const put = fetchMock.mock.calls.find((c) => c[0] === '/api/update/enabled')
      expect(JSON.parse(String(put![1]!.body))).toEqual({ enabled: false })
    })
    await waitFor(() => expect(box).not.toBeChecked())
    expect(screen.getByText(/sends nothing about you/i)).toBeInTheDocument()
  })

  it('shows a failed check quietly', async () => {
    updateView = { ...release, Available: false, Latest: '', Error: 'could not reach GitHub' }
    render(<Settings />)
    expect(await screen.findByText(/could not reach GitHub/)).toBeInTheDocument()
  })
})

describe('the Calendar card', () => {
  it('is called Calendar, with an icon, and says iCal only once open', async () => {
    render(<Settings />)
    const header = await screen.findByRole('button', { name: /^Calendar\b/ })
    expect(header.querySelector('svg')).not.toBeNull()
    expect(header).toHaveTextContent(/^Calendar/)
    expect(screen.queryByText(/iCal/)).toBeNull()
    expect(screen.queryByText(/maker/i)).toBeNull()
    await userEvent.click(header)
    const c = within(header.closest('li')!)
    expect(c.getByText(/Calendars from Google, Outlook and Microsoft Teams, Apple or any other/)).toBeInTheDocument()
    expect(c.getByText(/private iCal address/, { selector: 'div' })).toBeInTheDocument()
  })
})
