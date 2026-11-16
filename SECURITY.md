# Security

DevTopo reads your coding-agent sessions and keeps connector keys on your
machine, so security reports matter a lot.

## Reporting a vulnerability

Please report it **privately**: on this repository's **Security** tab, choose
**Report a vulnerability**. Don't open a public issue for it.

Say what is affected, how to reproduce it and what an attacker could do. You
will get an answer within a few days, and a fix in a new release as soon as
possible; you are credited in the release notes unless you'd rather not be.

## Supported versions

Only the latest release gets security fixes. Settings - About shows your
version and whether a newer one is out.

## What DevTopo is designed to protect

- Its web page and API answer only on this machine (127.0.0.1) and only to
  requests addressed to localhost, so other websites open in your browser
  cannot read or change your data.
- Keys, tokens and calendar addresses you enter stay in the local database
  and are never sent back to the page or written to the log.
- Session text is only ever handed to your own `claude` or `codex` command,
  to write the summaries.
