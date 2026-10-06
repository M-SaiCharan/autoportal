#!/bin/sh
# Builds dist/autoportal-macos.zip containing a universal (Intel + Apple
# Silicon) autoportal.app. Must run on macOS: the menu-bar code uses Cocoa
# through cgo, which cannot be cross-compiled from Linux.
#
# usage: scripts/package-macos.sh [version]
set -eu

VERSION="${1:-dev}"
APP="dist/autoportal.app"
LDFLAGS="-s -w -X main.version=${VERSION}"

[ "$(uname -s)" = "Darwin" ] || { echo "run this on macOS" >&2; exit 1; }

rm -rf "$APP" dist/autoportal-macos.zip
mkdir -p "$APP/Contents/MacOS" dist

for arch in arm64 amd64; do
	CGO_ENABLED=1 GOOS=darwin GOARCH=$arch MACOSX_DEPLOYMENT_TARGET=12.0 \
		go build -trimpath -ldflags "$LDFLAGS" -o "dist/autoportal-darwin-$arch" ./cmd/autoportal
done
lipo -create -output "$APP/Contents/MacOS/autoportal" dist/autoportal-darwin-arm64 dist/autoportal-darwin-amd64
rm dist/autoportal-darwin-arm64 dist/autoportal-darwin-amd64

# LSUIElement: menu-bar only, no Dock icon.
cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>               <string>autoportal</string>
	<key>CFBundleDisplayName</key>        <string>autoportal</string>
	<key>CFBundleIdentifier</key>         <string>io.github.m-saicharan.autoportal</string>
	<key>CFBundleExecutable</key>         <string>autoportal</string>
	<key>CFBundlePackageType</key>        <string>APPL</string>
	<key>CFBundleShortVersionString</key> <string>${VERSION#v}</string>
	<key>CFBundleVersion</key>            <string>${VERSION#v}</string>
	<key>LSMinimumSystemVersion</key>     <string>12.0</string>
	<key>LSUIElement</key>                <true/>
	<key>NSHighResolutionCapable</key>    <true/>
</dict>
</plist>
EOF

# Ad-hoc signature: required to run on Apple Silicon. (Not a Developer ID,
# so Gatekeeper still asks once; see README.)
codesign --force --deep --sign - "$APP"

ditto -c -k --keepParent "$APP" dist/autoportal-macos.zip
echo "built dist/autoportal-macos.zip"
