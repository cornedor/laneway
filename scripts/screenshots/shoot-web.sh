#!/usr/bin/env bash
# Every web screenshot, from a fresh laneway web -demo, into shots/web/.
#
#   ./shoot-web.sh [name…]   (default: all; names in web/shoot.mjs)
#
# LANEWAY is the binary (default: laneway on PATH); CHROMIUM the browser
# (default: chromium-browser or chromium on PATH, else Playwright's own).
set -u
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
mkdir -p "$HERE/shots/web" "$HERE/.run/home"

LANEWAY=${LANEWAY:-$(command -v laneway)}
CHROMIUM=${CHROMIUM:-$(command -v chromium-browser || command -v chromium || true)}
[ -n "$CHROMIUM" ] && CHROMIUM=$(command -v "$CHROMIUM")
export CHROMIUM
addr=127.0.0.1:${PORT:-8598}

[ -d "$HERE/web/node_modules" ] || (cd "$HERE/web" && npm ci --silent) || exit 1

# A fresh server: the demo keeps writes (and read inbox items) in memory.
# A home of its own, so nothing of the user's is read.
HOME="$HERE/.run/home" XDG_CONFIG_HOME="$HERE/.run/home/.config" XDG_CACHE_HOME="$HERE/.run/cache" \
  XDG_STATE_HOME="$HERE/.run/home/.state" \
  "$LANEWAY" -config "$HERE/config.yaml" web -demo -no-open -addr "$addr" 2>"$HERE/.run/web.err" &
server=$!
trap 'kill $server 2>/dev/null' EXIT
for _ in $(seq 50); do curl -sf "http://$addr/" >/dev/null && break; sleep 0.2; done

node "$HERE/web/shoot.mjs" "$HERE/shots/web" "http://$addr" "$@"
