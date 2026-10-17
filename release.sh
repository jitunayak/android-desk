#!/bin/sh
# Publishes a release: builds the app, tags the commit it was built from,
# creates the GitHub release, and attaches the disk image.
#
#   ./release.sh             publish the version in desk-gui/mygo.json
#   ./release.sh --dry-run   check everything, publish nothing
#
# `./build.sh --release` and `make release` are the same thing.
#
# It refuses to run on a dirty tree or an unpushed commit, because the app
# records the revision it was built from: a release has to be able to say which
# source it came from, and to be the same thing anyone else gets from that tag.
# It follows that a version is published once — bump `version` in
# desk-gui/mygo.json to release again. Re-running is safe: a tag already made,
# or a release already created, is picked up rather than made twice.
#
# The GitHub credential is the one git already uses for this repository. The
# token is read out of the keychain through `git credential fill`, never written
# to disk, and never put in a command line where `ps` could read it.

set -eu
cd "$(dirname "$0")"

dry=no
for arg in "$@"; do
	case "$arg" in
	--dry-run) dry=yes ;;
	-h | --help)
		sed -n '2,/^$/p' "$0" | sed -e 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "release: unknown argument $arg" >&2
		echo "         ./release.sh --help" >&2
		exit 2
		;;
	esac
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

die() {
	echo "release: $*" >&2
	exit 1
}

json_field() { # file, key
	python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],""))' "$1" "$2"
}

# ------------------------------------------------------------ preconditions

version="$(python3 -c 'import json;print(json.load(open("desk-gui/mygo.json"))["version"])' 2>/dev/null)" ||
	die "could not read \"version\" from desk-gui/mygo.json"
tag="v$version"
echo "==> Desk $version"

dirty="$(git status --porcelain)"
if [ -n "$dirty" ]; then
	printf '%s\n' "$dirty" | sed 's/^/    /' >&2
	die "the working tree has changes. Commit them first: the app records the
       revision it was built from, and a release has to point at a revision
       everyone can get."
fi

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" != HEAD ] || die "this is a detached HEAD. Check out a branch first."
git fetch --quiet origin "$branch" || die "could not fetch $branch from origin"
head="$(git rev-parse HEAD)"
if [ "$head" != "$(git rev-parse FETCH_HEAD)" ]; then
	die "$branch is ahead of origin/$branch (or does not match it).
       Push it first — the tag has to point at something GitHub has."
fi

slug="$(git remote get-url origin | sed -e 's|^.*github\.com[:/]||' -e 's|\.git$||')"
[ -n "$slug" ] || die "could not tell which repository origin is"
echo "    $head on $branch, clean"
echo "    repository $slug"

# The credential git would use to push here: the keychain, not a file.
cred="$(printf 'protocol=https\nhost=github.com\n\n' | GIT_TERMINAL_PROMPT=0 git credential fill 2>/dev/null || true)"
token="$(printf '%s\n' "$cred" | sed -n 's/^password=//p')"
[ -n "$token" ] || die "no GitHub credential for github.com. Push once by hand and
       let git save it to the keychain, then run this again."

# curl reads its options from stdin, so the token stays out of `ps`.
api() {
	printf 'header = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.github+json"\n' \
		"$token" | curl -sS --config - "$@"
}

code="$(api -o "$tmp/me.json" -w '%{http_code}' https://api.github.com/user)"
[ "$code" = 200 ] || die "GitHub refused the stored credential (http $code)."
echo "    publishing as $(json_field "$tmp/me.json" login)"

# Where things stand: the tag, and whether it is released already.
tag_remote="$(git ls-remote --tags origin "refs/tags/$tag" | cut -f1)"
if [ -n "$tag_remote" ] && [ "$tag_remote" != "$head" ]; then
	die "$tag already exists, on $tag_remote, and this is $head.
       A released version is published once: bump \"version\" in
       desk-gui/mygo.json, or delete that tag and release if it was a mistake."
fi

code="$(api -o "$tmp/release.json" -w '%{http_code}' "https://api.github.com/repos/$slug/releases/tags/$tag")"
case "$code" in
200) released=yes ;;
404) released=no ;;
*) die "GitHub answered $code asking about $tag: $(json_field "$tmp/release.json" message)" ;;
esac

if [ "$released" = yes ]; then
	echo "==> $tag is already released: $(json_field "$tmp/release.json" html_url)"
	echo "    Bump \"version\" in desk-gui/mygo.json to publish something new."
	exit 0
fi
if [ -n "$tag_remote" ]; then
	echo "    $tag exists, unpublished: picking up where the last run stopped"
fi

# The disk image mygo writes, and the name it gets on GitHub.
dmg="$(ls -t desk-gui/build/*/*"$version"*.dmg 2>/dev/null | head -1 || true)"
[ -n "$dmg" ] && [ "$dry" = no ] || dmg=""
if [ "$dry" = yes ]; then
	echo "==> would build, tag $tag, and publish"
	echo "    image    Desk-$version-<arch>.dmg, built from the sources above"
	echo "    release  $tag on $slug"
	echo
	echo "Nothing was published. Run ./release.sh to do it."
	exit 0
fi

# --------------------------------------------------------------- the build

echo "==> building $tag"
./build.sh --install --app

app="$(ls -d desk-gui/build/*/Desk.app 2>/dev/null | head -1)"
[ -n "$app" ] || die "no Desk.app was built"
binary="$app/Contents/MacOS/Desk"
built="$(grep -ao 'vcs\.revision=[0-9a-f]*' "$binary" | head -1 | cut -d= -f2)"
modified="$(grep -ao 'vcs\.modified=[a-z]*' "$binary" | head -1 | cut -d= -f2)"
[ "$built" = "$head" ] || die "the app was built from $built, not $head: refusing to
       publish something else than this commit."
[ "$modified" = false ] || die "the app was built from a modified tree: refusing to publish."
echo "    built from $built, unmodified"

dmg="$(ls -t desk-gui/build/*/*"$version"*.dmg 2>/dev/null | head -1 || true)"
[ -n "$dmg" ] || die "the build wrote no disk image for $version"
asset="Desk-$version-$(basename "$dmg" | sed -e 's/\.dmg$//' -e 's/.* //').dmg"
hdiutil verify "$dmg" >/dev/null 2>&1 || die "$dmg does not verify as a disk image"
echo "    $dmg"
echo "    $asset, $(wc -c <"$dmg" | tr -d ' ') bytes"

# ---------------------------------------------------------------- the tag

if [ -z "$tag_remote" ]; then
	echo "==> tagging $tag"
	git tag -a "$tag" -m "Desk $version"
	git push --quiet origin "$tag" ||
		die "could not push the tag. If it is only local, drop it with
       \`git tag -d $tag\` and run this again."
else
	echo "==> $tag already points at $head"
fi

# ------------------------------------------------------------- the release

python3 - "$version" "$asset" >"$tmp/body.json" <<'PY'
import json, sys

version, asset = sys.argv[1], sys.argv[2]
body = """A desktop mode for Android phones that don't have one, on a Mac.

**Install**: download `{asset}` below, open it, and drag **Desk** onto
Applications. Nothing else is needed — `adb` and `scrcpy` are inside the app.
The build is ad-hoc signed, so the first launch wants a right-click → *Open*
once; Gatekeeper refuses the double-click.

**Requirements**: an Apple Silicon Mac (this build is arm64 only), and a phone on
Android 10+ that reports `android.software.freeform_window_management`, with USB
debugging enabled.

**What it does**: asks scrcpy for a second, 1920x1080 display on the phone, puts
the phone's own launcher on it, and starts apps there as resizable freeform
windows — a desktop the phone never had. The bar that comes up adds the
navigation buttons the virtual display lacks, an app drawer over the apps
installed on the phone, screenshots, and the display's resolution, density and
video bit rate.

**What does not work, and why**: Motorola Ready For / Smart Connect cannot be
started from a Mac. Its entry point sits behind
`com.motorola.permission.ACCESS_DESKTOP` (`signature|privileged`), Motorola
disables the desktop components, and macOS cannot be a Miracast sink — Apple
implements AirPlay only, and Macs have no Wi-Fi Direct. All three are written up
in the README.

Instructions, screenshots and the `desk` CLI that does the same thing from a
script: https://github.com/jitunayak/android-desk#readme

Free software under the GPL-3.0: use it, change it, pass it on — and whatever is
built from it stays free too.
""".format(asset=asset)

json.dump({"tag_name": "v" + version, "name": "Desk " + version,
           "body": body, "draft": False, "prerelease": False},
          sys.stdout)
PY

echo "==> creating the release"
code="$(api -X POST -H "Content-Type: application/json" --data-binary @"$tmp/body.json" \
	-o "$tmp/created.json" -w '%{http_code}' "https://api.github.com/repos/$slug/releases")"
case "$code" in
201) ;;
422)
	# It is there after all: another run got in first, or the tag was made by hand.
	die "GitHub says a release for $tag already exists: $(json_field "$tmp/created.json" message)"
	;;
*) die "GitHub answered $code creating the release: $(json_field "$tmp/created.json" message)" ;;
esac
id="$(json_field "$tmp/created.json" id)"

echo "==> attaching $asset"
code="$(api -X POST -H "Content-Type: application/octet-stream" --data-binary @"$dmg" \
	-o "$tmp/asset.json" -w '%{http_code}' \
	"https://uploads.github.com/repos/$slug/releases/$id/assets?name=$asset")"
case "$code" in
201) ;;
*) die "GitHub answered $code uploading the disk image: $(json_field "$tmp/asset.json" message)" ;;
esac

# ------------------------------------------------------------- the check

url="$(json_field "$tmp/asset.json" browser_download_url)"
uploaded="$(json_field "$tmp/asset.json" size)"
local="$(wc -c <"$dmg" | tr -d ' ')"
[ "$uploaded" = "$local" ] || die "the uploaded image is $uploaded bytes, the local one $local"
code="$(curl -sIL -o /dev/null -w '%{http_code}' "$url")"
[ "$code" = 200 ] || die "the uploaded image does not download (http $code)"

echo
echo "Done.  Desk $version"
echo "  release: $(json_field "$tmp/created.json" html_url)"
echo "  image:   $asset, $uploaded bytes, downloads as http 200"
echo "  app:     /Applications/Desk.app, built from $built"
