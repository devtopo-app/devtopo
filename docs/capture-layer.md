# DevTopo - the capture layer

The always-on local engine that turns raw agent sessions and external tool data
into the summaries, time, and contribution numbers the views read. Everything
flows one way: raw -> extract -> summarize -> store -> serve. Nothing sensitive
leaves the machine.

## The pipeline

1. **Watcher** - knows when there is new activity.
   - Watches agent transcript files for growth:
     - Claude: `~/.claude/projects/<slug>/*.jsonl` (includes worktrees).
     - Codex: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`.
   - **[Decided] File-watch + idle detection is the backbone**, not tool hooks. It is
     tool-agnostic and survives sessions that run for days or never hand back control.
     "Session ended" is inferred from a file going idle, since Codex has no clean
     session-end event. Hooks are an optional later refinement.
   - External **pollers** on an interval for things with no file to watch: GitHub,
     calendar, Linear / Jira.

2. **Extractor (deterministic, no AI).** Parses new transcript lines since a per-file
   byte cursor and pulls structured facts: prompts, tool calls and their types,
   commits (SHAs), PRs, branch, repo, ticket keys, and record timestamps. From the
   timestamps it computes AI working time (idle-capped gaps, AI-side only - see
   `contribution-and-time.md`; the transcript has no reliable per-turn duration
   field) and the contribution counts. Runs every tick, cheap, no LLM.

3. **Summarizer (AI, via the user's own CLI).** When enough new activity has
   accrued, produces narrative + structured summaries:
   - **[Decided] Delta + rolling summary.** Summarize only what is new since the last
     checkpoint and fold it into a running per-session summary. Never re-read the
     whole session, so cost stays flat over multi-day sessions.
   - Map-reduce up the hierarchy: session delta -> session summary -> project summary
     (across sessions + GitHub) -> overall "state of now".
   - **[Decided] Runs through `claude -p` / `codex exec`** on the user's own
     subscription, on-device. BYO API key is the fallback (Cursor-only users, or to
     keep summaries off the coding-rate-limit budget).

4. **Reconciler.** Dedupes and links: agent-opened PR vs GitHub PR (match on repo +
   branch + PR number, SHA, ticket key), ties tickets to branches to sessions, and
   labels work agent-assisted vs by-hand. Also **[Proposed] resolves repo identity**
   so worktrees and parallel sessions roll up into one project.

5. **Store (local SQLite).** Holds only derived data (see `data-model.md`). Raw
   transcripts are read in place, never copied in.

6. **Server (localhost).** Serves the views over local HTTP + WebSocket (live
   updates). The one seam between daemon and UI.

## Cadence

- **[Decided] Checkpoint on activity, with a 30-minute ceiling, and skip idle
  sessions.** A busy session is captured promptly; a quiet one costs nothing; a
  days-long session still gets regular updates. Deterministic extraction runs every
  file-change; the LLM summary runs only when there is new narrative worth writing.
- External pollers on their own intervals (GitHub more often, calendar / tickets
  less).

## Time and contribution (computed here)

- **[Decided] AI working time** = sum of the agent's active spans, **including the
  code it executed and tests it ran**, per session, rolled up to project and overall.
- **[Decided] Human / attention time is not computed.** Too much human work is
  invisible; a number there would only mislead.
- **Contribution counts** come from the extractor's facts, split into "did directly"
  vs "directed" (see `contribution-and-time.md`), with the underlying facts kept so
  the views can show hover detail.

## Privacy and trust

- **[Decided] Local-first.** Raw sessions never leave the machine. The daemon reads
  them in place and stores only derived summaries and numbers locally.
- Inference runs on the user's own CLI, so even the LLM step stays on their machine /
  their subscription.
- **[Proposed] The daemon is the open-source trust core** - the one component that
  touches raw data, so it is auditable. Any later cloud sync ships only derived,
  inspectable summaries, and only if the user turns it on.

## Open questions

- **Summary schema versioning**: the structured summary shape will change; how do we
  version it and migrate old rows (or just re-derive from raw while it is still on
  disk)?
- **Poller intervals and auth**: how often to poll GitHub / calendar / Linear, and
  where their read tokens live (local only, per connections.md).
- **Hooks as a refinement**: if/when we add Claude/Codex hooks on top of file-watch,
  for faster "new activity" and cleaner session-end signals.
