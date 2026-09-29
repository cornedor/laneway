# 7. From the shell

**In this chapter:** use laneway without the board: in scripts, in your
prompt, in the background. And what happens when the connection drops.

## Scripts

Four commands, no board:

```sh
laneway list                                   # your open issues
laneway list -jql 'project = ABC ORDER BY rank' -format csv
laneway view ABC-12                            # one issue; -format json
laneway create -project ABC -type Bug -summary 'Login fails'
laneway move ABC-12 'In Progress'              # any status it can move to
```

`-format json` suits `jq`; `-site club` picks another site. Flags go before
the key.

> **Try it:** `laneway list -format json | jq -r '.[].key'`.

## Completion

```sh
source <(laneway completion bash)          # in ~/.bashrc; or zsh
laneway completion fish > ~/.config/fish/completions/laneway.fish
```

It completes commands, flags, site names and, for `view` and `move`, the
keys on the boards laneway last loaded.

## Your prompt

`laneway prompt` prints the branch's issue, its status, the timer and the
inbox count: `ABC-12 · In review · ⏱ 1h 20m · ✉ 3`. It reads laneway's
state file only, never Jira, so it is fast enough for every prompt.

```toml
# starship.toml
[custom.laneway]
command = "laneway prompt"
when = true
```

```sh
# tmux.conf
set -g status-right '#(laneway prompt -format "{{.Key}} {{.Timer}}")'
```

## Rules in the background

`laneway rules watch` runs your `watch:` rules without the board, printing
what fires: leave it in a tmux pane, or a user service. Chapter 8 shows
how to write them.

## When the connection drops

laneway keeps working.

- **Reading.** Every issue it has read is kept in a local index. Offline,
  the palette (`:`) and the panel answer from it and say so. It keeps the
  project's people as well, so `@` mentions and the assignee picker work
  offline too. `laneway index` shows what it holds, `laneway index clear`
  empties it.
- **Writing.** A change that never reached Jira waits in a queue, `⇡3` in
  the header, and goes out by itself once Jira answers, oldest first. If
  the issue changed in Jira meanwhile, it waits for you: the palette's
  *queue* row sends it anyway or drops it.
- **Typing.** A comment or description you're writing is kept as a draft;
  after a crash, `c` or `E` on the issue brings it back.

## Recap

- `list`, `view`, `create`, `move` for scripts; `completion` for your shell.
- `laneway prompt` for the prompt or tmux, `rules watch` in the background.
- Offline: reads come from the index, writes wait in the `⇡` queue.

Previous: [Work on an issue](06-work-on-an-issue.md) · Next:
[Make it yours](08-make-it-yours.md)
