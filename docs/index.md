# laneway

A terminal board for Jira. One board of one project as swim lanes or a list,
the selected issue in a panel on the right. Move cards, change status,
priority, points, assignee, summary and labels, comment and reply, all
without leaving the terminal.

![The board as swim lanes](screenshots/board.png)

## Install

```sh
brew install cornedor/tap/laneway      # macOS
nix run github:cornedor/laneway
go install github.com/cornedor/laneway@latest
```

Or grab a binary from [Releases](https://github.com/cornedor/laneway/releases).
`laneway -demo` tries it on a generated board, no Jira needed.

## Where to start

- **New here?** The [guide](guide/README.md) walks you from first run to
  planning sprints and writing rules, with tips and things to try.
- **Looking for one key or option?** The [reference](reference.md) lists
  every key, config option and search term.
- **Inside laneway**, `?` shows every key as bound and `:` finds any action.
