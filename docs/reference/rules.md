# Rules

Top-level `rules:` in the config fire on what a board refresh shows changed
since the last refresh of the same view and filters: a new issue, or a
status, assignee, priority, points or summary change, yours included unless
`by_me: false`. They run in the terminal and in `laneway web` alike.

```yaml
rules:
  - name: done-bugs
    on: status                  # new status assignee priority points summary; default all
    match:                      # all must hold; globs, case-insensitive, one or a list
      type: Bug
      status: [Done, "Won*"]
      from_status: "In *"
      by_me: false              # skip your own edits (reads the issue's changelog)
      # key, assignee ("none" = unassigned), priority, summary (regexp), not: {…}
    actions:
      - type: log               # appends to ~/.config/laneway/rules.log
        text: "{{.Key}} {{.OldStatus}} → {{.Status}}"   # empty: a default line
      - type: notify            # a desktop notification
        title: "{{.Key}} done"
      - type: exec              # argv; the issue as JSON on stdin and LANEWAY_* env; 30s timeout
        command: [notify-send, "{{.Key}}", "{{.Summary}}"]
      - type: highlight         # a ● on the card until you open it
        color: "#e0af68"        # optional, else the theme's highlight
      - type: transition        # move the issue along its workflow
        to: Closed
      - type: comment           # post a comment
        text: "Closed after {{.OldStatus}}"
```

Template fields: `Kind Key Summary Type Status Assignee Priority Points
Parent OldStatus OldAssignee OldPriority OldPoints Describe`.

`transition` and `comment` write to Jira, so they fire only on a change the
issue's changelog shows someone else made: never on yours, and never on
their own writes coming back on the next refresh. A transition to the
status the issue has does nothing; one its workflow doesn't offer is logged.

A bad rule is skipped and reported. A matterbox config's `rules:` are
ignored.

## Watch a search

A rule with `watch:` fires on the changes of its own JQL search instead,
polled every `every:` (default 5m, at least 1m) while laneway runs,
whichever board is open:

```yaml
  - name: mine-moved
    watch: assignee = currentUser() AND updated >= -1d
    every: 2m
    on: status
    actions: [{type: notify}]
  - name: stuck-in-review       # a time trigger: an issue enters the search
    watch: status = "In review" AND NOT status CHANGED AFTER -3d
    every: 1h
    on: new                     # issues already in it at start are the baseline
    actions: [{type: log, text: "{{.Key}} in review 3 days"}]
```

## Where they show

=== "Terminal"

    `notify` is a desktop notification through the terminal (OSC 777:
    kitty, Ghostty, WezTerm, foot). `highlight` marks the card `●`. A bad
    rule shows on the status line.

=== "Browser"

    `g l` lists the rules, a live feed of what fired, and *Try a change*: a
    form (`t`) that says which rules a change would fire and what stopped
    the rest. `notify` is a toast, and a browser notification once you allow
    them (`N` in Rules, or Settings). `highlight` marks the card `●`.

## Test them

```sh
laneway rules list                       # what loaded
laneway rules test -on status -type Bug -status Done -from-status "In review"
laneway rules test -watch 'status = Done' -on new
laneway rules test -by-me=false          # as if someone else made the change
laneway rules watch                      # poll the watch: rules without the board
```

`rules test` says which rules the change fires and what stopped the rest,
without running anything. Left out, `-type` and `-status` come from the
project (the `-key`'s, else the first of `jira.projects`): its Task, else
first type, and that type's first to-do status. `rules_test: {type: Story,
status: Backlog}` in the config overrides them; without Jira they are Task
and To Do.

`rules watch` prints what fires; `highlight` is skipped there and `notify`
needs a terminal. All three take `-site club` for a `sites:` entry.
