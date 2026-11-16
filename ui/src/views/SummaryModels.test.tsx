import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SummaryModels } from './SummaryModels'
import type { SummaryModelsView } from '../api'

const WARNING = 'Summaries run in the background on every active session, so this model uses your limits faster.'

const claude = {
  ID: 'claude',
  Name: 'Claude',
  Available: true,
  Cheapest: 'haiku',
  CheapestKnown: true,
  Models: [
    { ID: 'haiku', Name: 'Haiku', Multiple: 0, PaidCredits: false },
    { ID: 'sonnet', Name: 'Sonnet', Multiple: 0, PaidCredits: false },
    { ID: 'fable', Name: 'Fable', Multiple: 0, PaidCredits: true },
  ],
}
const codex = {
  ID: 'codex',
  Name: 'Codex',
  Available: true,
  Cheapest: 'gpt-6-luna',
  CheapestKnown: true,
  Models: [
    { ID: 'gpt-6-luna', Name: 'GPT-6 Luna', Multiple: 0, PaidCredits: false },
    { ID: 'gpt-6.1-sol', Name: 'GPT-6.1 Sol', Multiple: 20, PaidCredits: false },
  ],
}
const noCodex = { ...codex, Available: false, Models: [] }
const noClaude = { ...claude, Available: false, Models: [] }

const claudeRow = {
  Kind: 'claude',
  Label: 'Claude Code sessions',
  Provider: 'claude',
  Model: 'haiku',
  Listed: true,
  EffectiveProvider: 'claude',
  EffectiveModel: 'haiku',
  Fallback: false,
  Error: '',
}
const codexRow = {
  ...claudeRow,
  Kind: 'codex',
  Label: 'Codex sessions',
  Provider: 'codex',
  Model: 'gpt-6-luna',
  EffectiveProvider: 'codex',
  EffectiveModel: 'gpt-6-luna',
}

let view: Partial<SummaryModelsView>
let afterPut: Partial<SummaryModelsView> | undefined
let putStatus: number
let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  view = { Off: false, Providers: [claude, codex], Rows: [claudeRow, codexRow] }
  afterPut = undefined
  putStatus = 200
  fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      if (putStatus !== 200) return { ok: false, status: putStatus, text: async () => 'not available' }
      if (afterPut) view = afterPut
    }
    return { ok: true, json: async () => view }
  })
  vi.stubGlobal('fetch', fetchMock)
})

const providerPicker = (label: string) => screen.findByRole('combobox', { name: `Provider for ${label}` })
const modelPicker = (label: string) => screen.findByRole('combobox', { name: `Model for ${label}` })

function putTo(url: string) {
  return fetchMock.mock.calls.find((c) => c[0] === url && c[1]?.method === 'PUT')
}

describe('SummaryModels', () => {
  it('offers only the available tools', async () => {
    view = { ...view, Providers: [claude, noCodex], Rows: [claudeRow, { ...codexRow, Provider: 'claude', Model: 'haiku' }] }
    render(<SummaryModels />)
    const picker = await providerPicker('Codex sessions')
    expect(within(picker).getAllByRole('option').map((o) => o.textContent)).toEqual(['Claude'])
  })

  it('keeps a saved tool that is not available as a disabled option', async () => {
    view = { ...view, Providers: [noClaude, codex] }
    render(<SummaryModels />)
    const picker = await providerPicker('Claude Code sessions')
    expect(within(picker).getByRole('option', { name: 'Claude (not available)' })).toBeDisabled()
  })

  it('shows a setup hint for a tool that is not available', async () => {
    view = { ...view, Providers: [noClaude, noCodex] }
    render(<SummaryModels />)
    await screen.findByText('npm install -g @openai/codex')
    expect(screen.getByText('codex login')).toBeInTheDocument()
    expect(screen.getByText('claude')).toBeInTheDocument()
    expect(screen.getByText('/login')).toBeInTheDocument()
  })

  it('shows only setup hints with no tool available', async () => {
    view = { ...view, Providers: [noClaude, noCodex] }
    render(<SummaryModels />)
    await screen.findByText('codex login')
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeInTheDocument()
  })

  it('saves a Codex row that uses Claude', async () => {
    render(<SummaryModels />)
    await userEvent.selectOptions(await providerPicker('Codex sessions'), 'claude')
    const call = putTo('/api/summary-models/codex')
    expect(call).toBeTruthy()
    expect(JSON.parse(String(call![1].body))).toEqual({ provider: 'claude', model: 'haiku' })
  })

  it('resets to the cheapest model when the provider changes', async () => {
    view = { ...view, Rows: [{ ...claudeRow, Model: 'sonnet' }, codexRow] }
    render(<SummaryModels />)
    await userEvent.selectOptions(await providerPicker('Claude Code sessions'), 'codex')
    const call = putTo('/api/summary-models/claude')
    expect(JSON.parse(String(call![1].body))).toEqual({ provider: 'codex', model: 'gpt-6-luna' })
  })

  it('saves a chosen model', async () => {
    render(<SummaryModels />)
    await userEvent.selectOptions(await modelPicker('Claude Code sessions'), 'sonnet')
    const call = putTo('/api/summary-models/claude')
    expect(JSON.parse(String(call![1].body))).toEqual({ provider: 'claude', model: 'sonnet' })
  })

  it('warns above the cheapest model only', async () => {
    view = { ...view, Rows: [{ ...claudeRow, Model: 'sonnet' }, codexRow] }
    render(<SummaryModels />)
    await screen.findByText(WARNING)
    expect(screen.getAllByText(new RegExp(WARNING.replace(/\./g, '\\.')))).toHaveLength(1)
  })

  it('shows no warning on the cheapest models', async () => {
    render(<SummaryModels />)
    await providerPicker('Claude Code sessions')
    expect(screen.queryByText(/uses your limits faster/)).not.toBeInTheDocument()
  })

  it('warns on a reset model that is not known to be the cheapest', async () => {
    const soleModel = { ...codex, Cheapest: 'gpt-6.1-sol', CheapestKnown: false }
    view = { ...view, Providers: [claude, soleModel], Rows: [claudeRow, { ...codexRow, Model: 'gpt-6.1-sol' }] }
    render(<SummaryModels />)
    await screen.findByText(`${WARNING} About 20x the usage of GPT-6 Luna.`)
  })

  it('names the multiple for Codex models', async () => {
    view = { ...view, Rows: [claudeRow, { ...codexRow, Model: 'gpt-6.1-sol' }] }
    render(<SummaryModels />)
    await screen.findByText(`${WARNING} About 20x the usage of GPT-6 Luna.`)
  })

  it('mentions paid credits for fable', async () => {
    view = { ...view, Rows: [{ ...claudeRow, Model: 'fable' }, codexRow] }
    render(<SummaryModels />)
    await screen.findByText(`${WARNING} It may bill to paid usage credits on some plans.`)
  })

  it('marks a saved model that is no longer listed', async () => {
    view = { ...view, Rows: [claudeRow, { ...codexRow, Model: 'gpt-5.5', Listed: false }] }
    render(<SummaryModels />)
    const picker = await modelPicker('Codex sessions')
    expect(picker).toHaveValue('gpt-5.5')
    expect(within(picker).getByRole('option', { name: 'gpt-5.5 (no longer listed)' })).toBeInTheDocument()
  })

  it('says when a row has fallen back', async () => {
    view = {
      ...view,
      Providers: [noClaude, codex],
      Rows: [{ ...claudeRow, Model: 'sonnet', EffectiveProvider: 'codex', EffectiveModel: 'gpt-6-luna', Fallback: true }, codexRow],
    }
    render(<SummaryModels />)
    await screen.findByText('Claude is not available, so these sessions use Codex (gpt-6-luna).')
  })

  it('shows the last error on its row', async () => {
    view = { ...view, Rows: [claudeRow, { ...codexRow, Error: 'codex: logged out' }] }
    render(<SummaryModels />)
    await screen.findByText(/codex: logged out/)
  })

  it('shows a failed save', async () => {
    putStatus = 400
    render(<SummaryModels />)
    await userEvent.selectOptions(await modelPicker('Claude Code sessions'), 'sonnet')
    await screen.findByText('not available')
  })

  it('Check again posts to the check endpoint', async () => {
    render(<SummaryModels />)
    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }))
    await waitFor(() =>
      expect(fetchMock.mock.calls.some((c) => c[0] === '/api/summary-models/check' && c[1]?.method === 'POST')).toBe(true),
    )
  })

  it('says summaries are off with --no-narrative', async () => {
    view = { Off: true, Providers: [], Rows: [] }
    render(<SummaryModels />)
    await screen.findByText(/--no-narrative/)
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })

  it('renders nothing for an empty response', async () => {
    view = {}
    const { container } = render(<SummaryModels />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r))
    expect(container).toBeEmptyDOMElement()
  })
})
