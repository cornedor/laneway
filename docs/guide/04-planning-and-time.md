# 4. Planning, roadmap, charts and your time

**In this chapter:** step back from single cards. Fill a sprint against
everyone's capacity, see epics on a timeline, read the burndown, and keep
track of your own day. Each of these is one key from the board; `esc` (or
`q`) brings you back.

## Plan a sprint: `P`

On a board with sprints, `P` puts the backlog on the left and a sprint on the
right (the first future one; `[` `]` pick another). Each side counts its
cards and points; the sprint also splits them per person.

![The backlog beside the next sprint](../screenshots/planning.png)

| Key | Does |
| --- | --- |
| `←` `→` | switch side |
| `x` | mark cards |
| `M` or `space` | move the marked (or the selected) card across |
| `K` `J` | rank up or down |
| `S` | start the sprint on the right (today until the day you type, `+2w` by default) |
| `N` | a new sprint, named on from the last one |
| `R` / `E` | rename it / edit its goal |
| `C` twice | complete the active sprint; unfinished work moves to the next one |
| `y` | copy the sprint as a markdown table |
| `/` | filter both sides; `esc` clears it |
| `e` / `B` | quick edit the card (points, status, assignee…) / the marked ones |
| `u` | undo the last move across |

With the mouse, drag a card to the other side. Changes show at once and are
written behind.

> **Tip:** tell laneway how many points each person takes on:
>
> ```yaml
> ui:
>   capacity: {Ada: 13, default: 10}
> ```
>
> A person over capacity turns red and gets a `!` (`Ada 15/13!`).

> **Try it:** open `P` before your next planning meeting, mark the top
> backlog items with `x` until someone hits their limit, and `M` them into
> the sprint.

### Refine the backlog: `ctrl+e`

On the backlog view, `ctrl+e` steps through the open issues on screen (the
search and filters apply) one at a time, the ones without points first, in
a wide panel: set points (`P`), priority, labels, status, or split one into
subtasks (`A`). `J` goes to the next, `K` back;
`esc` ends it and copies what you changed, for the meeting notes.

## See the roadmap: `R`

`R` shows the project's epics on a timeline: a bar from start to due date,
filled by how much is done. Epics without dates span their children's
sprints, drawn fainter.

![Epics on a timeline, one expanded](../screenshots/roadmap.png)

- `←` `→` scroll, `+` `-` zoom (a day to two weeks per column), `.` back to
  today.
- `space` folds an epic's issues out, `enter` opens one.
- `H` `L` move a bar, `<` `>` move its end, or drag it with the mouse. The
  new dates go to Jira once you pause.
- `n` makes a new epic, `f` shows an epic's issues as a board view.

> **Tip:** a `⛓` on an epic means another open epic blocks it. It turns red
> `⛔` when that blocker ends after this one starts.

## Read the charts: `C`

On a board with sprints, `C` draws the active sprint: burndown, burnup,
cumulative flow, the velocity of the last sprints, and how long work takes
(cycle and lead time, with the 50th and 85th percentile). *Retro* compares
the last sprint with the one before, for the retrospective. `tab` steps through
them; `y` copies the open chart's numbers as a table.

![The active sprint's burndown](../screenshots/burndown.png)

> **Tip:** no story points in your team? The burndown and burnup count
> issues instead.

## Ship a release: `V`

`V` lists the project's versions with how much of each is done. `enter`
shows one's issues as a view; the `↳ release` row under an unreleased one
releases it today, on a second `enter`. Set an issue's fix version in the
panel, with its other fields.

## Your day

### Log work

`w` in the panel logs time on the issue: `1h 30m fixed the flaky test`
(also `1.5h`, `45m`, `2d`). It ends now, unless you put a day first:
`yesterday 2h`, `fri 1h`, `2026-09-21 3h`. It comes off the issue's
remaining estimate; add `left:2h` to say what's left instead.

### The timer

`T` starts a timer on the card or issue you're on; it shows in the header
and survives a restart. `T` again stops it into the same log input, filled
in with the time.

> **Try it:** `T` on the card you're about to work on. When you're done,
> `T` again, type what you did, `enter`. Your time is logged.

### Today's work: `W`

`W` lists what you logged today, with the total. `[` `]` step a day, `e`
edits an entry, `d` twice deletes it, `y` copies the day as a table. `p`
proposes what you forgot to log, from your commits and branch switches in
`jira.repos` and whatever `ui.activity` commands report (agent logs, shell
history); `enter` on one logs it.

![Today's worklogs](../screenshots/worklogs.png)

`W` again shows the whole week as a grid, an issue per row and a day per
column, with the totals and how short each workday is of 8h. `enter` on an
empty cell logs work there.

> **Try it:** on Friday, `W` `W`, fill the gaps, `y`, paste it into the
> time registration.

### Standup: `U`

`U` swaps the board for your standup since the previous workday (Friday, on
a Monday), one row per issue with what changed: *Done since*, *In progress* (a card of yours
with no activity too, with how long it sat), *Next* in the sprint and
*Blockers* (flagged). With `jira.repos` set your git commits count too. `y`
puts it on the clipboard as Yesterday / Today / Blockers; `[` goes a
workday further back.

![What you did since Friday](../screenshots/standup.png)

Running the standup? `tab` walks the board right to left, closest to done
first: each card with who has it, how long it has been in progress, and
what happened since the last workday. A card marked *no activity* is the
one worth raising, as is one *blocked by* another. `p` groups it per person
instead. `space` shows one card at a time; `P` parks a card that needs a
longer talk, in the parking lot at the end.

> **Try it:** `U`, `y`, paste it in your team's
> standup channel. Done before the coffee's ready.

### Inbox: `I`

`I` lists what others did on your issues since you last looked: field
changes and comments, mentions of you first, marked `@`. The header shows
`✉ 3` when there's something new.

![Others' changes and a mention in the inbox](../screenshots/inbox.png)

### My work: `O`

`O` shows what's assigned to you in every project, open or done this week,
grouped by status.

![Your issues across projects, by status](../screenshots/mywork.png)

### Your git

`ctrl+r` shows the issues whose pull or merge requests wait on your review.
In a repository, `laneway hook install` puts the branch's key in your
commit messages, and `laneway prompt` prints the branch's issue, the timer
and the inbox count for your shell prompt or tmux status line. See the
[reference](../reference.md#git-and-your-shell).

## Recap

- `P` planning, `ctrl+e` refinement, `R` roadmap, `C` charts (cycle time and a
  retro too), `V` releases; `esc` back to the board.
- `w` logs work, `T` times it, `W` shows the day (`p` proposes the gaps),
  `W` again the week.
- `U` writes your standup (its *Team* row everyone's), `I` shows what others
  did, `O` shows all your work, `ctrl+r` what waits on your review.

Previous: [Editing and moving](03-editing-and-moving.md) · Next:
[Rules, JQL, keys and themes](05-rules-jql-keys-themes.md)
