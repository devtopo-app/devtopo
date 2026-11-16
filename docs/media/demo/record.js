// Record a README GIF's frames from the demo DevTopo, in Edge driven by
// Playwright (npm install playwright-core next to this file, or anywhere on
// NODE_PATH). Serve the demo first (see seed.py), then:
//
//   DEMO_DB=demo.sqlite node record.js <scene> <out-dir> [url] [day]
//
// Scenes: hero, today, sessions, planner, history, ask. It writes
// frame-0000.png... and frames.json (each frame's time, in ms), which
// make-gif.py turns into the GIF. A drawn cursor follows the mouse, since a
// screenshot has none; the Claude and Codex usage in the status bar is made up,
// so no real account shows; each session opens in an app, as in real use; and
// Ask about today answers from a prepared reply, as the demo has no model.
const { chromium } = require('playwright-core')
const { execFileSync } = require('child_process')
const fs = require('fs')
const path = require('path')

const [scene, out, url = 'http://127.0.0.1:8799', day = new Date().toISOString().slice(0, 10)] = process.argv.slice(2)
const db = process.env.DEMO_DB // the demo database, for scenes that change it

const cursor = `
  addEventListener('DOMContentLoaded', () => {
    const c = document.createElement('div')
    c.innerHTML = '<svg width="22" height="28" viewBox="0 0 22 28"><path d="M2 2v21l5.5-5.2 3.6 8.2 3.3-1.4-3.5-8.1H18z" fill="#141c1b" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>'
    Object.assign(c.style, { position: 'fixed', left: '-40px', top: '-40px', zIndex: 2147483647, pointerEvents: 'none' })
    document.body.appendChild(c)
    const st = document.createElement('style')
    st.textContent = '* { user-select: none !important; }'
    document.head.appendChild(st)
    addEventListener('mousemove', (e) => { c.style.left = e.clientX - 2 + 'px'; c.style.top = e.clientY - 2 + 'px' }, true)
  })`

// sql runs a statement on the demo database.
function sql(statement) {
  execFileSync('python', ['-X', 'utf8', '-c', `import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute(sys.argv[2]); c.commit()`, db, statement])
}

// finish ends a running session's turn now, in the demo's time.
async function finish(page, id) {
  const now = await page.evaluate(() => new Date().toISOString())
  sql(`UPDATE sessions SET tail='ai_ended', last_event_at='${now}', last_activity_at='${now}' WHERE id='${id}'`)
  sql(`DELETE FROM subagents WHERE session_id='${id}'`)
}

// The mouse, and moves that take frames as they go.
let mx = 900, my = 600
async function glide(page, shot, x, y, ms = 700) {
  const steps = Math.max(1, Math.round(ms / 70))
  const x0 = mx, y0 = my
  for (let i = 1; i <= steps; i++) {
    const t = i / steps, e = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2
    mx = x0 + (x - x0) * e
    my = y0 + (y - y0) * e
    await page.mouse.move(mx, my)
    await shot(0)
  }
}
async function to(page, shot, locator, ms) {
  const b = await locator.boundingBox()
  await glide(page, shot, b.x + Math.min(b.width / 2, 60), b.y + b.height / 2, ms)
}
async function click(page, shot, locator, ms) {
  await to(page, shot, locator, ms)
  await page.mouse.down()
  await page.mouse.up()
  await shot(0)
}
async function drag(page, shot, locator, x, y, ms = 1400) {
  await to(page, shot, locator, 700)
  await page.mouse.down()
  await shot(150)
  await glide(page, shot, x, y, ms)
  await shot(250)
  await page.mouse.up()
  await shot(0)
}
async function scroll(page, shot, dy, ms) {
  const steps = Math.max(1, Math.round(ms / 90))
  for (let i = 0; i < steps; i++) {
    await page.mouse.wheel(0, dy / steps)
    await shot(0)
  }
}
async function type(page, shot, text) {
  for (const ch of text) {
    await page.keyboard.type(ch)
    await shot(0)
  }
}

const scenes = {
  // The Today page: a session finishes and moves to Waiting on you, then down to the plan.
  hero: { path: '/#/today', async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1800)
    await glide(page, shot, 400, 250, 900)
    await shot(1200)
    await finish(page, 'demo-checkout')
    await shot(6500)
    await glide(page, shot, 640, 560)
    await scroll(page, shot, 430, 1400)
    await shot(2600)
  } },

  // Today in dark mode: timers run, the Codex session hands back, then the plan and your PRs.
  today: { path: '/#/today', scheme: 'dark', async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1500)
    await to(page, shot, page.getByText('fix/rate-limit-headers').first(), 900)
    await shot(1000)
    await finish(page, 'demo-ratelimit')
    await shot(6000)
    await glide(page, shot, 700, 600)
    await scroll(page, shot, 520, 1600)
    await shot(1200)
    // Mark a ticket done from its block on the calendar.
    await click(page, shot, page.locator('.blk', { hasText: 'Offline drafts' }).first(), 900)
    await shot(900)
    await click(page, shot, page.getByRole('button', { name: 'Mark done' }), 700)
    await shot(3500)
  } },

  // Sessions: open two cards to read what each did today, step by step (a click
  // on a card's body opens it; one on its name would rename it).
  sessions: { path: '/#/today', async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1200)
    await click(page, shot, page.getByText('github.com/acme/mobile').first(), 900)
    await shot(2600)
    await click(page, shot, page.getByText('github.com/acme/webapp').first(), 900)
    await shot(2400)
    // The fuller summary of the day, on More detail.
    const card = page.locator('div.cursor-pointer', { hasText: 'github.com/acme/webapp' }).first()
    await click(page, shot, card.getByRole('button', { name: 'More detail' }), 800)
    await shot(1600)
    // Down to the end of it, so the whole of More detail is read.
    await scroll(page, shot, 640, 1700)
    await shot(3600)
  } },

  // Planner in dark mode: drag a ticket and a review onto the work plan.
  planner: { path: '/#/planner', scheme: 'dark', height: 1000, async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1200)
    // The Work plan row's lane, and where an hour starts on the ruler (7:00 to
    // 20:00 over 1280 px: 13:00 at 805, 64 px an hour).
    const y = 398
    const hourX = async (h) => 805 + (h - 13) * 64
    await drag(page, shot, page.getByText('Paginate the orders endpoint').first(), (await hourX(13)) + 14, y)
    await shot(1500)
    await drag(page, shot, page.getByText('Show the order history on the account page').first(), (await hourX(15)) - 20, y)
    await shot(2200)
  } },

  // History: step back through past days.
  history: { path: '/#/history', async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1400)
    for (const d of ['13', '12', '11', '10']) {
      await click(page, shot, page.getByRole('button', { name: new RegExp(`^${d}\\b`) }).first(), 600)
      await shot(1500)
    }
    await glide(page, shot, 760, 600)
    await scroll(page, shot, 360, 1000)
    await shot(1800)
  } },

  // Ask about today in dark mode: a question, and the answer with its sources.
  ask: { path: '/#/today', scheme: 'dark', async run(page, shot) {
    await page.mouse.move(mx, my)
    await shot(1000)
    await click(page, shot, page.locator('header button[aria-label="Ask about today"]'), 900)
    const box = page.getByRole('dialog').locator('input').last()
    await box.waitFor()
    await page.waitForTimeout(900) // the window settles before it is filmed
    await shot(500)
    await to(page, shot, box, 600)
    await box.focus()
    await type(page, shot, 'Where did we fix the double-counted order total?')
    await shot(400)
    await page.keyboard.press('Enter')
    await shot(5200)
  } },
}

const detailReply = [
  'What was done',
  '- Declined card payments gave up after one try. A retry with backoff now runs for declines the provider marks as retryable, behind a flag so it can be switched off.',
  '- A retry that succeeded added the order total a second time. The total is now counted once, when the payment finally goes through.',
  '',
  'What you decided',
  '- You chose to retry only declines the provider marks as retryable, not every decline, so a blocked card is never tried again.',
  '- You asked for the retry behind a flag, to turn it off without a release.',
  '',
  'What I decided',
  '- I set three tries, 2, 4 and 8 seconds apart: a first guess worth checking with the payments team.',
  '- I left refunds out of the retry, as they go through a different flow.',
  '',
  'Open items',
  '- The end-to-end tests against the staging provider are still running; waits on them.',
  '- The retry delays need a look with the payments team; waits on you.',
].join('\n')

const askReply = [
  { Type: 'checking', Total: 5 },
  { Type: 'checked', Done: 5, Total: 5 },
  { Type: 'done', Result: {
    Answer: 'In feat/checkout-retry (acme/webapp), at 13:30. When a declined payment was retried and then went through, the order total was added a second time; the fix counts the total once, when the payment finally succeeds, and a new test covers a retry that succeeds.',
    Sources: [{ SessionID: 'demo-checkout', Label: 'feat/checkout-retry', Agent: 'claude', At: '13:30', Why: 'Fixed the order total being counted twice when a retry succeeds.' }],
    FromSummaries: true, Total: 5, Read: 0, Shortened: null, Failed: null,
  } },
]

;(async () => {
  const sc = scenes[scene]
  fs.mkdirSync(out, { recursive: true })
  const browser = await chromium.launch({ channel: 'msedge' })
  // Twice the pixels, so text stays sharp on high-resolution screens (SCALE=1 for smaller files).
  const ctx = await browser.newContext({ viewport: { width: 1280, height: sc.height ?? 800 }, deviceScaleFactor: Number(process.env.SCALE ?? 2), colorScheme: sc.scheme ?? 'light' })
  const page = await ctx.newPage()
  await page.addInitScript(cursor)
  await page.route('**/api/usage', (r) => r.fulfill({ json: {
    Windows: [{ Label: 'week', Percent: 34, ResetsAt: `${day}T23:00:00Z` }, { Label: 'Fable', Percent: 12, ResetsAt: `${day}T23:00:00Z` }],
    Status: '', Codex: [{ Label: '30d', Percent: 21, ResetsAt: `${day}T23:00:00Z` }],
  } }))
  // Nothing really runs in the demo, so every card would say Resume: give each
  // session the app it runs in, as in real use, so it says Open in VS Code etc.
  const hosts = { 'demo-checkout': 'vscode', 'demo-ratelimit': 'codex-app', 'demo-offline': 'orca', 'demo-docs': 'vscode', 'demo-infra': 'windows-terminal' }
  await page.route('**/api/today', async (r) => {
    const res = await r.fetch()
    const view = await res.json()
    for (const lane of [view.Running, view.Waiting, view.Idle]) {
      for (const c of lane?.Cards ?? []) {
        if (hosts[c.ID]) Object.assign(c, { Host: hosts[c.ID], Live: true, CanFocus: true })
      }
    }
    await r.fulfill({ response: res, json: view })
  })
  // Opening the Planner would sync the connected tools through the real
  // accounts on this machine (gh, Linear...); the demo answers it itself.
  await page.route('**/api/refresh', (r) => r.fulfill({ json: {} }))
  // More detail on a session: the fuller summary a model would write.
  await page.route('**/api/sessions/*/detail', async (r) => {
    await new Promise((res) => setTimeout(res, 900))
    await r.fulfill({ json: { AsOf: new Date().toISOString(), Text: detailReply } })
  })
  // Mark done on a ticket: move it to done here, as Linear would, and record the finish.
  await page.route('**/api/tickets/done', async (r) => {
    const { id, done } = r.request().postDataJSON()
    const now = new Date(Date.parse(`${day}T14:20:00`) + 60000).toISOString()
    sql(done
      ? `UPDATE external_items SET state='Done', stage='completed', completed_at='${now}' WHERE external_id='${id}'`
      : `UPDATE external_items SET state='In Progress', stage='started', completed_at='' WHERE external_id='${id}'`)
    await r.fulfill({ json: {} })
  })
  await page.route('**/api/ask/models', (r) => r.fulfill({ json: { Off: false, Models: [{ ID: 'claude:haiku', Tool: 'claude', Label: 'Claude Haiku', Default: true }] } }))
  await page.route('**/api/ask', async (r) => {
    await new Promise((res) => setTimeout(res, 600))
    await r.fulfill({ contentType: 'application/x-ndjson', body: askReply.map((e) => JSON.stringify(e)).join('\n') + '\n' })
  })
  await page.clock.install({ time: new Date(`${day}T14:20:00`) })
  await page.goto(url + '/?k=demo-key') // sign in, as DevTopo's own link does (seed.py sets the key)
  await page.goto(url + sc.path)
  await page.waitForTimeout(2500)

  const frames = []
  const start = Date.now()
  // shot takes frames for ms (at least one), each stamped with its time.
  async function shot(ms) {
    const until = Date.now() + ms
    do {
      const file = `frame-${String(frames.length).padStart(4, '0')}.png`
      await page.screenshot({ path: path.join(out, file) })
      frames.push({ file, t: Date.now() - start })
    } while (Date.now() < until)
  }
  await sc.run(page, shot)
  fs.writeFileSync(path.join(out, 'frames.json'), JSON.stringify(frames))
  await browser.close()
  console.log(`${scene}: ${frames.length} frames in ${((Date.now() - start) / 1000).toFixed(1)}s`)
})()
