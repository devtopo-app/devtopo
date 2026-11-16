# DevTopo - v1 scope and tech stack

What the first version is, in what order it gets built, and what it is built with.

## v1 scope

- **[Decided] Two views**: the planner and the today surface. No third view in v1
  (a retrospective / over-time view is v2; today already shows the day's
  contribution and AI-hours).
- **[Decided] Capture layer is the foundation** both views read from, and the first
  thing to build. The views are empty shells without it.
- **[Proposed] Read-only connectors in v1.** No writing to tickets or calendar, no
  Slack.

## v1 contents

**[Decided] v1 is a full first release: both views, wired to this connector set
(all read-only):**
- Claude session capture (feeds the today surface).
- GitHub - contribution including non-agent work and reviews given, and reviews
  requested of you.
- Linear and Jira - the planner's assigned tasks.
- Google Calendar - the planner's capacity and meetings.

Not the thin Claude-only validation slice considered earlier; the owner chose to
ship both the today surface and the planner against real connectors from the start.

Suggested build order within v1 [Proposed]:
1. Claude capture + today surface (local transcripts, AI-hours, summaries).
2. GitHub read (contribution + reviews-requested).
3. Planner + Linear / Jira + Google Calendar read.

Later / v2: Codex, Cursor, GitLab / Bitbucket, other trackers, Microsoft calendar,
cloud sync, team features, any AI assist in the planner.

## Tech stack [Proposed]

- **Daemon**: **[Decided] Go** - small static binary, easy file-watching, trivial
  cross-compile, gentle to maintain. Runs always-on as a background service
  (launchd / systemd / Windows service) and must outlive the UI.
- **Local store**: SQLite.
- **Inference**: subprocess to the user's `claude -p` / `codex exec`; BYO API key as
  fallback.
- **UI**: React + Vite + Tailwind, served by the daemon over localhost HTTP +
  WebSocket. MVP opens in the browser; then wrap in **Tauri** with a system-tray icon
  for the always-on daemon.
- **Packaging**: signed binary + per-OS installer, auto-update.
- **Licensing**: **[Decided]** the whole project (daemon and UI) is open source
  under the MIT license.

## What "validated" means [Decided]

A ladder - all three rungs, in order:

1. **Accurate** - the summaries, AI-time, and contribution match what actually
   happened. The prerequisite.
2. **Relied on** - you open it daily and trust it enough to stop scrolling raw
   sessions; it leaves you oriented and in control. The real bar.
3. **Calmer** - over time it demonstrably lowers your pressure and adds stability.
   The mission.

## Open questions

None open.
