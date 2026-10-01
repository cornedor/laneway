# 5. Your day

**In this chapter:** start the day on one screen, log your time without a
spreadsheet, have your standup written for you, and see what others did on
your issues while you were busy.

## Home: `~`

`~` shows your day on one screen: your open work, unread inbox threads, the
sprint (done against the time gone, *behind* when it trails), the running
timer, what waits on your review and a count per saved search. `enter`
opens the row; on a heading, its own screen. To start every day there:

```yaml
ui:
  home: [work, inbox, sprint, timer, reviews, filters]   # any of them, in your order
```

## Log work

`w` in the panel logs time on the issue: `1h 30m fixed the flaky test`
(also `1.5h`, `45m`, `2d`). It ends now, unless you put a day first:
`yesterday 2h`, `fri 1h`, `2026-09-21 3h`. It comes off the issue's
remaining estimate; add `left:2h` to say what's left instead.

## The timer

`T` starts a timer on the card or issue you're on; it shows in the header
and on the card (`⏱ 12m`) and survives a restart. `T` again stops it into
the same log input, filled in with the time. Started on the wrong card?
`ctrl+d` there drops it unlogged. Switching tasks? `T` on the next card logs
this one and starts timing that one.

> **Try it:** `T` on the card you're about to work on. When you're done,
> `T` again, type what you did, `enter`. Your time is logged.

## Today's work: `W`

`W` lists what you logged today, with the total. `[` `]` step a day, `e`
edits an entry, `d` twice deletes it, `y` copies the day as a table. `p`
proposes what you forgot to log, from your commits and branch switches in
`jira.repos` and whatever `ui.activity` commands report (agent logs, shell
history); `enter` on one logs it. Point `ui.calendar` at your calendar (an
`.ics` URL or file, or khal's vdir) and set `ui.meeting_key` to the issue you
book meetings on, and each meeting that day is proposed too, its title as
the comment.

![Today's worklogs](../screenshots/worklogs.png)

`W` again shows the whole week as a grid, an issue per row and a day per
column, with the totals and how short each past workday is of 8h. `enter`
on an empty cell logs work there; `#` adds a row for an issue you haven't
logged on yet this week.

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

`I` shows what others did on your issues this week, on every site you
added: one thread per issue, mentions of you first, each in full beside the
list. Reading one leaves the rest unread. `e` marks a thread done until
something new happens, `s` snoozes it till tomorrow, `u` makes it unread,
`c` / `R` comment and reply. The header shows `✉ 3` for unread threads.

![Others' changes and a mention in the inbox](../screenshots/inbox.png)

## My work: `O`

`O` shows what's assigned to you in every project, open or done this week,
grouped by status. `ui.my_work_jql` changes what it asks.

![Your issues across projects, by status](../screenshots/mywork.png)

## Recap

- `~` is your day on one screen; `ui.home` starts there.
- `w` logs work, `T` times it, `W` shows the day (`p` proposes the gaps,
  meetings included), `W` again the week.
- `U` writes your standup, `tab` there the team's.
- `I` shows what others did, `O` all your work.

Previous: [Planning and reporting](04-planning-and-reporting.md) · Next:
[Work on an issue](06-work-on-an-issue.md)
