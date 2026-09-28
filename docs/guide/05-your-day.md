# 5. Your day

**In this chapter:** log your time without a spreadsheet, have your standup
written for you, and see what others did on your issues while you were
busy.

## Log work

`w` in the panel logs time on the issue: `1h 30m fixed the flaky test`
(also `1.5h`, `45m`, `2d`). It ends now, unless you put a day first:
`yesterday 2h`, `fri 1h`, `2026-09-21 3h`. It comes off the issue's
remaining estimate; add `left:2h` to say what's left instead.

## The timer

`T` starts a timer on the card or issue you're on; it shows in the header
and survives a restart. `T` again stops it into the same log input, filled
in with the time.

> **Try it:** `T` on the card you're about to work on. When you're done,
> `T` again, type what you did, `enter`. Your time is logged.

## Today's work: `W`

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

## Standup: `U`

`U` swaps the board for your standup since the previous workday (Friday, on
a Monday), one row per issue with what changed: *Done since*, *In progress*
(a card of yours with no activity too, with how long it sat), *Next* in the
sprint and *Blockers* (flagged). With `jira.repos` set your git commits
count too. `y` puts it on the clipboard as Yesterday / Today / Blockers; `[`
goes a workday further back.

![What you did since Friday](../screenshots/standup.png)

Running the standup? `tab` switches to the team's: it walks the board right
to left, closest to done first, each card with who has it, how long it has
been in progress, and what happened since the last workday. A card marked
*no activity* is the one worth raising, as is one *blocked by* another. `p`
groups it per person instead. `space` shows one card at a time; `P` parks a
card that needs a longer talk, in the parking lot at the end.

> **Try it:** `U`, `y`, paste it in your team's standup channel. Done
> before the coffee's ready.

## Inbox: `I`

`I` lists what others did on your issues since you last looked, on every
site you added: field changes and comments, mentions of you first, marked
`@`. The header shows `✉ 3` when there's something new. Opened it by
accident? Its last row, `↶ the inbox before`, brings the previous one back.

![Others' changes and a mention in the inbox](../screenshots/inbox.png)

## My work: `O`

`O` shows what's assigned to you in every project, open or done this week,
grouped by status. `ui.my_work_jql` changes what it asks.

![Your issues across projects, by status](../screenshots/mywork.png)

## Recap

- `w` logs work, `T` times it, `W` shows the day (`p` proposes the gaps),
  `W` again the week.
- `U` writes your standup, `tab` there the team's.
- `I` shows what others did, `O` all your work.

Previous: [Planning and reporting](04-planning-and-reporting.md) · Next:
[Work on an issue](06-work-on-an-issue.md)
