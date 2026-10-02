#!/bin/sh
# Builds dist/Laneway-mac.zip (and its .sha256, for the app's updater): Laneway.app, universal, ad-hoc signed, with
# the laneway binary inside. Run on macOS from desktop/: ./build-mac.sh 1.2.3
set -eu
cd "$(dirname "$0")"
version=${1:-0.0.0}
app=dist/Laneway.app
rm -rf dist
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
# Go's own floor; else cgo targets the build machine's macOS.
export MACOSX_DEPLOYMENT_TARGET=13.0

for arch in arm64 amd64; do
	CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -ldflags "-s -w -X main.version=$version" -o "dist/desktop-$arch" .
	(cd .. && CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -ldflags "-s -w -X main.version=$version" -o "desktop/dist/laneway-$arch" .)
done
lipo -create -output "$app/Contents/MacOS/laneway-desktop" dist/desktop-arm64 dist/desktop-amd64
lipo -create -output "$app/Contents/MacOS/laneway" dist/laneway-arm64 dist/laneway-amd64
sed "s/VERSION/$version/g" darwin/Info.plist > "$app/Contents/Info.plist"

icons=dist/icon.iconset
mkdir -p "$icons"
for s in 16 32 128 256 512; do
	sips -z $s $s ../internal/web/static/icon-512.png --out "$icons/icon_${s}x${s}.png" >/dev/null
	if [ $s -le 256 ]; then
		sips -z $((s * 2)) $((s * 2)) ../internal/web/static/icon-512.png --out "$icons/icon_${s}x${s}@2x.png" >/dev/null
	fi
done
iconutil -c icns -o "$app/Contents/Resources/icon.icns" "$icons"

# Ad-hoc: no Apple account. Enough for notifications; Gatekeeper still asks once.
codesign --force --sign - "$app/Contents/MacOS/laneway"
codesign --force --sign - "$app"
codesign --verify --verbose "$app"
# No xattrs or resource forks: unpacked by the updater they would be ._ files that break the seal.
ditto -c -k --norsrc --noextattr --keepParent "$app" dist/Laneway-mac.zip
(cd dist && shasum -a 256 Laneway-mac.zip > Laneway-mac.zip.sha256)
