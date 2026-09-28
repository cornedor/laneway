# 3. Editing and moving

**In this chapter:** move cards along the workflow, change fields, write
descriptions and comments, create issues, and change a dozen cards at once.
From here on laneway writes to Jira, so practise on an issue of your own.

## Move a card

On the board, `H` and `L` (or `shift+←` `shift+→`) push the card a lane
left or right. It moves on screen at once; Jira catches up behind it. With
the mouse, drag it.

- A lane holding several statuses asks which one.
- Moved the wrong card, or set the wrong priority? `u` takes back the last
  change: a move, an edit, a bulk edit or a deleted comment.
- `K` and `J` rank a card up or down within its lane.
- `M` sends it to another sprint or the backlog.

> **Try it:** pick one of your cards in To do and press `L`. Watch the
> status line: `ABC-12 → In progress`. Then `u` to put it back.

### When the workflow wants more

Some moves come with conditions: Code review needs a reviewer, Waiting for
needs a comment. Instead of failing with Jira's message, laneway opens a
small form with exactly those fields, the required ones first and marked
`*`:

- `↑` `↓` pick a field, `enter` edits it (a list for people and options,
  an editor for longer text), `del` clears it.
- `ctrl+s` makes the move. If Jira still refuses, the form stays up with
  its reason, so the fix is one edit away.
- `esc` cancels and the card goes back.

> **Tip:** the same form shows up when you create an issue in a project
> that insists on a field, such as a Component. Fill it in and the issue is
> created with it.

## Change fields

**From the board:** `e` on a card opens a quick edit: status, priority,
assignee, labels, points or sprint, without opening the panel.

**From the panel:** one key per common field.

| Key | Edits |
| --- | --- |
| `s` | status |
| `p` | priority |
| `P` | story points |
| `e` | summary |
| `l` | labels |
| `a` | assignee ("Assign to me" comes first) |
| `E` | the description |

Any other field, custom ones too: `tab` to it, `enter`. Text, numbers and
dates edit right in their row; people and options drop a list under it.

> **Tip:** dates understand you: `2026-10-01`, `today`, `+3d`, `fri`. A
> date-time takes `fri 14:00`.

## Write

`E` opens the description as markdown in an editor, right where it was.
Bold, italic, code and links show styled as you type.

- `enter` is a newline, `ctrl+s` saves, `esc` cancels (it asks once if you
  changed something).
- `ctrl+e` hands the text to your own `$EDITOR`.
- Things markdown can't hold, like a table or an image, stand as a
  `<!-- keep:1 table … -->` line. Move the line and the block moves; leave
  it and the block comes back untouched.

## Talk

- `c` writes a comment after the thread. `enter` or `ctrl+s` posts it.
- `R` replies to a comment, composed right under it.
- `@` and a few letters list people; `tab` inserts a mention that notifies
  them.
- Your own comments: `A` → Edit a comment of yours, or delete it.

> **Try it:** on an issue a colleague works on, press `c`, type `@` and
> the first letters of their name, `tab`, then your message. `esc` instead
> of `enter` if you'd rather not send it; laneway asks before dropping
> what you wrote.

## More actions

`A` in the panel holds the rest: a subtask (or a child for an epic), a link
to another issue (type its key or words, check the hit, `enter`) or a web page, clone, change its type, move it to another project or delete it, watch it or add and remove watchers, vote, flag, upload a file, the image on
your clipboard or a screenshot of a region, download or delete an attachment. With the project in
`jira.repos` it also opens a draft pull request from the issue's branch
(`gh` or `glab`), which `D` then lists.

## Create an issue

`n` on the board opens one form: the type (`←` `→` change it; it starts on
the type you last made), the summary, where the cursor waits, and the
description. `enter` on the summary creates it, `ctrl+s` from anywhere. The
issue lands in the board's project, and in the sprint you are looking at;
the panel opens on it. Fields the type requires, like a Component, show up
in the form as soon as the type is set, and the hint says what is still
empty. `+ more fields` shows the others the type allows, like the assignee,
labels or a due date; the form remembers which you prefer.
A subtask, an epic's child, a roadmap epic and a clone open the same form,
the parent or the clone's copy filled in. A refused create keeps the form, Jira's reasons under the fields
they are about.

> **Tip:** give a type a starting description with `ui.templates` in the
> config, such as steps, expected and actual for every new Bug.

## Many cards at once

1. `x` marks the card under the cursor; `X` marks the whole lane (or every
   row in the list, after `/` narrowed it).
2. `B` edits all marked cards: status, priority, assignee, labels, points or
   sprint. For labels, `ui -old` adds ui and removes old.
3. Cards that fail stay marked, with the reason on the status line.
   `esc` clears the marks.

> **Try it:** `/label:` shows the cards without a label. `X` marks them
> all, `B` → labels, type one, `enter`.

## Recap

- `H` `L` move, `u` undoes, `K` `J` rank, `M` to a sprint.
- `e` on the board, or one key per field in the panel; `tab` + `enter` for
  the rest.
- `E` description, `c` comment, `R` reply, `@` mention, `A` for more.
- `n` creates, `x` / `X` / `B` edit many at once.

Previous: [The board and the panel](02-board-and-panel.md) · Next:
[Planning, roadmap, charts and your time](04-planning-and-time.md)
