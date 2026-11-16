# DevTopo guide

DevTopo is a dashboard for developers who work with coding agents (Claude Code
and Codex). It reads your agent sessions on your own machine and shows:

- **Today**: AI hours, prompts and finished work, today's plan on a calendar,
  and every session with a short, timed summary of what it did.
- **Planner**: your tickets and meetings on a timeline, to plan the day.
- **History**: any past day, with the same numbers and summaries.

Everything runs and stays on your machine. Keys and URLs you enter are stored
in a local database and never shown back. The one thing DevTopo asks on its
own is whether a newer release is out: twice a day it reads the public list of
releases on GitHub, sending nothing about you (see Updates below; you can
turn it off).

This guide is also in the app, under **Help**. For an overview, see the
[README](README.md).

## Install

Download the latest version from the repository's **Releases** page.

**Windows**: download `DevTopo.exe`, put it somewhere it can stay (for
example a `DevTopo` folder in your user folder) and double-click it. Windows
may say "Windows protected your PC", because the app is not signed: click
**More info**, then **Run anyway**.

**macOS**: download `DevTopo-mac.zip`, open it and drag **DevTopo** into
Applications. The app is not signed by Apple, so the first time macOS blocks
it: open System Settings - Privacy & Security, scroll down and click **Open
Anyway** next to DevTopo. If macOS says the app is damaged, run this once in
Terminal: `xattr -dr com.apple.quarantine /Applications/DevTopo.app`.

**Linux**: download `devtopo-linux-amd64.tar.gz` (or `-arm64` for ARM),
unpack it and put `devtopo` somewhere on your PATH, for example
`~/.local/bin`:

```sh
tar -xzf devtopo-linux-amd64.tar.gz
install -m 755 devtopo ~/.local/bin/
devtopo
```

The tray icon needs a desktop with a tray (KDE, XFCE, Cinnamon, Budgie and
most others). On GNOME, install the **AppIndicator and KStatusNotifierItem
Support** extension; Ubuntu has it on already. Without a tray DevTopo still
runs and serves its page; stop it with Ctrl-C.

## Use

DevTopo runs in the background with an icon in the tray (Windows, near the
clock; Linux, in the panel) or the menu bar (macOS). The first start opens it
in your browser.
The icon's menu has:

- **Open DevTopo** - opens the page again after you closed the tab
- **Start at login** - starts DevTopo in the background when you log in
- **Update available: vX.Y.Z** - only when a newer release is out; opens its
  release page
- **Quit** - stops DevTopo

DevTopo is at http://127.0.0.1:8787. When another program already uses that
port, DevTopo takes the next free one up to 8796; the tray's Open DevTopo
always knows which. Starting DevTopo again while it runs just opens the page.

Your data lives in `~/.devtopo/devtopo.sqlite`, and what DevTopo logs in
`~/.devtopo/devtopo.log`. It reads Claude Code sessions from `~/.claude` (or
`$CLAUDE_CONFIG_DIR`) and Codex sessions from `~/.codex` (or `$CODEX_HOME`).

## Build from source

You need Go 1.22 or newer and Node.js 20 or newer.

```sh
cd ui && npm ci && npm run build && cd ..
go build -o devtopo ./cmd/devtopo        # devtopo.exe on Windows
./devtopo          # in the tray
./devtopo app      # in the terminal, until Ctrl-C
```

`devtopo app` takes `--claude-dir`, `--db` and `--addr` to point it elsewhere.
`devtopo --version` (or `devtopo version`) prints the version and exits. A
build from source is version `dev` and does not check for updates.
On macOS the tray icon needs cgo (the Xcode command line tools); without it
DevTopo runs without the icon. On Linux no cgo is needed
(`CGO_ENABLED=0` works).

## Make a release

Releases are made from `main` only. On GitHub, open the repository's
**Releases** page - **Draft a new release** - **Choose a tag**, type a new
version such as `v0.1.0`, keep the target `main`, and click **Publish
release**. About five minutes later `DevTopo.exe`, `DevTopo-mac.zip`, the
two Linux archives and a `SHA256SUMS` file to check them against appear on
that release (see `.github/workflows/release.yml`; its progress is
on the **Actions** tab). A tag on a commit that is not on `main` builds
nothing.

## Updates

DevTopo tells you when a newer release is out; it never downloads or installs
anything by itself.

- **How it checks**: about 30 seconds after it starts and then twice a day,
  DevTopo asks GitHub's public API for the latest published release of the
  repository it was released from (drafts and pre-releases are skipped). The
  request carries no account, token or data of yours - only DevTopo's version
  in the User-Agent header, like any web request.
- **Where it shows**: a small **Update available** link in the status bar,
  an item in the tray menu, a dot on the question mark at the bottom of the
  left bar, and **Settings - Updates**, with the release notes, **Check for
  updates**, and how to update for the way you installed it.
- **Check now**: the question mark's **Check for updates** asks right away and
  says what it found in the menu; so does **Check for updates** in Settings -
  Updates. **Settings - About** shows the version you run.
- **Turn it off**: untick **Check for updates automatically** in Settings -
  Updates. **Check for updates** still works when you ask.
- **How to update**:
  - Homebrew: `brew upgrade <name>` (Settings shows the exact command).
  - Scoop: `scoop update <name>` (likewise).
  - Downloaded from the Releases page: download the new version the same way
    and replace the old one (on macOS, drag the new app over the old one in
    Applications). Quit DevTopo from the tray first; your data in
    `~/.devtopo` stays.

Builds from source (version `dev`) do not check. The release build names the
repository to check (`-X main.updateRepo=owner/name`, set from
`github.repository` in the release workflow).

## Session summaries

DevTopo writes the summary of each session with the coding agent you already
use, under your own subscription. No API key is needed.

- **Claude Code**: install it and log in (`claude`, then `/login`). DevTopo
  runs `claude -p`, by default with the Haiku model.
- **Codex**: DevTopo runs `codex exec`, by default with the gpt-6-luna model.
  Install the command-line tool and log in:
  ```sh
  npm install -g @openai/codex
  codex login
  ```
  The Codex desktop app alone is not enough: it does not install the `codex`
  command.

By default, a Claude session is summarized by Claude and a Codex session by
Codex. With only one installed, it summarizes every session. With neither,
DevTopo still shows the numbers, just no written summaries.

To choose differently, open **Settings > Session summaries**. For Claude Code
sessions and for Codex sessions you can pick the tool and the model, so Claude
can summarize your Codex sessions too. Only tools that are installed and logged
in are offered. If a chosen tool stops being available, its sessions use the
other tool on its cheapest model until it is back.

Summaries run in the background on every active session, so a larger model uses
your subscription limits faster, and Fable may bill to paid usage credits on
some plans. A change applies to new activity, not to summaries already written.

DevTopo checks for these tools when it starts. After you install or log in to
one, press **Check again** in that card; there is no need to restart.

The summary runs leave no session files behind, cannot change
anything on your disk (Claude gets no tools at all; Codex runs read-only, with
its shell, browser, computer use and apps turned off), and do not load your own
agent settings, plugins or MCP servers.

A summary covers the whole day of a session, from its first turn to its last,
in 1 to 15 lines: few on a quiet day, more on a busy one. On a long day every
turn is shortened to fit, rather than the morning being dropped. The lines are
in time order, each starting with the time the step began, for example:

```
09:12 Fixed the Jira sync so tickets read with a scoped token show in the planner.
10:40 Added a Disconnect button in Settings that also removes the synced tickets.
```

Today's summaries refresh after new activity, at most every 15 minutes while a
session runs. To get a newer one sooner, open the session and click the small
refresh icon next to the "as of" time. A past day is summarized the first time
you open it in History.

## Ask about today

**Ask about today**, at the top right of Today, opens a chat about the sessions
you worked on today. Ask "in which session did we fix the login?" or "why did we
pick that library?" and it answers, naming the session each point came from,
with an **Open** link for each. Follow-up questions build on what it already
found. It covers today only, not past days.

It uses the same Claude Code or Codex you set up for summaries, under your own
subscription: no API key, but every question counts toward your usage limits.
The window says so. Pick the model next to the button. The list comes from the
tools that are installed and logged in, and falls back to a short fixed list if
a tool can't say. It starts on Haiku, the cheapest Claude model. Fable may bill
to paid usage credits on some plans. The chat has not answered a question through
a real Codex install yet.

It starts with today's summaries and answers from them when they are enough,
which is often one call. If not, it reads the sessions that look relevant, shows
its progress, and combines what it found. **Stop** ends a run and the tool
processes it started. If it finds nothing, it says "didn't find it in what I
read", not that the thing never happened.

What to know:

- **One model reads everything.** The model you pick reads every session, including
  the ones from the other tool. With Claude selected, your Codex conversations go
  to Anthropic, and the other way round. Change the model only if you are fine
  with that.
- **Claude gets no tools at all. Codex runs read-only, with its shell, browser,
  computer use and apps turned off.** Neither can change anything on your disk or
  run commands, and neither loads your own agent settings or MCP servers.
- **Long sessions may be shortened.** A long session has its longest turns cut to
  fit, and the window marks those sessions. A detail can be missing from what
  was read.
- **Not everything is read.** Only the human and assistant text of the main
  conversation is read. Tool output and sub-agent conversations are not.
- **Chats aren't saved.** A chat lives in the open app and is gone when you
  reload or quit. Nothing from it goes into the DevTopo database, and the
  runs leave no session files behind.

Without Claude Code or Codex installed, or with `--no-narrative`, the window
explains that chat is off.

## Connections

Connect your tools in **Settings**. Each one can be changed or disconnected
later; disconnecting forgets the saved key and removes what was synced.

### GitHub

Install the [GitHub CLI](https://cli.github.com) and log in: `gh auth login`.
DevTopo uses it to read the reviews requested of you and your pull requests;
there is nothing to enter in DevTopo. Settings shows which account it is
connected as; after installing gh or logging in, click **Check again** there.
Synced every 10 minutes.

In the Planner, each PR waiting on you is estimated from the lines it changes,
leaving out lockfiles and built output: 15 minutes up to 50 lines, 30 minutes up
to 200, an hour up to 500, two hours up to 1000, and three hours above that. You
can drag a block's edge to change it. Click a PR to open it on GitHub, or hide it;
**Show hidden** lists what you hid. A hidden PR comes back when you're asked to
review it again, but not for new commits or a request to a team you're on. Once
a PR is in your plan, its menu can also mark it done, as on Today, where a task's
menu can also remove it from the plan. So can the
menu of an activity of your own; what you mark done shows as done in both places.

### GitLab

DevTopo shows the same as for GitHub: merge requests waiting on your review, your
own open ones that have been reviewed, and the reviews you finished. It works with
gitlab.com and your own server, and it only reads.

1. In GitLab: your avatar - **Edit profile** - **Access tokens** - **Add new token**.
   It must be this personal token: a project or group access token acts as a bot,
   not as you, and DevTopo won't accept one.
2. Give it a name (for example `DevTopo`), an expiry, and the scope **read_api**.
3. In DevTopo Settings, under GitLab, fill in the **address** (`gitlab.com`, or
   the address of your server) and the **token**.

Synced every 10 minutes and whenever you open the Planner.

A few things work a little differently from GitHub, because GitLab does:

- You can comment without submitting a review, so any comment from someone else
  counts as **Commented** on your own merge requests.
- You stay a reviewer after you review. A merge request leaves your list once you
  have reviewed it, and comes back if you're asked again.

### Azure DevOps

DevTopo shows the same as for GitHub: pull requests waiting on your vote, your own
active ones that have been reviewed, and the reviews you finished. It works with
Azure DevOps Services and your own server, and it only reads.

1. In Azure DevOps: **User settings** - **Personal access tokens** - **New Token**.
2. Choose your organization, an expiry, and the scope **Code (Read)**.
3. In DevTopo Settings, under Azure DevOps, fill in the **organization** (its
   name, as in `dev.azure.com/your-organization`, or your server's address with its
   collection) and the **token**.

Synced every 10 minutes and whenever you open the Planner.

A few things work a little differently from GitHub, because Azure DevOps does:

- Votes become the badges: *approved* and *approved with suggestions* show as
  **Approved**, *waiting for author* and *rejected* as **Changes requested**.
- There are no submitted reviews, so any comment from someone else counts as
  **Commented**, and only a vote counts as a review you finished.
- The API gives no line counts, so these pull requests start at 30 minutes in the
  Planner. You can drag a block's edge to change it.
- A pull request you hid comes back once you've voted and been asked again; there
  is no record of when you were asked.

### Linear

In Linear: Settings - Security & access - Personal API keys - create a key.
Paste it in DevTopo Settings. DevTopo shows the issues assigned to you.
Synced every 10 minutes and whenever you open the Planner.

### Jira

DevTopo shows the Jira issues **assigned to you**: open ones, and ones
finished in the last 14 days. Mark done moves an issue in Jira.

1. Go to https://id.atlassian.com/manage-profile/security/api-tokens, logged
   in with the account you use for Jira.
2. Click **Create API token with scopes**, give it a name (for example
   `DevTopo`) and an expiry (at most a year).
3. Choose the app **Jira**, then tick these scopes:
   - `read:jira-work` - read your issues
   - `read:jira-user` - read your own profile, used to check the token
   - `write:jira-work` - only if you want Mark done to move issues in Jira;
     leave it out for read-only access
4. Click **Create** and copy the token straight away; it is shown only once.
5. In DevTopo Settings, under Jira, fill in:
   - **Site**: the address you see in the browser, like
     `your-team.atlassian.net`
   - **Email**: the email of the account that created the token
   - **API token**: the token

A classic token without scopes works too, but it can do everything your
account can. If Connect fails, the message says whether the site is not a Jira
site or Jira refused the email and token.

Synced every 10 minutes and whenever you open the Planner.

### Calendar

DevTopo reads any calendar through its private iCal (`.ics`) address. Where to
find it:

- **Google Calendar**: Settings - your calendar - Integrate calendar - **Secret
  address in iCal format**.
- **Outlook and Microsoft Teams** (Teams meetings are in your Outlook calendar):
  Outlook on the web - Settings - Calendar - Shared calendars - **Publish a
  calendar**, then copy the ICS link. Some work accounts don't allow publishing.
- **Apple Calendar (iCloud)**: share the calendar as a **Public Calendar**, then
  copy its link (a `webcal://` link works as it is).

Paste it in DevTopo Settings, under Calendar, with an optional name such
as "Work" or "Personal", and press **Add calendar**. Without a name, DevTopo
uses the calendar's own name, when the feed has one; **Rename** changes it any
time (clear it to go back to the calendar's name). Anyone with this address can
read your calendar; if it leaks, reset or unpublish it where you got it.
DevTopo never shows the address again.

You can connect several calendars: **Add another calendar** for each one. Each is synced on its own every
15 minutes, so one that fails (for example after you reset its address) keeps
its last meetings and shows the error in Settings without holding up the
others. A meeting that is on two of your calendars is counted once. **Remove**
forgets one calendar and only its meetings. Google's own caching can add a
delay.

## The Today page

The page has four parts: the numbers, today's plan, Finished today and the
sessions. Hover one and drag the handle at its left onto another part to
change the order. The order is kept in your browser.

A session card counts today's prompts and decisions. A decision is one question
from Claude that you answered; approving a permission doesn't count, and Codex
sessions don't count decisions yet.

### Stickers

**Add sticker** (next to the day/night switch) puts a hexagon sticker on the
Today page, pinned to the spot where you put it, so it scrolls with the page.
Pick its picture, its name and its kind:

- **Counter** - starts at 0 every day; double-click adds one (for example a
  glass of water drunk).
- **Done for today** - starts undone every day; double-click marks it done
  (for example vitamins taken).

Drag a sticker to move it and drag the dot at its lower right to resize it.
Right-click it to remove one, reset, mark it not done, change its picture,
rename or delete it. Stickers and their daily counts are saved by DevTopo.

## Platforms

Windows, macOS and Linux. On macOS DevTopo reads the Claude usage limits
through the Keychain item Claude Code stores, and opens sessions in Terminal,
iTerm, VS Code, Orca or the Claude and Codex apps.

On Linux, **Resume** opens a closed session in a new terminal window: the one
in `$TERMINAL` if set, else the first found of x-terminal-emulator,
gnome-terminal, konsole, xfce4-terminal, kitty, alacritty, wezterm and xterm.
Bringing a running session's window to the front is not available on Linux
(Wayland does not let one app raise another's window), so a live session has
no Focus button there. Start at login adds DevTopo to
`~/.config/autostart`.

## License

MIT - see [LICENSE](LICENSE).
