#!/usr/bin/env bash
# Capture one screenshot of laneway -demo.
#
#   ./shoot.sh <name> <cols> <rows> <settle-seconds> [keys...]
#
# Keys are tmux send-keys arguments, plus two forms of its own:
#   lit:TEXT    send TEXT literally (send-keys -l)
#   wait:N      sleep N seconds
#
# LANEWAY is the binary (default: laneway on PATH); CHROMIUM the browser
# render.py drives. The PNG lands in shots/.
set -u
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export TMUX_TMPDIR=${LANE_TMUX:-$HERE/.run}
mkdir -p "$TMUX_TMPDIR" "$HERE/shots" "$HERE/.run"

name=$1; cols=$2; rows=$3; settle=$4; shift 4
LANEWAY=${LANEWAY:-$(command -v laneway)}
tmux -L lane kill-session -t lw 2>/dev/null
sleep 0.4
# stty tab3: tabs expand on output, so the renderer moves the cursor with
# spaces, not tabs; capture-pane keeps tabs, which no stop maps back.
tmux -L lane -f /dev/null new-session -d -x "$cols" -y "$rows" -s lw \
  -e XDG_CACHE_HOME="$HERE/.run/cache" -e COLORTERM=truecolor -e TERM=xterm-256color \
  "stty tab3; $LANEWAY -demo -config $HERE/config.yaml 2>$HERE/.run/laneway.err; sleep 900"
sleep "$settle"

while [ $# -gt 0 ]; do
  case "$1" in
    wait:*) sleep "${1#wait:}" ;;
    lit:*)  tmux -L lane send-keys -t lw -l "${1#lit:}" ;;
    *)      tmux -L lane send-keys -t lw "$1" ;;
  esac
  shift
done

sleep 1.5
tmux -L lane capture-pane -p -e -t lw > "$HERE/shots/$name.ansi"
"${PY:-python3}" "$HERE/tidy.py" "$HERE/shots/$name.ansi"
"${PY:-python3}" "$HERE/render.py" "$HERE/shots/$name.ansi" "$HERE/shots/$name.png" "$cols"
