# DevTopo - connections

Which external tools DevTopo connects to, what each is for, and the access it
needs. All connections start **read-only**; see the note at the end.

## The connections

| Tool | Job it does | Priority | Access | Status |
|---|---|---|---|---|
| Agent sessions (Claude Code, Codex) | In-flight work: what each agent is doing, where it is mid-task, the reasoning | Core | Local file read | [Decided] |
| Cursor | Same, for Cursor users | Later | Local SQLite read | [Proposed] |
| Calendar (any iCal calendar: Google, Outlook / Microsoft Teams, Apple, others) | Your meetings, for planning the day | Core | Read | [Decided] |
| Linear | What work is assigned to you (tickets, cycle) | Core | Read | [Decided] |
| Jira | Same, for Jira teams | Core | Read | [Decided] |
| GitHub | Contribution that happens outside the agents, and what is blocking you | Core | Read | [Decided] |
| GitLab, Azure DevOps | The same as GitHub, for teams that host code there | Core | Read | [Decided] |
| GitLab / Bitbucket | Same, for teams on those hosts | Later | Read | [Proposed] |
| GitHub Issues / Projects, Azure DevOps Boards, Shortcut | Other common issue trackers | Later | Read | [Proposed] |
| Asana, ClickUp, Notion-as-tasks, Trello, Monday | Broader trackers, less dev-centric | Maybe | Read | [Open] |
| Slack / Teams | Mentions, who is waiting on you; posting summaries | - | - | [Out, for now] |

## Connecting a calendar

DevTopo reads any calendar through its private iCal URL, which the guide (GUIDE.md) says
where to find for Google, Outlook / Microsoft Teams and Apple. In Google
Calendar, it is Settings - your calendar - Integrate calendar - **Secret address
in iCal format**. Paste it in DevTopo Settings. DevTopo re-reads it every 15 minutes
and on every planner open; Google's own caching of the feed can add a delay on
top. The URL gives read access to the whole calendar to anyone who has it, so it
is stored only in the local database and never shown back; if it leaks, reset
it in Google Calendar. (Google OAuth was tried and dropped: it needs a Google
Cloud project per user.)

## Why GitHub is core, not optional

The agent sessions only ever see agent-driven work. GitHub is the only source for:

- **Code written outside an agent** - a hand fix in the IDE, a hotfix, a commit
  from a machine without DevTopo, a web edit.
- **Reviews you gave** - approvals, change requests, review comments. Often a large
  part of a senior's day and invisible to your own sessions.
- **Collaboration and unblocking** - issue/PR comments, merges, releases.
- **What is waiting on you** - reviews requested of you, which neither Linear nor
  the calendar knows about.
- **A backstop** for any work the local daemon never saw (other machine, web).

It also serves the feel-good goal directly: reviews and mentoring are the classic
invisible work that leaves people feeling they "did nothing," and GitHub is what
gives that back to them.

### How GitHub and agent sessions fit together [Proposed]

- Agent session = process and reasoning (how it was made, where you are).
- GitHub = outcome and collaboration (what landed, work done with others).
- **Reconcile, do not double-count**: match on repo + branch + PR number, commit
  SHA, and ticket key, so one piece of work shows once. Optionally label it
  "agent-assisted" vs "by hand" - neutrally, both are your contribution.

## Jira [Decided]

Connect in Settings with your Jira Cloud site (`your-team.atlassian.net`), your
Atlassian account email and an API token from
https://id.atlassian.com/manage-profile/security/api-tokens. DevTopo checks
them against `/rest/api/3/myself`, then every 10 minutes reads your open
assigned issues (`assignee = currentUser() AND statusCategory != Done`, up to
200) through `/rest/api/3/search/jql`. They show in the planner as tasks next to
Linear ones. The token is stored only in the local database and never shown back.

## Access model

- **[Decided] Read-only to start.** Everything above powers the product with read
  access alone. For the planning feature specifically, the user was explicit: we do
  not edit their tickets or calendar.
- **[Out, for now] Write access.** Creating tickets, setting statuses, blocking
  focus time, posting standups. Powerful, but each is a separate trust ask and not
  part of the current scope.

## Superseded / dropped

- **Slack as an input and output channel** - earlier floated as a strong
  nice-to-have. [Out, for now], to keep the tool a personal buddy rather than the
  control center of the whole work day.
- **Auto-actions on your behalf** (create tickets, block calendar, post standup) -
  [Out, for now] in favor of a read-only, advisory tool.
