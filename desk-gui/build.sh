#!/bin/sh
# Builds Desk.app and Desk.dmg.
#
# Desk drives scrcpy and adb, so it ships them: the script copies them into
# resources/darwin/bin, which `mygo build` installs as Contents/Resources/bin
# in the bundle, signed along with the app. Nothing else has to be installed
# on the Mac that runs it.
#
#   ./build.sh            build the app and the disk image
#   ./build.sh --install  build, then copy the app into /Applications
#
# Set DESK_SCRCPY_DIR to use a scrcpy release other than the newest one in
# ~/Downloads.

set -eu
cd "$(dirname "$0")"

install=no
for arg in "$@"; do
	case "$arg" in
	--install) install=yes ;;
	*)
		echo "build: unknown argument $arg (only --install)" >&2
		exit 2
		;;
	esac
done

GO="${GO:-$HOME/.local/go/bin/go}"
if ! command -v "$GO" >/dev/null 2>&1; then
	GO="$(command -v go)" || {
		echo "build: no Go toolchain: install Go, or set GO" >&2
		exit 1
	}
fi

# ---------------------------------------------------------------- the icon

echo "==> drawing the icon"
"$GO" run ./cmd/mkicon -o resources/icon.png

# ------------------------------------------------------- scrcpy and adb

echo "==> bundling scrcpy and adb"
src="${DESK_SCRCPY_DIR:-}"
if [ -z "$src" ]; then
	# The newest unpacked release by name, which sorts by version.
	for d in "$HOME"/Downloads/scrcpy-macos-*; do
		if [ -x "$d/scrcpy" ]; then
			src="$d"
		fi
	done
fi
if [ -z "$src" ] || [ ! -x "$src/scrcpy" ]; then
	echo "build: no scrcpy release found." >&2
	echo "       Download one from https://github.com/Genymobile/scrcpy/releases," >&2
	echo "       unpack it into ~/Downloads, or set DESK_SCRCPY_DIR." >&2
	exit 1
fi
echo "    from $src"

mkdir -p resources/darwin/bin
for f in adb scrcpy scrcpy-server; do
	if [ ! -f "$src/$f" ]; then
		echo "build: $src has no $f" >&2
		exit 1
	fi
	cp "$src/$f" resources/darwin/bin/$f
done
chmod 755 resources/darwin/bin/adb resources/darwin/bin/scrcpy
# The release came from the internet: drop the quarantine flag so the
# bundled copies run without a Gatekeeper prompt of their own.
xattr -cr resources/darwin/bin 2>/dev/null || true

# ------------------------------------------------------------- the app

echo "==> building the app"
"$GO" tool mygo build -sign -

app="build/darwin-arm64/Desk.app"
if [ ! -d "$app" ]; then
	# mygo build names the directory after the platform it built.
	app="$(ls -d build/*/Desk.app 2>/dev/null | head -1 || true)"
fi
dmg="$(ls build/*/Desk*.dmg 2>/dev/null | head -1 || true)"

if [ -z "$app" ] || [ ! -d "$app" ]; then
	echo "build: Desk.app was not written" >&2
	exit 1
fi

if [ "$install" = yes ]; then
	echo "==> installing into /Applications"
	rm -rf /Applications/Desk.app
	cp -R "$app" /Applications/Desk.app
	app=/Applications/Desk.app
fi

echo
echo "Done."
echo "  app: $app"
[ -n "$dmg" ] && echo "  dmg: $dmg  (open it and drag Desk onto Applications)"
echo
echo "Run it: open '$app'"
