# DevTopo - summaries, contribution, and time

How we summarize activity, show the user's contribution with numbers, and measure
time in a world where agents do much of the work.

## Summary hierarchy

- **[Decided] Three levels of summary**: overall activity, per project, and per
  individual session (a Claude session, a worktree, a separate Codex run). You read
  these instead of opening pages across many sessions.
  - Session summary = the rolling per-session summary from the capture layer.
  - Project summary = synthesis across all that project's sessions + its GitHub
    activity.
  - Overall summary = synthesis across projects.
  - Drill down: overall -> project -> session -> raw session only if wanted.
- **[Proposed] A project groups sessions by repo identity, not by folder.** Worktrees
  (different paths) and parallel sessions (different ids) for the same repo roll up
  together, resolved via git remote / repo root. Otherwise worktrees look like
  separate projects.

## Contribution, with numbers and hover detail

- **[Decided] Highlight the user's contribution, with numbers**, e.g. "in this
  project you made 10 directions...", and **[Decided] hover shows a text summary** of
  what those were about.
- **[Decided] Show two labelled layers**, so it is honest in an agent workflow:
  - **What you did directly**: prompts / directions, decisions and approvals, manual
    commits, reviews given, comments. Your hands on the work.
  - **What the agents did (under your steering)**: represented by **AI running time
    plus outcomes** (agent-made commits, PRs opened, files changed). **Raw tool-call
    counts are not surfaced** - "how much the agent did" is told through time and
    outcomes, not a tally of edits/commands/searches.
  - Do not inflate and do not erase: agent effort shows as time + outcomes, never as
    a tool-call count dressed up as your own output.
- **[Proposed] Hover detail reuses existing summaries.** Each count is backed by its
  underlying items; hovering shows the capture layer's summary of them. No new AI
  concept.
- **[Proposed] Metric honesty guardrail.** Numbers about your own day are good (the
  "see your contribution" payoff). Numbers that rank you against others, or
  lines-of-code as a headline, are out - pressure and gameable. The rule is "no
  scoreboard," not "no numbers."
- **[Decided] Lines of code omitted entirely.** Misleading and gameable; outcomes
  (commits, PRs) and AI time carry the story.
- **[Decided] Tool-call counts not surfaced.** Agent effort is shown as AI time +
  outcomes.

## Time - three independent axes

Time is not one thing. Measure three axes from different signals.

- **[Decided] AI working time is the time we measure.** How long Claude / Codex were
  actually working. "Claude worked 2h14m on ENG-142."
- **[Decided] Measured from timestamps, not durations.** We checked real Claude
  transcripts (2026-10-21): there is no reliable per-turn `durationMs` field, so AI
  time is the idle-capped sum of the gaps between records, counting only gaps whose
  later event is agent-produced (an assistant turn, or a tool result coming back). A
  gap longer than the idle cap (5 minutes to start) is treated as the session paused,
  not the agent working. This counts generation, tool orchestration, and the
  executions the agent triggered, while excluding the human's own turns and long idle
  stretches. It is an estimate, and more honest than any human-time guess.
- **[Decided] Count everything the agent did, including code execution and tests it
  ran.** The gap until a tool result returns covers the executions the agent
  triggered (builds, test runs, scripts), so they are in the number. This resolves
  the earlier open question about what counts.
- **[Decided] Human / attention time is dropped for now.** Too much real human work
  leaves no trace we can measure (manual debugging, testing the webapp by hand,
  clicking through a deploy on GitHub). Any "your time" number would always
  underestimate and feel bad - the opposite of this tool's job. Better to show
  nothing there than something demoralizing.
- **[Proposed] Wall-clock** - session-open duration. Kept minor; never presented as
  "your time."

### The parallelism reframe [Proposed]

Because agents run in parallel and unattended, AI-hours stack up fast - and that is
the leverage worth *seeing*. Lean into it:

> "Claude and Codex put in 6 hours of work across your projects today."

- **Sum AI time across parallel sessions** - it is genuinely that much work done, and
  the big number is the point.
- Show wall-clock alongside so the stacked AI number is never mistaken for elapsed
  time.

### Honesty caveats [Proposed]

- AI time is approximate (depends how an "active" span is bounded), but far more
  honest than any human-time guess, which is why human time is dropped.
- AI time is descriptive, not a productivity score or a comparison.

## Open questions

None open. See the decisions above.
