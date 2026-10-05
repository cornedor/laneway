# 4. Planning and reporting

**In this chapter:** step back from single cards. Fill a sprint against
everyone's capacity, see epics on a timeline, read the burndown and ship a
release. Each of these is one key from the board; `esc` (or `q`) brings you
back.

> **Kanban board?** Planning (`P`) and the charts (`C`) need a scrum board
> with sprints; the roadmap and releases work on any. A kanban board hides
> work done more than two weeks ago, like Jira does
> (`ui.kanban_done_days`).

## Plan a sprint: `P`

On a board with sprints, `P` puts the backlog on the left and a sprint on the
right (the first future one; `[` `]` pick another). Each side counts its
cards and points; the sprint also splits them per person, and weighs its
points against what the last sprints got done (`10p of ~12p`, marked over
when it is more).

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
| `/` | filter both sides, in the board's query language; `esc` clears it |
| `F` | filter builder: field, compare, value → the filter |
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
> A person over capacity turns red and gets a `!` (`Ada 15/13!`). With
> `ui.calendar` set, your meetings in the sprint come off yours: a sprint
> with a quarter of its hours in meetings leaves you three quarters.

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
them; `y` copies the open chart's numbers as a table. The burndown's title
says where the sprint stands against the ideal line: ahead, behind or on
track.

![The active sprint's burndown](../screenshots/burndown.png)

> **Tip:** no story points in your team? The burndown and burnup count
> issues instead.

## Ship a release: `V`

`V` lists the project's versions with how much of each is done. `enter`
shows one's issues as a view; the `↳ release` row under an unreleased one
releases it today, on a second `enter`. Set an issue's fix version in the
panel, with its other fields.

## Recap

- `P` planning, `ctrl+e` refinement, `R` roadmap, `C` charts (cycle time and a
  retro too), `V` releases; `esc` back to the board.

Previous: [Editing and moving](03-editing-and-moving.md) · Next:
[Your day](05-your-day.md)
