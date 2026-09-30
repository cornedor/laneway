# 6. Work on an issue

**In this chapter:** take an issue from the board to a merged pull request:
a branch, commits that name it, a coding agent if you like, a draft pull
request, and the reviews waiting on you. This needs a real repository, so
not the demo.

## Tell laneway where the code is

Map each project to its checkout:

```yaml
jira:
  repos: {ABC: ~/src/abc}
```

That one line feeds most of this chapter, and your commits into the
standup (`U`) and the worklog proposals (`W` → `p`).

## A branch for the issue

`ctrl+y` on a card or in the panel copies a branch name for it
(`ABC-12-login-fails`; `ui.branch_template` shapes it). Start laneway
inside a branch named after an issue and it opens that issue; `⎇ ABC-12` in
the header opens it again.

## Commits that name the issue

In the repository, once:

```sh
laneway hook install
```

From then on a commit on `issue/ABC-12-fix` that names no issue gets the
key in front: `fix it` becomes `ABC-12 fix it`. A branch without a key
refuses the commit; merges, reverts and fixups pass. `-strict` also
refuses keys Jira doesn't know.

Checking out an issue's branch while it's still to do asks
`move ABC-12 (To Do) to In Progress? [y/N]`.

## Start work with an agent: `S`

With [herdr](https://herdr.dev) running, `S` in the panel starts work: the
issue's worktree opens as a herdr workspace with a coding agent in it. One
form shows the agent (those on your `PATH`, `ui.work_agent` first), the
branch and the prompt (`jira.start_prompt`), all filled in and editable;
enter starts it. An empty prompt starts the agent without one. Without
herdr, `S` isn't there.

Starting can do more, each off until you turn it on:

```yaml
ui:
  start_assigns: on           # assign the issue to you
  start_status: In Progress   # and move it there
  timer_on_start: on          # and start the timer
```

A project can move to another status, or to none:

```yaml
jira:
  start_statuses: {ABC: Doing, OPS: ""}   # over ui.start_status
```

With any of them on, the form has an **Also** row listing them, ticked;
enter on it unticks them for this start.

While it runs, its card shows how it's doing:

| Mark | Means |
| --- | --- |
| `⚙` | working |
| `✋` | waiting on you (with a desktop notification) |
| `✓` | done, not looked at yet |
| `○` | idle |
| `◌` | a worktree without an agent |

`S` again, a click on the mark, or a click on the agent's row in the
panel, attaches to its terminal; herdr's
`ctrl+b q` detaches. Rather keep the board in sight? `ui.agent_view: panel`
puts the agent in the panel instead, and `ctrl+\` goes back to the issue.
In the panel, `A` sends an agent a prompt, stops it, or starts another in
the same worktree.

> **Try it:** `S` on a small bug, pick your agent, `enter` on the empty
> prompt. Go back to the board and watch its card turn `✋` or `✓`.

## All your agents: `ctrl+g`

`ctrl+g` swaps the board for every agent on an issue, grouped by state,
the ones waiting on you first, each with its issue's summary and status,
whichever site it's on. Beside the list: the cursor's agent's own
terminal, live. `enter` or a click types into it, `ctrl+\` goes back to
the list. `v` opens the issue, `p` sends a prompt, `d` twice stops it.
`tab` adds the worktrees without an agent. The header counts the agents
waiting (`✋1`) and working (`⚙2`); a click opens the list.

## Open a pull request

`A` → *Open a pull request* pushes the issue's branch and opens a draft,
titled with the key and summary and linking the issue: `gh` for a GitHub
origin, `glab` otherwise. `D` in the panel lists it, with its builds,
deployments, branches and commits. Cards show the state (`pr` in
`ui.card_fields`), and `/pr:open` finds the ones with an open one.

## Reviews waiting on you: `ctrl+r`

`ctrl+r` shows the issues whose pull or merge requests wait on your review
as a view, their cards marked `⌥`.

## Clean up

On a done issue, `A` → *Remove its worktree* removes the checkout once its
branch is merged. Uncommitted changes keep it; the branch stays. `ctrl+g`
then `tab` lists every issue that still has one, across projects.

## Recap

- `jira.repos` maps a project to its checkout.
- `ctrl+y` a branch name, `laneway hook install` keys in commits.
- `S` starts an agent in a worktree, the card shows its state, `S` attaches.
- `ctrl+g` all your agents by state, each one's terminal beside the list.
- `A` → pull request, `D` lists it, `ctrl+r` your reviews, `A` → remove
  the worktree.

Previous: [Your day](05-your-day.md) · Next:
[From the shell](07-from-the-shell.md)
