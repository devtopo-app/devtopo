// Types mirror the Go JSON API. /api/today marshals the view structs with no
// json tags, so its keys are PascalCase; /api/status is camelCase.

export interface CardData {
  ID: string
  Name?: string // what the user called the session; '' or absent until renamed
  Agent?: string // 'claude' or 'codex'
  Title?: string // the name the agent gave the session (Codex's thread name); '' when none
  RunningAgents?: string[] | null // names of the sub-agents working now
  Project: string
  Branch: string
  Status: string
  AIHours: number
  WallHours: number
  Prompts: number
  Decisions?: number // questions the human answered today
  LastActivity: string
  FinishedAt?: string // when the agent stopped, once it isn't running
  BackgroundTasks?: number // background tasks the agent left running
  WorkingSince?: string // when a running session's current task started
  TodayPoints: string[] | null // what the session did today, at most 10; null before its first summary
  TodayAsOf: string // the activity TodayPoints cover; zero time when there are none
  Signals: Record<string, number> | null
  Host: string // the app the session runs in, or last ran in; '' when unknown
  Live: boolean // a claude process is running the session now
  CanFocus: boolean // DevTopo can bring the session's app to the front here; false on Linux
}

export interface LaneData {
  Title: string
  Status: string
  Cards: CardData[] | null
}

export interface ReviewItem {
  Title: string
  Repo: string
  URL: string
  UpdatedAt: string
}

export interface FinishedTicket {
  Source: string
  ID: string // the external id POST /api/tickets/done takes
  Key: string
  Title: string
  URL: string
  Project: string
  CompletedAt: string
}

export interface ReviewDone {
  Title: string
  Repo: string
  URL: string
  SubmittedAt: string
}

// StartedTicket went in progress today and is in no plan for today.
export interface StartedTicket {
  Source: string
  ID: string // the external id addItem's refId and the done endpoint take
  Key: string
  Title: string
  URL: string
  Project: string
  State: string
  Stage: string
  StartedAt: string
  Hours: number // the estimate, 1 when unestimated
}

// YourPR is one of the user's open PRs that has a review.
export interface YourPR {
  Source: string // the code host: "github" | "gitlab" | "azure"
  RefID: string // the external id addItem's refId takes
  Title: string
  Repo: string
  URL: string
  ReviewState: string // "approved" | "changes_requested" | "commented"
  Reviewer: string
  ReviewedAt: string
  Hours: number
  Done: boolean
}

export interface TodayView {
  Running: LaneData
  Waiting: LaneData
  Idle: LaneData
  DayAIHours: number
  DaySpanHours: number
  DaySessions: number
  Reviews: ReviewItem[] | null
  ContribPRs: number
  FinishedToday: FinishedTicket[] // newest first
  PromptsToday: number
  ReviewsDone: ReviewDone[] // newest first
  StartedToday: StartedTicket[] // newest first
  YourPRs?: YourPR[] | null // newest review first
  StateOfNow: string
  AsOf: string
}

export interface StatusView {
  daemon: string
  activeSessions: number
  waiting: number
  dayAIHours: number
  asOf: string
}

export async function fetchToday(): Promise<TodayView> {
  const r = await fetch('/api/today')
  if (!r.ok) throw new Error(`today: ${r.status}`)
  return r.json()
}

// renameSession stores the name the user gave a session ('' clears it); a
// refusal throws the server's short message.
export async function renameSession(id: string, name: string): Promise<void> {
  const r = await fetch(`/api/sessions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`)
}

// Sticker is a counter or a done-for-today mark floating over the Today page;
// X, Y and W are pixels of the window, Count is its count on the asked day
// (a done sticker counts 1 when done).
export interface Sticker {
  ID: string
  Kind: 'counter' | 'done'
  Pic: string
  Name: string
  X: number
  Y: number
  W: number
  Count: number
}

const json = { 'Content-Type': 'application/json' }
async function ok(r: Response): Promise<Response> {
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`)
  return r
}

export async function fetchStickers(day: string): Promise<Sticker[]> {
  return (await ok(await fetch(`/api/stickers?d=${day}`))).json()
}

// putSticker adds a sticker or saves its picture, name, place and size.
export async function putSticker(s: Sticker): Promise<void> {
  await ok(
    await fetch(`/api/stickers/${encodeURIComponent(s.ID)}`, {
      method: 'PUT',
      headers: json,
      body: JSON.stringify({ kind: s.Kind, pic: s.Pic, name: s.Name, x: s.X, y: s.Y, w: s.W }),
    }),
  )
}

export async function setStickerCount(id: string, day: string, count: number): Promise<void> {
  await ok(
    await fetch(`/api/stickers/${encodeURIComponent(id)}/count`, { method: 'PUT', headers: json, body: JSON.stringify({ day, count }) }),
  )
}

export async function deleteSticker(id: string): Promise<void> {
  await ok(await fetch(`/api/stickers/${encodeURIComponent(id)}`, { method: 'DELETE' }))
}

// focusSession brings the app running the session to the front, and resume
// reopens a closed one (action 'resume'); a refusal throws the server's short
// message.
export async function focusSession(id: string, action: 'focus' | 'resume' = 'focus'): Promise<void> {
  const r = await fetch(`/api/sessions/${encodeURIComponent(id)}/${action}`, { method: 'POST' })
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`)
}

export async function fetchStatus(): Promise<StatusView> {
  const r = await fetch('/api/status')
  if (!r.ok) throw new Error(`status: ${r.status}`)
  return r.json()
}

// UsageView is the Claude subscription limits: one window per limit (5h, week,
// a model's week), or a Status saying why there are none, and Codex's limits
// ([] when there is no Codex data). ResetsAt is the Go zero time when the API
// gave no reset.
export interface UsageView {
  Windows: { Label: string; Percent: number; ResetsAt: string }[]
  Status: string
  Codex?: { Label: string; Percent: number; ResetsAt: string }[]
}

export async function fetchUsage(): Promise<UsageView> {
  const r = await fetch('/api/usage')
  if (!r.ok) throw new Error(`usage: ${r.status}`)
  return r.json()
}

export interface SessionDetail {
  Text: string
  AsOf: string
}

// fetchDetail asks for the longer summary of what a session did today. It can
// take a while (a claude call); a failure throws the server's error text.
export async function fetchDetail(id: string): Promise<SessionDetail> {
  const r = await fetch(`/api/sessions/${encodeURIComponent(id)}/detail`, { method: 'POST' })
  if (!r.ok) throw new Error((await r.text()).trim() || `detail: ${r.status}`)
  return r.json()
}

export interface SessionPoints {
  Points: string[] | null
  AsOf: string
}

// refreshPoints brings a session's points for today up to date now. It can take
// a while (a claude call); a failure throws the server's error text.
export async function refreshPoints(id: string): Promise<SessionPoints> {
  const r = await fetch(`/api/sessions/${encodeURIComponent(id)}/points`, { method: 'POST' })
  if (!r.ok) throw new Error((await r.text()).trim() || `points: ${r.status}`)
  return r.json()
}

// HistoryMonthDay is one day of a month with AI time or a finished ticket.
export interface HistoryMonthDay {
  Date: string
  AIHours: number
  Tickets: number
}

export interface HistorySession {
  ID: string
  Name: string // the user's name for it, else its agent's title; '' for neither
  Agent: string
  Project: string
  Branch: string
  AIHours: number
  Prompts: number
  Points: string[]
  Summarizing: boolean // points not made yet: fetchHistoryPoints makes them
}

export interface Finish {
  Source: string
  Kind: string
  Key: string
  Title: string
  URL: string
  At: string
}

// HistoryDay is one local day. First and Last are the Go zero time on a day
// with no activity; MeetingHours is null when the calendar keeps nothing for it.
export interface HistoryDay {
  Date: string
  First: string
  Last: string
  AIHours: number
  Prompts: number
  Hours: number[] // 24 AI-hour figures, one per local hour
  Sessions: HistorySession[]
  Tickets: Finish[] // oldest first
  Reviews: number
  OtherTasks: number
  MeetingHours: number | null
}

export async function fetchHistoryMonth(month: string): Promise<HistoryMonthDay[]> {
  const r = await fetch(`/api/history/month?m=${month}`)
  if (!r.ok) throw new Error(`history: ${r.status}`)
  return r.json()
}

export async function fetchHistoryDay(date: string): Promise<HistoryDay> {
  const r = await fetch(`/api/history/day?d=${date}`)
  if (!r.ok) throw new Error(`history: ${r.status}`)
  return r.json()
}

// fetchHistoryPoints makes a session's points for a past day (a claude call
// the first time); a failure throws the server's error text.
export async function fetchHistoryPoints(date: string, id: string, signal?: AbortSignal): Promise<string[]> {
  const r = await fetch(`/api/history/points?d=${date}&session=${encodeURIComponent(id)}`, { method: 'POST', signal })
  if (!r.ok) throw new Error((await r.text()).trim() || `points: ${r.status}`)
  return (await r.json()).Points
}

// UpdateView is the release check (GET /api/update). Supported is false in a
// build that cannot check (from source, or no repository named); Install is
// how DevTopo was installed (homebrew, scoop, direct) and Hint how to upgrade.
export interface UpdateView {
  Current: string
  Latest: string
  Available: boolean
  URL: string
  Notes: string
  PublishedAt: string | null
  CheckedAt: string | null
  Error: string
  Enabled: boolean
  Supported: boolean
  Install: string
  Hint: string
}

export async function fetchUpdate(): Promise<UpdateView> {
  return (await ok(await fetch('/api/update'))).json()
}

// checkUpdate asks GitHub for the latest release now.
export async function checkUpdate(): Promise<UpdateView> {
  return (await ok(await fetch('/api/update/check', { method: 'POST' }))).json()
}

export async function setUpdateEnabled(enabled: boolean): Promise<UpdateView> {
  return (
    await ok(await fetch('/api/update/enabled', { method: 'PUT', headers: json, body: JSON.stringify({ enabled }) }))
  ).json()
}

// SummaryModelsView is which tool and model summarize each kind of session
// (GET /api/summary-models). Off is true with --no-narrative.
export interface SummaryModel {
  ID: string
  Name: string
  Multiple: number
  PaidCredits: boolean
}

export interface SummaryProvider {
  ID: string
  Name: string
  Available: boolean
  Cheapest: string
  CheapestKnown: boolean
  Models: SummaryModel[]
}

export interface SummaryRow {
  Kind: string
  Label: string
  Provider: string
  Model: string
  Listed: boolean
  EffectiveProvider: string
  EffectiveModel: string
  Fallback: boolean
  Error: string
}

export interface SummaryModelsView {
  Off: boolean
  Providers: SummaryProvider[]
  Rows: SummaryRow[]
}

export async function fetchSummaryModels(): Promise<SummaryModelsView> {
  return (await ok(await fetch('/api/summary-models'))).json()
}

// checkSummaryModels checks the tools again, after one was installed or logged in.
export async function checkSummaryModels(): Promise<SummaryModelsView> {
  return (await ok(await fetch('/api/summary-models/check', { method: 'POST' }))).json()
}

export async function saveSummaryModel(kind: string, provider: string, model: string): Promise<SummaryModelsView> {
  return (
    await ok(
      await fetch(`/api/summary-models/${encodeURIComponent(kind)}`, {
        method: 'PUT',
        headers: json,
        body: JSON.stringify({ provider, model }),
      }),
    )
  ).json()
}
