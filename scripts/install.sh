#!/bin/sh
# One-line installer for Linux and macOS:
#   curl -fsSL https://raw.githubusercontent.com/M-SaiCharan/autoportal/main/scripts/install.sh | sh
#
# Downloads the latest release into your user folders (no sudo), then starts
# autoportal. Files fetched with curl are not quarantined, so macOS does not
# show the "unidentified developer" warning.
set -eu

REPO="M-SaiCharan/autoportal"
BASE="https://github.com/$REPO/releases/latest/download"

os=$(uname -s)
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "unsupported CPU: $arch" >&2; exit 1 ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

case "$os" in
Linux)
	dest="$HOME/.local/bin/autoportal"
	echo "Downloading autoportal for Linux ($arch)…"
	curl -fL --progress-bar -o "$tmp/autoportal" "$BASE/autoportal-linux-$arch"
	chmod +x "$tmp/autoportal"
	mkdir -p "$(dirname "$dest")"
	mv -f "$tmp/autoportal" "$dest"
	echo "Installed to $dest"
	# Start the tray app detached from this terminal.
	nohup "$dest" tray >/dev/null 2>&1 &
	;;
Darwin)
	dest="$HOME/Applications/autoportal.app"
	echo "Downloading autoportal for macOS…"
	curl -fL --progress-bar -o "$tmp/autoportal.zip" "$BASE/autoportal-macos.zip"
	mkdir -p "$HOME/Applications"
	rm -rf "$dest"
	ditto -x -k "$tmp/autoportal.zip" "$HOME/Applications"
	echo "Installed to $dest"
	open "$dest"
	;;
*)
	echo "unsupported OS: $os (on Windows, use install.ps1)" >&2
	exit 1
	;;
esac

echo "autoportal is starting. Look for its icon in the menu bar / system tray."
