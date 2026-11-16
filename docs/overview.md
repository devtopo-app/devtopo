# DevTopo - product overview

Working name: DevTopo. A local-first companion for developers who run several
coding agents and projects at once and have lost the big picture.

This folder is the running design record. It is a brainstorm in progress, not a
plan to build yet. Each decision is tagged:

- **[Decided]** - agreed by the product owner.
- **[Proposed]** - a suggestion on the table, not yet confirmed.
- **[Open]** - a question still to answer.
- **[Out, for now]** - considered and deliberately left out of the current scope.

## What it is

A buddy that helps a developer feel in control and less under pressure across a
day spent in multiple agents (Claude Code, Codex, Cursor) and multiple projects.
It gathers what is happening, reflects it back in plain language, and helps you
plan a realistic day and see the work you actually did - including the work that
never shows up as a number.

## Who it is for

Developers running multiple agents and projects in parallel, who are starting to
miss the overview: what got done, where each thing stands, what is waiting on
them, and whether they are doing enough.

## North-star principles

- **[Decided] A buddy, not a control center.** This tool exists to help you feel
  better and more in control. It is not trying to run your whole work day. When a
  feature starts turning it into mission control, that is a signal it has drifted.
- **[Decided] Pull, not push.** It never forces anything and never nags. You open
  it when you want it. No scheduled rituals firing at you.
- **[Decided] Read-first.** It reads your tools to understand your day. It does not
  edit your tickets or calendar. Any future write access is a separate, explicit,
  opt-in step.
- **[Proposed] Pressure down, never up.** No comparison to colleagues, no output
  quotas. Plan to the time you actually have, celebrate contribution including the
  invisible kind (reviews, unblocking others, investigations).
- **[Decided] Local-first and private.** Raw agent sessions never leave the
  machine. Summarization runs through the user's own installed CLIs. Only derived,
  inspectable summaries ever sync, and only if the user turns sync on.

## The documents

- `connections.md` - which external tools we connect to and why.
- `planning-session.md` - the planning experience (the first feature in focus).
- `today-surface.md` - the glanceable surface that reflects a plan back while you work.
- `contribution-and-time.md` - summary hierarchy, contribution numbers, and how time is measured.
- `capture-layer.md` - the always-on local engine that produces everything the views read.
- `data-model.md` - the entities stored locally.
- `v1-scope.md` - v1 scope, build order, and tech stack.
- `design-system.md` - themes, palettes (light: white/gray/turquoise; dark: Dracula), and the app shell.

Drafted with Claude as a thinking aid; treat it as notes, not a spec.
