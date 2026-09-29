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
