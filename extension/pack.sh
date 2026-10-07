#!/bin/sh
# Builds the store upload into extension/dist/: the extension zip, plus the listing's promo tile
# and screenshots (from docs/screenshots/web). Needs zip and ImageMagick.
set -eu
cd "$(dirname "$0")"
v=$(node -p 'require("./manifest.json").version')
out=dist
rm -rf "$out" && mkdir -p "$out/store"

zip -q -X "$out/laneway-redirect-$v.zip" manifest.json background.js go.html go.js map.js options.html options.js settings.js icon-*.png

font=$(fc-match -f '%{file}' 'sans:bold')
magick -size 440x280 xc:'#1e2433' \
  \( -background none -density 384 icon.svg -resize 120x120 \) -gravity west -geometry +36+0 -composite \
  +density -font "$font" -fill white -gravity northwest -pointsize 40 -annotate +180+82 'laneway' \
  -pointsize 19 -fill '#c9d3e6' -annotate +182+142 'Jira links open in\nlaneway web' \
  "$out/store/promo-440x280.png"
for s in panel board planning; do
  magick ../docs/screenshots/web/$s.png -resize 1280x800! "$out/store/$s-1280x800.png"
done
ls -1 "$out" "$out/store"
