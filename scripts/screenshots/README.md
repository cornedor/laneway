# Docs screenshots

Retakes `docs/screenshots` from `laneway -demo`, so no Jira is needed.
Needs tmux, Python 3 and Chromium (render.py draws each capture as HTML
and screenshots it headless).

```sh
go build -o scripts/screenshots/.run/laneway .
cd scripts/screenshots
LANEWAY=$PWD/.run/laneway CHROMIUM=chromium-browser ./shoot-all.sh
```

The PNGs land in `shots/`; copy the ones that changed to
`docs/screenshots`. `config.yaml` is the `ui:` they are taken with. One
shot: `./shoot.sh <name> <cols> <rows> <settle> [keys…]`, keys as in
`shoot-all.sh`. The render kit is Koen Hendriks'.

## Web video

`./video.sh` records `web/tour.mjs` (Playwright, captions per step)
against a fresh `laneway web -demo` into `shots/web-tour.mp4`. Needs node
and ffmpeg; the first run installs Playwright in `web/`. Without
`CHROMIUM` it uses Playwright's own (`npx playwright install chromium`).

```sh
LANEWAY=$PWD/.run/laneway CHROMIUM=chromium-browser ./video.sh
```

Another scenario: `./video.sh path/to/scenario.mjs`, as in `web/record.mjs`.

## Web screenshots

`./shoot-web.sh` takes `web/shoot.mjs`'s shots (1440x900, dark theme)
from a fresh `laneway web -demo` into `shots/web/`; copy them to
`docs/screenshots/web`. One shot: `./shoot-web.sh <name>…`. Needs node,
as the video.

```sh
LANEWAY=$PWD/.run/laneway ./shoot-web.sh
```
