"""Seed a demo DevTopo database for the README's GIFs and screenshots.

Everything in it is made up (acme projects, ENG tickets), so nothing private
is ever recorded. The day is today, at AT (14:20 by default):

    python docs/media/demo/seed.py --exe ./devtopo.exe --db demo.sqlite
    ./devtopo.exe serve --db demo.sqlite --addr 127.0.0.1:8799 --at 2026-11-16T14:20

The --at given to serve must be the same day and time as the seed's.
"""
import argparse
import json
import os
import sqlite3
import subprocess
import tempfile
from datetime import datetime, timedelta, timezone

ap = argparse.ArgumentParser()
ap.add_argument("--exe", required=True, help="the devtopo program, to create the database")
ap.add_argument("--db", required=True)
ap.add_argument("--at", default="14:20", help="the time of day the demo is at, HH:MM")
args = ap.parse_args()

if os.path.exists(args.db):
    os.remove(args.db)
# The program creates the tables; an empty Claude folder leaves them empty.
with tempfile.TemporaryDirectory() as empty:
    subprocess.run([args.exe, "ingest", "--db", args.db, "--claude-dir", empty], check=True, capture_output=True)

h, m = map(int, args.at.split(":"))
now = datetime.now().astimezone().replace(hour=h, minute=m, second=0, microsecond=0)
day = now.strftime("%Y-%m-%d")


def at(hh, mm, ss=0):
    return now.replace(hour=hh, minute=mm, second=ss)


def ago(**kw):
    return now - timedelta(**kw)


def z(t):  # the store's time format
    return t.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")


db = sqlite3.connect(args.db)
q = db.execute

# The demo's access key, which the recorder signs in with (record.js).
q("INSERT OR REPLACE INTO settings(key,value) VALUES('access_key','demo-key')")

# Sessions: two running, two waiting on you, one idle since the morning.
sessions = [
    # id, agent, repo, branch, tail, first, last event, AI hours today, prompts, decisions, summary
    ("demo-checkout", "claude", "acme/webapp", "feat/checkout-retry", "ai_working", at(9, 5), ago(seconds=35), 2.6, 14, 3, [
        "09:05 Mapped how the checkout handles a failed card payment and where it gives up.",
        "09:40 Added a retry with backoff for declined-but-retryable payments, behind a flag.",
        "11:15 Wrote tests for the retry: one decline, three declines, and a timeout.",
        "13:30 Fixed the order total being counted twice when a retry succeeds.",
        "14:02 Running the checkout end-to-end tests against the staging payment provider.",
    ]),
    ("demo-ratelimit", "codex", "acme/api", "fix/rate-limit-headers", "ai_working", at(13, 10), ago(seconds=50), 0.9, 5, 1, [
        "13:10 Found why clients retry too early: the rate limit reset header was in seconds, not a date.",
        "13:45 Sent the reset as an HTTP date and kept the old header for one release.",
        "14:12 Updating the API reference and the changelog.",
    ]),
    ("demo-offline", "claude", "acme/mobile", "feat/offline-sync", "ai_ended", at(10, 20), ago(minutes=6), 1.8, 9, 4, [
        "10:20 Sketched how drafts sync when the phone is back online, with conflicts kept side by side.",
        "11:50 Stored drafts in a local queue and replayed them in order.",
        "14:14 Asked whether a conflict should keep both versions or the newest; waiting for your answer.",
    ]),
    ("demo-docs", "claude", "acme/docs", "docs/api-quickstart", "ai_ended", at(12, 30), at(13, 12), 0.5, 4, 0, [
        "12:30 Rewrote the API quickstart around a single curl request.",
        "13:12 Added the rate limit section and a troubleshooting table.",
    ]),
    ("demo-infra", "claude", "acme/infra", "chore/terraform-1-9", "ai_ended", at(8, 40), at(9, 25), 0.6, 3, 1, [
        "08:40 Upgraded the Terraform providers and fixed two deprecated arguments.",
        "09:25 Plan shows no changes to production; ready for review.",
    ]),
]
task_start = {"demo-checkout": at(14, 2), "demo-ratelimit": at(14, 9)}
for sid, agent, repo, branch, tail, first, last, hours, prompts, decisions, lines in sessions:
    key = "github.com/" + repo
    q("INSERT OR IGNORE INTO projects(key,name) VALUES(?,?)", (key, repo.split("/")[1]))
    q("""INSERT INTO sessions(id,project_key,agent,cwd,branch,started_at,last_activity_at,last_event_at,last_event_is_ai,ai_active_seconds,status,tail,transcript_path)
         VALUES(?,?,?,?,?,?,?,?,1,?,'',?,'')""",
      (sid, key, agent, "/home/dev/" + repo.split("/")[1], branch, z(first), z(last), z(last), hours * 3600, tail))
    # The day row, its AI time spread over the hours worked.
    span = list(range(first.hour, last.hour + 1))
    buckets = [0.0] * 24
    for hr in span:
        buckets[hr] = hours * 3600 / len(span)
    q("INSERT INTO session_days(session_id,day,ai_seconds,first_at,last_at,hours) VALUES(?,?,?,?,?,?)",
      (sid, day, hours * 3600, z(first), z(last), json.dumps(buckets)))
    # Prompts and decisions through the day. A running session's last prompt,
    # below, starts its current task, so its earlier ones end before that.
    until = task_start.get(sid, last)
    for i in range(prompts):
        t = first + (until - first) * (i / max(prompts, 1))
        q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES(?,?,?, '')", (sid, "prompt", z(t)))
    for i in range(decisions):
        t = first + (last - first) * ((i + 0.5) / decisions)
        q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES(?,?,?, '')", (sid, "decision", z(t)))
    for i in range(prompts * 3):
        q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES(?,'assistant_turn',?, '')", (sid, z(first)))
    q("INSERT INTO summaries(level,subject_id,as_of,narrative,structured,schema_version) VALUES('session_day',?,?,'',?,6)",
      (f"{sid}/{day}", z(ago(minutes=2)), json.dumps(lines)))
# The checkout session's current task started at 14:02.
q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES('demo-checkout','prompt',?, '')", (z(at(14, 2)),))
q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES('demo-ratelimit','prompt',?, '')", (z(at(14, 9)),))
q("INSERT INTO subagents(path,session_id,last_event_at,tail) VALUES('demo/agent-1','demo-checkout',?,'ai_working')", (z(ago(seconds=20)),))

# Tickets assigned to you.
tickets = [
    ("ENG-142", "Retry declined card payments at checkout", "In Progress", "started", 3, "Checkout", "payments,backend", at(9, 5), None),
    ("ENG-151", "Offline drafts for the mobile app", "In Progress", "started", 4, "Mobile", "mobile", at(10, 15), None),
    ("ENG-147", "Rate limit headers follow the HTTP spec", "In Review", "started", 1, "API", "api", None, None),
    ("ENG-155", "Show the order history on the account page", "Todo", "unstarted", 2, "Webapp", "frontend", None, None),
    ("ENG-160", "Alert when the payment provider slows down", "Backlog", "backlog", 2, "Platform", "observability", None, None),
    ("ENG-139", "Fix the totals rounding on refunds", "Done", "completed", 1, "Checkout", "payments", None, at(11, 40)),
    ("ENG-144", "Upgrade Terraform to 1.9", "Done", "completed", 1, "Platform", "infra", None, at(9, 30)),
]
for key, title, state, stage, pts, project, labels, started, completed in tickets:
    q("""INSERT INTO external_items(source,external_id,kind,title,state,url,repo,assigned_to_me,updated_at,estimate,stage,project,labels,started_at,completed_at)
         VALUES('linear',?,'task',?,?,?,?,1,?,?,?,?,?,?,?)""",
      ("lin-" + key, title, state, "https://linear.app/acme/issue/" + key, key, z(ago(minutes=30)), pts, stage, project,
       labels.replace(",", "\x1f"), z(started) if started else "", z(completed) if completed else ""))

# Reviews waiting on you, ones you finished, and your own PRs once reviewed.
reviews = [
    ("acme/webapp", 412, "Lazy-load the product images", 180, 40, at(11, 5)),
    ("acme/api", 238, "Paginate the orders endpoint", 64, 12, at(13, 20)),
]
for repo, n, title, add, dele, asked in reviews:
    url = f"https://github.com/{repo}/pull/{n}"
    q("""INSERT INTO external_items(source,external_id,kind,title,state,url,repo,review_requested_of_me,updated_at,additions,deletions,changed_files,requested_at)
         VALUES('github',?,'review_request',?,'open',?,?,1,?,?,?,4,?)""", (url, title, url, repo, z(asked), add, dele, z(asked)))
url = "https://github.com/acme/mobile/pull/97"
q("""INSERT INTO external_items(source,external_id,kind,title,state,url,repo,updated_at) VALUES('github',?,'review_done','Cache the feed for offline reading','open',?,'acme/mobile',?)""",
  ("review:" + url, url, z(at(10, 5))))
for repo, n, title, state, reviewer, when in [
    ("acme/webapp", 405, "Move the cart to the new design tokens", "approved", "maria-k", at(12, 50)),
    ("acme/api", 231, "Add idempotency keys to payments", "changes_requested", "tom-r", at(13, 35)),
]:
    url = f"https://github.com/{repo}/pull/{n}"
    q("""INSERT INTO external_items(source,external_id,kind,title,state,url,repo,updated_at,reviewer,reviewed_at)
         VALUES('github',?,'own_pr',?,?,?,?,?,?,?)""", ("own:" + url, title, state, url, repo, z(when), reviewer, z(when)))

# A calendar with today's meetings.
q("INSERT INTO calendar_feeds(id,label,name,url,created_at,last_sync_at) VALUES('cal-demo','Work','Work','https://example.com/work.ics',?,?)", (z(ago(days=30)), z(ago(minutes=5))))
for uid, title, s, e in [("m1", "Team standup", at(9, 30), at(9, 45)), ("m2", "Design review: checkout", at(15, 30), at(16, 15)), ("m3", "1:1 with Maria", at(17, 0), at(17, 30))]:
    q("INSERT INTO calendar_events(feed_id,uid,title,start_at,end_at) VALUES('cal-demo',?,?,?,?)", (uid, title, z(s), z(e)))

# Today's plan.
pid = "p-demo"
q("INSERT INTO plans(id,window_start,window_end,created_at,status,name) VALUES(?,?,?,?,'active',?)",
  (pid, z(at(9, 0)), z(at(18, 0)), z(at(8, 50)), "Plan for " + now.strftime("%a %-d %b" if os.name != "nt" else "%a %#d %b")))
q("INSERT INTO plan_rows(id,plan_id,name,row_order) VALUES(?,?,'Work plan',0)", (pid + "-work", pid))
q("INSERT INTO plan_rows(id,plan_id,name,row_order) VALUES(?,?,'Private activities',1)", (pid + "-private", pid))
items = [
    ("i1", "linear", "lin-ENG-144", "Upgrade Terraform to 1.9", 1, "work", at(8, 30) + timedelta(minutes=30), None),
    ("i2", "linear", "lin-ENG-142", "Retry declined card payments at checkout", 3, "work", at(10, 0), None),
    ("i3", "", "", "Lunch + walk", 1, "private", at(13, 0), None),
    ("i4", "github", "https://github.com/acme/webapp/pull/412", "Lazy-load the product images", 0.5, "work", at(14, 0), None),
    ("i5", "linear", "lin-ENG-151", "Offline drafts for the mobile app", 1.5, "work", at(16, 15), None),
    ("i6", "", "", "Pick up the kids", 0.5, "private", at(17, 30), None),
    ("i7", "linear", "lin-ENG-155", "Show the order history on the account page", 2, "work", None, None),
]
for iid, src, ref, text, hrs, row, start, done in items:
    q("INSERT INTO plan_items(id,plan_id,ref_source,ref_id,text,intended_hours,row_id,start_at,done_at) VALUES(?,?,?,?,?,?,?,?,?)",
      (iid, pid, src, ref, text, hrs, f"{pid}-{row}", z(start) if start else "", z(done) if done else ""))

# Past days, for History: a few sessions a day over the last two weeks.
past = [
    ("acme/webapp", "feat/saved-carts", "claude", [
        "09:10 Stored carts per account so they follow you to another device.",
        "11:30 Merged a guest cart into the account cart on sign-in.",
        "15:05 Added tests for two devices editing the same cart."]),
    ("acme/api", "feat/webhooks-retry", "codex", [
        "10:00 Retried failed webhooks with backoff, up to a day.",
        "13:40 Recorded each attempt so support can see why one failed."]),
    ("acme/mobile", "fix/login-loop", "claude", [
        "09:30 Found the login loop: an expired refresh token was retried forever.",
        "10:15 Signed the user out cleanly instead, keeping their drafts."]),
    ("acme/infra", "chore/alerts", "claude", [
        "14:00 Moved the error-rate alerts to the new dashboards.",
        "16:20 Silenced the noisy disk alert on the build servers."]),
]
n = 0
for back in range(1, 15):
    d = now - timedelta(days=back)
    if d.weekday() >= 5:
        continue
    dstr = d.strftime("%Y-%m-%d")
    for k in range(2 + back % 3):
        repo, branch, agent, lines = past[(back + k) % len(past)]
        n += 1
        sid = f"demo-past-{n}"
        first = d.replace(hour=8 + k * 2, minute=10 * k)
        last = first + timedelta(hours=2 + (back + k) % 4, minutes=25)
        hrs = 0.8 + ((back * 7 + k * 3) % 20) / 10
        key = "github.com/" + repo
        q("INSERT OR IGNORE INTO projects(key,name) VALUES(?,?)", (key, repo.split("/")[1]))
        q("""INSERT INTO sessions(id,project_key,agent,cwd,branch,started_at,last_activity_at,last_event_at,last_event_is_ai,ai_active_seconds,status,tail,transcript_path)
             VALUES(?,?,?,?,?,?,?,?,1,?,'','ai_ended','')""",
          (sid, key, agent, "/home/dev/" + repo.split("/")[1], branch, z(first), z(last), z(last), hrs * 3600))
        buckets = [0.0] * 24
        for hr in range(first.hour, last.hour + 1):
            buckets[hr] = hrs * 3600 / (last.hour - first.hour + 1)
        q("INSERT INTO session_days(session_id,day,ai_seconds,first_at,last_at,hours) VALUES(?,?,?,?,?,?)",
          (sid, dstr, hrs * 3600, z(first), z(last), json.dumps(buckets)))
        for i in range(4 + (back + k) % 6):
            q("INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES(?,'prompt',?, '')", (sid, z(first + timedelta(minutes=20 * i))))
        q("INSERT INTO summaries(level,subject_id,as_of,narrative,structured,schema_version) VALUES('session_day',?,?,'',?,6)",
          (f"{sid}/{dstr}", z(last), json.dumps(lines)))
    if back % 2:
        q("INSERT INTO finishes(source,external_id,kind,key,title,url,at) VALUES('linear',?,'task',?,?,?,?)",
          (f"lin-past-{back}", f"ENG-{120 + back}", ["Paginate the search results", "Fix the dark mode contrast", "Send the receipt by email", "Speed up the product page"][back % 4],
           f"https://linear.app/acme/issue/ENG-{120 + back}", z(d.replace(hour=16, minute=30))))

# Stickers.
q("INSERT INTO stickers(id,kind,pic,name,x,y,w) VALUES('st-water','counter','water','Water',1150,1180,72)")
q("INSERT INTO sticker_days(sticker_id,day,count) VALUES('st-water',?,3)", (day,))

db.commit()
print("seeded", args.db, "at", now.isoformat(timespec="minutes"))
