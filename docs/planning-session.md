# DevTopo - the planning session

The first feature in focus: a lightweight, on-demand way to plan a stretch of
time. It helps you feel in control of what is ahead without becoming a task
manager or a schedule you have to obey.

## Shape of the experience

1. You open the planner tab whenever you feel like planning. Nothing prompts you.
2. You pick the period you want to plan for, with a flexible selector.
3. You get a read-only overview of that period: your calendar for the window, plus
   the Linear / Jira tasks assigned to you.
4. Your buddy helps you make sense of it and shape a realistic plan. (Depth of this
   step is still being designed - see Open questions.)

## Decisions

- **[Decided] On-demand, never forced.** The planner is something you open when you
  want it. No scheduled morning or evening ritual that fires at you, no nagging.
- **[Decided] Plan any window, any time.** You choose the horizon freely. Examples:
  the next 2 hours, the rest of today until a certain time, tomorrow, a specific day
  three days out, an arbitrary range. You can plan whatever period you want,
  whenever you want.
- **[Decided] A "fancy slider" period selector.** The horizon is chosen through a
  nice, tactile slider-style control rather than a plain date form. (Exact
  interaction still to be designed - see Open questions.)
- **[Decided] Read-only inputs.** For the chosen window the planner shows:
  - your calendar for that period, and
  - the Linear / Jira tasks delegated to you.
  We do not edit these. No ticket editing, no calendar writes.
- **[Decided] No Slack, and not a control center.** The planner is your buddy
  helping you feel better and more in control, not the hub that runs your whole
  work day.

## In scope for the overview [Decided unless noted]

- Calendar events in the chosen window.
- Linear / Jira tasks assigned to you.
- **[Proposed]** How much real maker time the window actually holds, once meetings
  are subtracted. This is what keeps a plan realistic rather than wishful.
- **[Proposed]** In-progress agent sessions relevant to the window (to resume).
- **[Decided]** PRs you were asked to review. The planner shows not just your assigned
  tasks but also the review requests waiting on you, so "what's on me" is complete.

## How the buddy helps you plan [Proposed]

Not a blank form and not a long chat - a draft you nudge. Carried over from the
anti-pressure principles:

- **Plan to the time you have, not to the backlog.** Size the plan to the window's
  real maker time.
- **One main thing.** Suggest a single primary focus for the window, not a list of
  ten.
- **A "not today / not now" view.** Make it easy to set things aside, so you are not
  carrying them in your head.
- **Carry-over without guilt.** Unfinished work is just "still here," never a
  shaming pile.

All of the above are [Proposed] and up for discussion when we design this step.

## Plans and plan items

- **[Decided] Plans are shown graphically on the planner page.** You can see "this
  is what you planned for today, this is tomorrow," at a glance.
- **[Decided] Multiple active plans.** People can hold several plans at once (today,
  tomorrow, a future day, a short block).
- **[Decided] A plan is made of items you choose.** You pick tickets from your
  assigned Linear / Jira list and add them to a plan.
- **[Decided] A plan is a timeline.** A day view with your calendar on top and
  user-named rows below ("Work plan", "Private activities", any you add). Each item
  is a block with a start time and a length in hours (ticket story points, 1 pt =
  1h; reviews half an hour), or sits in "anytime this day" without a time. This
  replaced the earlier order and priority per item.
- **[Decided] Free-form items allowed.** Alongside tickets and review-requests, you
  can add a plain line like "review the deploy" or "investigate the flaky test". Not
  all real work is a ticket.
- **[Decided] A "life tasks" category.** Personal, non-work things you want to get to
  during the day (book a doctor's appointment, an errand), tracked alongside the work
  agenda but clearly its own category. Purely user-entered, no connector, no time or
  contribution tracking - just a gentle place so life does not fall off the day.
  Reinforces the work-life-quality purpose. (Its own "Private activities" row on the
  timeline, see data-model.md.)
- **[Decided] Capacity context, shown plainly.** The window's real maker time
  (calendar minus meetings) next to your allocated total, as plain facts - no warning
  colour, overfilling on purpose allowed. Useful control, never a nag.
- **[Decided] Plans persist locally and auto-archive into history.** You return to a
  plan during its window; when the window passes it moves to a "past" state and is
  kept (nothing deleted), feeding the later retrospective / contribution-over-time
  view.

## AI in the planner (v1)

- **[Decided] v1 is all user call.** No "let AI plan this for me," and no AI coaching
  (telling you to slow down, plan less, or do X first).
- **[Proposed] The planner page needs no new AI.** Everything it does is
  deterministic: fetch calendar + tickets, capacity math, ordering, priority, hours.
  The only AI-derived content it shows is the short per-ticket / per-session summary
  already produced by the capture layer, reused read-only so you can pick with
  context. The product's AI understands and summarizes; the human plans.
- **[Out, for now] AI assists** (suggested order, overcommit nudges, auto-planning).
  A possible opt-in, off-by-default, never-prescriptive v2 feature. Not in v1.

## Out of scope / superseded

- **[Out, for now] Editing tickets or calendar from the planner.** Read-only.
- **[Out, for now] Slack input or output** (mentions, posting a standup).
- **[Superseded] Fixed morning/evening scheduled ritual.** Replaced by on-demand,
  open-whenever planning.
- **[Superseded] Auto-actions** (create tickets, block focus time, post standup).
  The planner advises; it does not act on your tools.

## Open questions

- **The slider itself**: what are its stops or anchors (2h, 4h, today, tomorrow,
  pick-a-day, custom range), and is it one continuous control or a few presets plus
  a free range? How does it show "until 4pm today" vs "all of Thursday"?
- **Depth of the buddy's help**: does it draft a plan, or just lay out the overview
  and let you think? Where on that line for v1?
- **Where planning intelligence runs**: on the user's own CLI, or a small own-API
  call on the (read-only, derived) calendar + task data? Ties to the inference model
  in the overview.
- **Does a plan persist?** Is a plan a saved thing you return to during the window,
  or a throwaway you generate each time you open the tab?
