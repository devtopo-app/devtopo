# Media

The README's GIFs, recorded from a demo DevTopo with made-up data (acme
projects, ENG tickets), so nothing private is in them. To record them again
after the UI changes, from the repository root, with a built devtopo and
playwright-core where node finds it:

    docs/media/demo/record-all.sh ./devtopo.exe            # all of them
    docs/media/demo/record-all.sh ./devtopo.exe planner    # just one

- `demo/seed.py` builds the demo database, as of 14:20 today.
- `demo/record.js` plays each scene in Edge: hero, today, sessions, planner,
  history and ask. The status bar's Claude and Codex usage is made up, and Ask
  answers from a prepared reply.
- `demo/make-gif.py` turns the frames into a GIF.
