# 1. First run

**In this chapter:** install laneway, give it a Jira token, and get your
team's board on screen. At the end you can find your way around a board
and back out again.

## Install

With Go:

```sh
go install github.com/cornedor/laneway@latest
```

No Go? Grab a binary from
[Releases](https://github.com/cornedor/laneway/releases) and put it on your
`PATH`.

## Tell it where your Jira is

Run `laneway`. The first time, it has no config yet, so it prints the three
lines it needs and where to put them:

```yaml
jira:
  base_url: https://your-instance.atlassian.net
  email: you@example.com
  api_token: ...   # or JIRA_API_TOKEN
```

Save that as `~/.config/laneway/config.yaml` with your own values.

The token comes from
<https://id.atlassian.com/manage-profile/security/api-tokens>: *Create API
token*, give it a name like "laneway", copy it.

> **Tip:** rather not keep the token in a file? Leave `api_token` out and
> export `JIRA_API_TOKEN` in your shell instead.

> **Tip:** add `projects: [ABC]` under `jira:` to put the projects you work
> in at the top of the project picker.

Got something wrong? laneway names exactly what is missing (`jira: set
api_token …`) or off (a `base_url` without `https://`), so read the first
line it prints.

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

Add the others under `sites:` and switch with `@` in the app, or start on
one with `laneway -site club`:

```yaml
sites:
  club: {base_url: https://club.atlassian.net, email: you@example.com, api_token: ...}
```

## Recap

- `~/.config/laneway/config.yaml` holds your Jira address, email and token.
- `p` project, `b` board, `[` `]` views, `t` lanes or list.
- `enter` opens an issue, `esc` closes it, `?` when in doubt.

Next: [The board and the panel](02-board-and-panel.md), where you narrow
the board down and read an issue without leaving the terminal.
