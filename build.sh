#!/bin/sh
# Builds everything in this repository, from a fresh clone:
#
#   ./build.sh              the `desk` CLI, with adb and scrcpy in ./bin, and
#                           Desk.app and Desk.dmg in desk-gui/build
#   ./build.sh --install    ... and install: Desk.app into /Applications,
#                           and `desk` into ~/.local/bin
#   ./build.sh --test       run the checks instead of building
#   ./build.sh --docs       redraw the pictures in the READMEs
#   ./build.sh --cli        only the CLI
#   ./build.sh --app        only the app
#   ./build.sh --clean      remove what the build wrote
#   ./build.sh --release    build, then publish it as a GitHub release
#
# `make` is this with shorter names; see the Makefile. `--release` hands over to
# release.sh, which does the publishing and has its own manual: ./release.sh -h
#
# The CLI needs python3 and a scrcpy release. The app needs a Go toolchain;
# desk-gui/build.sh explains what it does with it. Both look for the release
# the way the CLI itself does: $DESK_SCRCPY_DIR, then ./bin, then the newest
# ~/Downloads/scrcpy-macos-*. Set GO or DESK_BIN_DIR to override where the Go
# toolchain is, or where `desk` is installed.

set -eu
cd "$(dirname "$0")"

root=$(pwd)
scrcpy_dir=""
install=no
want_cli=yes
want_app=yes
mode=build

usage() {
	sed -n '2,/^$/p' "$0" | sed -e 's/^# \{0,1\}//'
}

for arg in "$@"; do
	case "$arg" in
	--install) install=yes ;;
	--test) mode=test ;;
	--docs) mode=docs ;;
	--clean) mode=clean ;;
	--release) exec ./release.sh ;;
	--cli) want_app=no ;;
	--app) want_cli=no ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "build: unknown argument $arg" >&2
		echo "       ./build.sh --help" >&2
		exit 2
		;;
	esac
done

# ---------------------------------------------------------------- the tools

find_go() {
	GO="${GO:-$HOME/.local/go/bin/go}"
	if ! command -v "$GO" >/dev/null 2>&1; then
		GO="$(command -v go || true)"
	fi
	if [ -z "$GO" ]; then
		echo "build: no Go toolchain." >&2
		echo "       Install Go (https://go.dev/dl), or set GO to its go binary." >&2
		exit 1
	fi
}

# Sets scrcpy_dir to a folder holding adb, scrcpy and scrcpy-server.
find_scrcpy_dir() {
	if [ -n "${DESK_SCRCPY_DIR:-}" ]; then
		scrcpy_dir="$DESK_SCRCPY_DIR"
	else
		# The newest unpacked release by name, which sorts by version.
		for d in "$HOME"/Downloads/scrcpy-macos-*; do
			if [ -x "$d/scrcpy" ]; then
				scrcpy_dir="$d"
			fi
		done
	fi
	if [ -n "$scrcpy_dir" ] && [ -x "$scrcpy_dir/scrcpy" ] && [ -f "$scrcpy_dir/adb" ]; then
		return 0
	fi
	# Already bundled by an earlier run: good enough to build from again.
	if [ -x bin/scrcpy ] && [ -f bin/adb ] && [ -f bin/scrcpy-server ]; then
		scrcpy_dir="$root/bin"
		return 0
	fi
	echo "build: no scrcpy release found." >&2
	echo "       Download one from https://github.com/Genymobile/scrcpy/releases," >&2
	echo "       unpack it into ~/Downloads, or set DESK_SCRCPY_DIR to the folder" >&2
	echo "       holding scrcpy, adb and scrcpy-server." >&2
	exit 1
}

# -------------------------------------------------------------- the steps

step_bin() {
	# `desk` looks in ./bin before anywhere else, so with these three files
	# there it runs with nothing installed on the Mac but python3.
	echo "==> bundling adb and scrcpy for the CLI, from $scrcpy_dir"
	mkdir -p bin
	for f in adb scrcpy scrcpy-server; do
		if [ ! -f "$scrcpy_dir/$f" ]; then
			echo "build: $scrcpy_dir has no $f" >&2
			exit 1
		fi
		cmp -s "$scrcpy_dir/$f" "bin/$f" || cp "$scrcpy_dir/$f" "bin/$f"
	done
	chmod 755 bin/adb bin/scrcpy
	# The release came from the internet: drop the quarantine flag so the
	# copies run without a Gatekeeper prompt of their own.
	xattr -cr bin 2>/dev/null || true
}

step_cli() {
	echo "==> checking the CLI"
	chmod 755 desk
	python3 -m py_compile desk
	find_scrcpy_dir >/dev/null # it needs adb and scrcpy to be able to say so
	./desk --help >/dev/null
}

step_install_cli() {
	bin="${DESK_BIN_DIR:-$HOME/.local/bin}"
	mkdir -p "$bin"
	# A symlink, not a copy: `desk` keeps its state and its cached app list in
	# .desk/ beside the script, and it runs against this checkout.
	ln -sf "$root/desk" "$bin/desk"
	echo "==> installed $bin/desk -> $root/desk"
	case ":$PATH:" in
	*":$bin:"*) ;;
	*)
		echo
		echo "    $bin is not on your PATH. To add it, put this in ~/.zshrc:"
		echo "        export PATH=\"$bin:\$PATH\""
		;;
	esac
}

step_app() {
	echo "==> building the app"
	if [ "$install" = yes ]; then
		DESK_SCRCPY_DIR="$scrcpy_dir" desk-gui/build.sh --install
	else
		DESK_SCRCPY_DIR="$scrcpy_dir" desk-gui/build.sh
	fi
}

step_test() {
	find_go
	echo "==> gofmt"
	unformatted="$(cd desk-gui && "$(dirname "$GO")/gofmt" -l .)"
	if [ -n "$unformatted" ]; then
		echo "build: gofmt would reformat:" >&2
		echo "$unformatted" >&2
		exit 1
	fi
	echo "==> go vet"
	(cd desk-gui && "$GO" vet ./...)
	echo "==> go test"
	(cd desk-gui && "$GO" test ./...)
	echo "==> mygo vet"
	(cd desk-gui && "$GO" tool mygo vet .)
	echo "==> the CLI"
	python3 -m py_compile desk
	./desk --help >/dev/null
	echo "==> all checks pass"
}

step_docs() {
	find_go
	echo "==> redrawing the pictures in the READMEs"
	# The windows are rendered offscreen, so this needs no phone and no screen.
	(cd desk-gui &&
		"$GO" run . -capture docs/bar.png \
			-capture-drawer docs/drawer.png \
			-capture-display docs/display.png)
	echo "    docs/bar.png, docs/drawer.png, docs/display.png"
	echo "    docs/glass.png is a real screenshot: it cannot be drawn."
}

step_clean() {
	echo "==> removing what the build wrote"
	# Not .desk/: that is the CLI's own state, and its screenshots.
	rm -rf bin __pycache__ desk-gui/__pycache__ desk-gui/build \
		desk-gui/resources/darwin/bin desk-gui/desk
}

# ------------------------------------------------------------------- main

case "$mode" in
test) step_test ;;
docs) step_docs ;;
clean) step_clean ;;
build)
	if [ "$want_cli" = yes ]; then
		find_scrcpy_dir
		step_bin
		step_cli
	fi
	if [ "$want_app" = yes ]; then
		find_scrcpy_dir
		step_app
	fi
	if [ "$install" = yes ] && [ "$want_cli" = yes ]; then
		step_install_cli
	fi
	echo
	echo "Done."
	[ "$want_cli" = yes ] && echo "  cli: $root/desk   (adb and scrcpy in $root/bin)"
	[ "$want_app" = yes ] && echo "  app: $(ls -d "$root"/desk-gui/build/*/Desk.app 2>/dev/null | head -1 || echo 'desk-gui/build')"
	;;
esac
