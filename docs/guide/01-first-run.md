# 1. First run

**In this chapter:** install laneway, give it a Jira token, and get your
team's board on screen. At the end you can find your way around a board
and back out again.

## Install

Pick one:

```sh
brew install cornedor/tap/laneway      # macOS
yay -S laneway                         # Arch (AUR), laneway-git for main
nix run github:cornedor/laneway
go install github.com/cornedor/laneway@latest
```

Or grab a binary from
[Releases](https://github.com/cornedor/laneway/releases) and put it on your
`PATH`. When a newer release is out, `↑ v1.2` shows in the header and the
palette (`:`) has the command that updates it.

**Try it first:** `laneway -demo` (or `demo` at the first start's site
prompt) opens a generated project, with sprints,
comments and worklogs, served inside laneway itself. No site, no token;
move cards and comment all you like, it is gone when you quit. Most of this
guide works on it; git, agents and your own sites need the real thing.

## Tell it where your Jira is

Run `laneway`. The first time, it has no config yet, so it asks three
things:

```
Connect laneway to a Jira site. ctrl+d cancels.

Jira site, its name (acme) or URL: acme
  https://acme.atlassian.net
Email: you@example.com
API token: make one at https://id.atlassian.com/manage-profile/security/api-tokens
  paste it, or press enter to open that page:
  signing in… ✓ signed in as Ada Lovelace

Saved as jira in ~/.config/laneway/config.yaml.
```

For the token, press `enter` and the page opens: *Create API token*, name
it "laneway", copy it, paste it back (it isn't shown). laneway signs in
before it saves anything; a typo says what went wrong and asks again, with
`enter` keeping what you typed. Then your board opens.

The config it wrote is plain YAML: `~/.config/laneway/config.yaml`.

> **Tip:** rather not keep the token in a file? Where there is a keyring
> (`secret-tool`, macOS Keychain) setup offers to keep it there. Or export
> `JIRA_API_TOKEN` first; setup then offers that, and the file gets none.

> **Tip:** add `projects: [ABC]` under `jira:` to put the projects you work
> in at the top of the project picker.

Token expired? `laneway setup` again with the same site replaces the
email and token and leaves the rest of the config alone;
`laneway -site club setup` fills in club's URL for you.

## Your board

Run `laneway` again. It opens on a board of the first project in
`projects` (or, without that list, the first project you can see by name):
the board's columns side by side as lanes, one card per issue.

Two keys pick what you look at:

| Key | Does |
| --- | --- |
| `p` | pick a project (type to filter) |
| `b` | pick one of the project's boards |

laneway remembers both, so next time it opens right there, from a cached
copy while the fresh one loads.

> **Try it:** press `p`, type a few letters of your project, `enter`. Then
> `b` to pick your team's board.

## Look around

| Key | Does |
| --- | --- |
| arrows or `h` `j` `k` `l` | move between cards and lanes |
| `[` `]` | the board's views: active sprint, next sprints, backlog |
| `t` | swap the lanes for a sortable list, and back |
| `enter` | open the card's issue in a panel beside the board |
| `esc` | close the panel again |
| `?` | every key, as bound on your machine |
| `q` | quit |

![The same board as a list](../screenshots/list.png)

> **Try it:** press `t` for the list, then `s` a few times to sort it by
> priority, points, assignee… `t` again brings the lanes back.

> **Tip:** lost? `?` opens the help at the screen you are on, and `:` opens
> the command palette: every action you can take right now, searchable by
> any word.

## More than one Jira?

Press `@` and pick *+ add a Jira site*: the same three questions, and a
name to pick it by (`club` for club.atlassian.net). It lands under
`sites:`, and laneway opens on it. `@` switches between them after that,
and the next start opens the one you last picked; `laneway -site club`
starts on one. `laneway setup` does the same from
the shell.

```yaml
sites:
  club: {base_url: https://club.atlassian.net, email: you@example.com, api_token: ...}
```

## Recap

- The first `laneway` asks for your Jira, email and token and saves them in `~/.config/laneway/config.yaml`; `@` adds another site.
- `p` project, `b` board, `[` `]` views, `t` lanes or list.
- `enter` opens an issue, `esc` closes it, `?` when in doubt.

Next: [The board and the panel](02-board-and-panel.md), where you narrow
the board down and read an issue without leaving the terminal.
