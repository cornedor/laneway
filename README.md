# laneway

Jira, keyboard first, in your terminal or your browser. One board of one
project as swim lanes or a list, the selected issue in a panel beside it.
Move cards, edit fields, comment and reply, plan sprints, log time and
review merge requests without opening Jira.

![The board as swim lanes](docs/screenshots/board.png)

`laneway web` serves the same app in a browser, from the same binary and
config:

![The board in the browser](docs/screenshots/web/board.png)

- Swim lanes or a sortable list, with drag and drop between lanes and to rank
- Sprints, backlog and kanban boards; the board's quick filters plus your own
- Local search, JQL with completion, a command palette, jump to any issue by key
- Issue panel with description (edited as markdown), comments, links, subtasks, an epic's children, attachments and private notes; ask an LLM command about it
- Sprint planning and refinement; burndown, velocity, cycle time and retro charts; an epic roadmap; releases
- A home screen of your day: your work, inbox, sprint health, timer, reviews and saved searches
- Time tracking: log work, a timer, the day's and the week's worklogs, proposals from git, your agents' activity and your calendar
- Inbox: a thread per issue others changed, on every site; a standup that walks the board, with a timer
- Git: branch keys in commits, draft pull requests, what waits on your review, a prompt segment
- GitLab merge requests: pipelines and job logs, a diff to review line by line with notes and suggestions, or a coding agent reviews it for you
- Coding agents: start one on an issue in its own worktree (herdr), see its state on the card, attach to its terminal
- Rules that notify, run a command or act on Jira when issues change
- Scripts: list, view, create and move from the shell, completion, your own actions on a key
- Several Jira sites, changes made offline sent later; every key and colour configurable

## Install

```sh
brew install cornedor/tap/laneway      # macOS
yay -S laneway                         # Arch (AUR), laneway-git for main
nix run github:cornedor/laneway
go install github.com/cornedor/laneway@latest
```

Or grab a binary from [Releases](https://github.com/cornedor/laneway/releases).

## Start

```sh
laneway              # the terminal app; the first start asks for your Jira and a token
laneway web          # the browser app on http://127.0.0.1:8484; the first start shows a setup page
laneway -demo        # a generated project, no Jira needed (laneway web -demo too)
```

The [documentation](https://cornedor.github.io/laneway/) has a guide from
first run to running your sprint, for the terminal and the browser, and a
reference of every key, option and command
([source](docs/)). Inside laneway, `?` lists the keys and `:` finds any
action.

## Build

`make` · `make test` · `make install`. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
