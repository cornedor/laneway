#!/usr/bin/env bash
# Record a tour of laneway web -demo as shots/<name>.mp4.
#
#   ./video.sh [scenario]   (default web/tour.mjs)
#
# LANEWAY is the binary (default: laneway on PATH); CHROMIUM the browser
# Playwright drives (default: its own). Needs node and ffmpeg.
set -u
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
mkdir -p "$HERE/shots" "$HERE/.run"

scenario=$(realpath "${1:-$HERE/web/tour.mjs}")
name=web-$(basename "$scenario" .mjs)
LANEWAY=${LANEWAY:-$(command -v laneway)}
addr=127.0.0.1:${PORT:-8597}

[ -d "$HERE/web/node_modules" ] || (cd "$HERE/web" && npm ci --silent) || exit 1

# A fresh server each time: the demo keeps writes in memory, and the tour moves a card.
"$LANEWAY" web -demo -no-open -addr "$addr" 2>"$HERE/.run/web.err" &
server=$!
trap 'kill $server 2>/dev/null' EXIT
for _ in $(seq 50); do curl -sf "http://$addr/" >/dev/null && break; sleep 0.2; done

node "$HERE/web/record.mjs" "$scenario" "$HERE/shots/$name.mp4" "http://$addr"
