// Types mirror the Go planner API (PascalCase, no json tags on the structs).

export interface PlanRow {
  ID: string
  PlanID: string
  Name: string
  Order: number
}

export interface PlanItem {
  ID: string
  PlanID: string
  RowID: string
  RefSource: string
  RefID: string
  Text: string
  Start: string | null // null = anytime this day
  IntendedHours: number
  RefState: string // the referenced ticket's status now, "" when unknown
  RefStage: string // its stage, e.g. "completed" once finished after planning
  RefRepo: string // its key, e.g. "ENG-1"
  RefURL: string // the ticket in its tracker
  RefProject: string
  RefLabels: string[]
  DoneAt: string | null // when an own activity or review was marked done; a ticket's done is its RefStage
}

export interface Meeting {
  Title: string
  Start: string
  End: string
  Calendar?: string // the label of the calendar it came from, when it has one
}

export interface Plan {
  ID: string
  Name: string
  WindowStart: string
  WindowEnd: string
  CreatedAt: string
  Status: string
  Rows: PlanRow[] | null
  Items: PlanItem[] | null
  MakerHours: number
  MeetingHours: number
  Meetings: Meeting[] | null // the whole day, for the calendar track
}

export interface Plannable {
  RefSource: string
  RefID: string
  Text: string
  Repo: string
  URL: string // the ticket or PR in its tracker
  Kind: string // "review_request" | "own_pr" | "task"
  Hours: number
  State: string // the tracker's status name, e.g. "In Review"; an own PR's review state
  Stage: string // "started" | "unstarted" | "triage" | "backlog" | ""
  Project: string
  Labels: string[]
  // A review's lines, known when Sized, and whether the user hid it.
  Additions?: number
  Deletions?: number
  Sized?: boolean
  Hidden?: boolean
  // An own PR's latest word, and whether the user marked it done.
  Reviewer?: string
  ReviewedAt?: string
  Done?: boolean
}

export interface ItemBody {
  rowId: string
  refSource?: string
  refId?: string
  text: string
  start: string | null
  intendedHours: number
}

async function okJSON<T>(r: Response): Promise<T> {
  if (!r.ok) throw new Error(`${r.url}: ${r.status}`)
  return r.json()
}

function send(method: string, url: string, body?: unknown): Promise<Response> {
  return fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function fetchPlans(): Promise<Plan[]> {
  return okJSON(await fetch('/api/plans'))
}

export async function fetchPlannable(): Promise<Plannable[]> {
  return okJSON(await fetch('/api/plannable'))
}

// refreshConnectors asks the daemon to sync every configured connector now, so
// opening the Planner shows fresh issues and capacity without waiting for the
// slow background loop. It resolves when the sync is done; callers refetch after.
export async function refreshConnectors(): Promise<void> {
  await fetch('/api/refresh', { method: 'POST' })
}

export async function createPlan(name: string, windowStart: string, windowEnd: string): Promise<Plan> {
  return okJSON(await send('POST', '/api/plans', { name, windowStart, windowEnd }))
}

export async function updatePlan(id: string, name: string, windowStart: string, windowEnd: string): Promise<void> {
  await send('PATCH', `/api/plans/${id}`, { name, windowStart, windowEnd })
}

export async function deletePlan(id: string): Promise<void> {
  await send('DELETE', `/api/plans/${id}`)
}

export async function addRow(planID: string, name: string): Promise<PlanRow> {
  return okJSON(await send('POST', `/api/plans/${planID}/rows`, { name }))
}

export async function renameRow(id: string, name: string): Promise<void> {
  await send('PATCH', `/api/rows/${id}`, { name })
}

export async function deleteRow(id: string): Promise<void> {
  await send('DELETE', `/api/rows/${id}`)
}

export async function addItem(planID: string, item: ItemBody): Promise<PlanItem> {
  return okJSON(await send('POST', `/api/plans/${planID}/items`, item))
}

export async function updateItem(id: string, item: ItemBody): Promise<void> {
  await send('PATCH', `/api/items/${id}`, item)
}

export async function setReviewHidden(url: string, hidden: boolean): Promise<void> {
  await send('POST', '/api/reviews/hidden', { URL: url, Hidden: hidden })
}

export async function deleteItem(id: string): Promise<void> {
  const r = await send('DELETE', `/api/items/${id}`)
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`)
}

// postDone sends a done toggle; a refusal throws the server's short message.
async function postDone(url: string, body: unknown): Promise<void> {
  const r = await send('POST', url, body)
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`)
}

// setOwnPRDone marks one of the user's PRs done in the app, or not done.
export const setOwnPRDone = (url: string, done: boolean) => postDone('/api/own-prs/done', { URL: url, Done: done })

// markItemDone marks an own activity or review item done, or not done.
export const markItemDone = (id: string, done: boolean) => postDone(`/api/items/${id}/done`, { done })

// markTicketDone moves a ticket to done, or back in progress, in its tracker.
export const markTicketDone = (source: string, id: string, done: boolean) => postDone('/api/tickets/done', { source, id, done })
