# DevTopo - data model

The entities the capture layer stores locally (SQLite) and the views read. Entity
sketch, not a schema - field lists are indicative. All derived; raw transcripts are
read in place, never stored here.

## Core entities

- **project** - a repo, the unit the UI groups by.
  - repo identity (git remote or root), display name, branches seen.
  - Derived by resolving each session's directory to its repo, so worktrees and
    parallel sessions fold in. [Proposed]
- **session** - one agent run (a Claude session, a worktree, a Codex run).
  - id (transcript uuid), project_id, agent (claude / codex), cwd, branch,
    started_at, last_activity_at, status (running / waiting / idle / stuck),
    is_worktree.
  - cursor (byte offset or last-seen uuid) for incremental reads.
  - ai_active_seconds (incl. executions / tests).
  - rolling_summary (narrative text + structured block, schema_version).
- **fact** - the atomic unit of activity, kept so counts can show hover detail.
  - session_id, type (prompt / decision / tool_call / commit / pr / review /
    comment), timestamp, duration_ms, ticket_key, sha, short text summary.
  - Powers both contribution counts and the "hover a number, see what it was."
- **summary** - a stored summary at a level.
  - level (session / project / overall), subject_id, as_of, narrative text,
    structured json, schema_version.
- **external_item** - something pulled from a connected tool.
  - source (github / linear / jira / calendar), kind (pr / review_request / ticket /
    event), external_id, title, status, link, assigned_to_me, review_requested_of_me,
    window (for calendar), short summary.
- **plan** - an intention for a window.
  - id, name, window_start, window_end, created_at, status (active / past).
- **plan_row** - one user-named row of a plan's timeline. A new plan starts with
  "Work plan" and "Private activities". [Decided]
  - id, plan_id, name, order.
- **plan_item** - one block in a plan.
  - plan_id, row_id, ref (external_item id, or free-form text), start,
    intended_hours. No start means "anytime this day". An item with no ref is the
    user's own activity (lunch, an errand); the row it sits on says whether it is
    work or life. [Decided]
- **contribution_rollup** - precomputed counts for fast display.
  - scope (session / project / day), counts by fact type, split did-directly vs
    directed, pointers back to facts for hover.
- **sync_state** - cursors, last-poll times, schema versions.

## Relationships (shape)

- project 1-* session 1-* fact
- project 1-* external_item (PRs, tickets tied to the repo)
- plan 1-* plan_row 1-* plan_item -> references an external_item (or free-form)
- summary attaches to a session, project, or "overall"
- reconciler links: fact(commit/pr) <-> external_item(pr), ticket_key <-> plan_item

## Notes / decisions

- **[Decided] Store derived only; read raw in place.** Keeps the privacy promise and
  lets us re-derive if the summary schema changes while raw is on disk.
- **[Proposed] Keep facts, not just counts.** The "10 calls, hover for detail"
  feature needs the underlying items, and facts also let us re-aggregate without
  re-summarizing.
- **[Proposed] AI time stored per session**, aggregated to project/day on read (or
  cached in contribution_rollup).

## Open questions

- **Granularity of `fact`**: store every tool call, or only ones that matter for
  contribution (edits, commits, PRs, decisions)? Volume vs detail.
- **Retention**: how long to keep facts and old summaries locally before pruning or
  archiving into history for the retrospective view.
- **Plan item ref**: hard foreign key to external_item, or a loose reference that
  survives a ticket being closed / removed upstream.
