# Contributing

Thanks for wanting to help DevTopo get better.

## Issues: yes, please

Bug reports, ideas and questions are very welcome:

- **Bug?** Open an issue with the **Bug report** template. Include your OS,
  the DevTopo version (`devtopo --version`, or Settings - About), how you
  installed it, what you did and what happened. The log in
  `~/.devtopo/devtopo.log` often shows the cause; check it for anything
  private before you paste it.
- **Idea or missing feature?** Use the **Feature request** template and
  describe the problem you want solved, not only the solution.
- **Security problem?** Please don't open a public issue; see
  [SECURITY.md](SECURITY.md).

## Pull requests: not accepted

DevTopo does not take outside code, so pull requests are closed
automatically. This keeps the project's copyright in one hand and its
direction simple. It is nothing personal: if you have a fix in mind,
describe it in an issue (a snippet in the issue is fine) and it may well
make it in.

## Building it yourself

You are free to fork and change DevTopo under its [MIT license](LICENSE).
How to build it is in the README, under **Build from source**. The tests:

```sh
go vet ./... && go test ./...
cd ui && npm ci && npx vitest run && npx tsc --noEmit -p .
```
