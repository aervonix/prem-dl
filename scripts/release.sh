#!/usr/bin/env bash
# Builds the Linux release files into build/release, both with yt-dlp and ffmpeg built in:
#   Prem-dl-x86_64.AppImage       runs on its own
#   prem-dl_<version>_amd64.deb   installs on Debian and Ubuntu
# plus SHA256SUMS-linux.txt. Windows' SHA256SUMS.txt (release.ps1) stays separate: its updater
# reads that one. Both need the system's WebKitGTK 4.1 (Ubuntu 22.04+, Debian 12+).
# The app runs only the tools it carries, so yt-dlp changes with releases. Needs Go, Node, the
# Wails CLI and dpkg-deb; appimagetool is downloaded into build/tools the first time.
#
#   scripts/release.sh               # fetches the newest yt-dlp and ffmpeg first
#   scripts/release.sh --skip-fetch  # uses what bundle/ already holds
#   scripts/release.sh --no-updates  # a build without the build secret: it never checks for updates
#
# The build carries Prem-dl's build secret for the release server (internal/selfupdate), read
# with the TrueRip API repo's scripts/project.py ($TRUERIP_API, default ~/Coding/API) and
# written into a gitignored file for the build only. Publish the AppImage on the "linux"
# channel afterwards (see CLAUDE.md, "Releases").
set -euo pipefail
umask 022 # package files must not be group-writable, whatever this machine uses

skip_fetch=false
no_updates=false
for arg in "$@"; do
  case "$arg" in
    --skip-fetch) skip_fetch=true ;;
    --no-updates) no_updates=true ;;
    *) echo "usage: $0 [--skip-fetch] [--no-updates]" >&2; exit 2 ;;
  esac
done

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
export PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

step() { printf '\033[36m==> %s\033[0m\n' "$*"; }
fail() { printf 'release: %s\n' "$*" >&2; exit 1; }

for tool in go npm wails dpkg-deb sha256sum curl; do
  command -v "$tool" >/dev/null || fail "$tool isn't installed or isn't on PATH"
done

bin="$root/build/bin/Prem-dl"
# wails build can't replace a running binary; never stop the user's copy, ask instead.
for pid in $(pgrep -x Prem-dl || true); do
  if [ "$(readlink "/proc/$pid/exe" 2>/dev/null)" = "$bin" ]; then
    fail "build/bin/Prem-dl is running. Quit it (tray icon > Quit Prem-dl) and run this again."
  fi
done

version=$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' wails.json)
app_version=$(sed -n 's/^const appVersion = "\([^"]*\)"/\1/p' app.go)
[ -n "$version" ] && [ "$version" = "$app_version" ] || fail "wails.json says '$version' but app.go says '$app_version'; make them match"
commit=$(git rev-parse --short HEAD)
if [ -n "$(git status --porcelain)" ]; then
  echo "warning: the working tree has uncommitted changes; the build won't match commit $commit." >&2
fi

out="$root/build/release"
mkdir -p "$out"
# Only this script's files: release-windows.sh puts Windows' beside them.
rm -f "$out"/prem-dl_*_amd64.deb "$out"/Prem-dl-x86_64.AppImage "$out"/SHA256SUMS-linux.txt
stage=$(mktemp -d)
secret_file="$root/internal/selfupdate/secret_release.go"
trap 'rm -rf "$stage"; rm -f "$secret_file"' EXIT

if ! $skip_fetch || [ ! -f bundle/yt-dlp.zip ]; then
  step "Fetching yt-dlp and ffmpeg"
  go run ./cmd/bundle
fi
[ -f bundle/yt-dlp.zip ] || fail "bundle/yt-dlp.zip is missing. Run without --skip-fetch."
[ -f bundle/ffmpeg.zip ] || fail "bundle/ffmpeg.zip is missing; releases must carry ffmpeg. Run without --skip-fetch."
# Also fails when bundle/ holds another platform's tools (fetched on Windows): they don't run here.
step "Checking the built-in tools unpack"
go test -tags bundled . -run BuiltIn -count=1
if $no_updates; then
  echo "warning: --no-updates: this build never checks for updates." >&2
else
  api=${TRUERIP_API:-$HOME/Coding/API}
  [ -f "$api/scripts/project.py" ] || fail "the TrueRip API repo isn't at $api (set TRUERIP_API), so the build can't get its build secret. --no-updates builds without it."
  step "Writing the build secret"
  (cd "$api" && uv run --project backend python scripts/project.py secret-file prem-dl --format raw --out -) | go run ./cmd/buildsecret
fi
# One binary per package: each carries its update target (internal/selfupdate), since a .deb and
# an AppImage update differently. The second build reuses the first's frontend.
build_for() {
  step "Building Prem-dl for $1"
  # -trimpath keeps this machine's folders (and the user name in them) out of the binary.
  wails build -tags "webkit2_41 bundled" -trimpath $2 -ldflags "-X yt-dlp-gui/internal/selfupdate.buildTarget=$1"
  [ -x "$bin" ] || fail "wails build made no build/bin/Prem-dl"
  # The secret is in there XORed: its plain form must not be.
  # (The updater's own pattern for a secret, trb_[0-9A-Za-z]{12}_..., is in there too: look for a real one.)
  if grep -aqE 'trb_[0-9A-Za-z]{12}_' "$bin"; then fail "the build secret is readable in build/bin/Prem-dl"; fi
  # (Its name is in every build as a constant; the build info says which one this build was given.)
  go version -m "$bin" | grep -q "selfupdate.buildTarget=$1" || fail "the $1 build doesn't carry its target"
  cp "$bin" "$stage/prem-dl-$1"
}
build_for linux-x64-deb ""
build_for linux-x64-appimage -s
rm -f "$secret_file"

# The desktop entry both packages carry. The .deb's Exec must read exactly as the app writes it
# for /usr/bin/prem-dl (sys.SchemeCommand), or the app adds a user entry of its own.
desktop_entry() {
  cat <<EOF
[Desktop Entry]
Type=Application
Name=Prem-dl
Comment=Download video and audio
Exec=$1 %u
Icon=prem-dl
Terminal=false
Categories=AudioVideo;Video;
MimeType=x-scheme-handler/prem-dl;
StartupWMClass=prem-dl
EOF
}
validate() {
  if command -v desktop-file-validate >/dev/null; then desktop-file-validate "$1"; fi
}

step "Packing prem-dl_${version}_amd64.deb"
deb="$stage/deb"
install -Dm755 "$stage/prem-dl-linux-x64-deb" "$deb/usr/bin/prem-dl"
mkdir -p "$deb/usr/share/applications"
desktop_entry /usr/bin/prem-dl >"$deb/usr/share/applications/prem-dl.desktop"
validate "$deb/usr/share/applications/prem-dl.desktop"
XDG_DATA_HOME="$deb/usr/share" go run scripts/icons.go
install -Dm644 LICENSE "$deb/usr/share/doc/prem-dl/copyright"
mkdir -p "$deb/DEBIAN"
# dpkg's own triggers refresh the desktop and icon caches; no maintainer scripts needed.
cat >"$deb/DEBIAN/control" <<EOF
Package: prem-dl
Version: $version
Section: video
Priority: optional
Architecture: amd64
Maintainer: aervonix <348173904+aervonix@users.noreply.github.com>
Depends: libc6 (>= 2.34), libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0, xdg-utils
Installed-Size: $(du -sk --exclude=DEBIAN "$deb" | cut -f1)
Homepage: https://github.com/aervonix/prem-dl
Description: Download video and audio with yt-dlp
 Paste a link, check what you're about to get, pick a quality and download.
 Handles playlists, a queue, cookies and subtitles. yt-dlp and ffmpeg are
 built in, at the versions this release was tested with.
EOF
dpkg-deb --root-owner-group -Zxz --build "$deb" "$out/prem-dl_${version}_amd64.deb" >/dev/null

step "Packing Prem-dl-x86_64.AppImage"
tools="$root/build/tools"
appimagetool="$tools/appimagetool-x86_64.AppImage"
if [ ! -x "$appimagetool" ]; then
  mkdir -p "$tools"
  curl -fsSL -o "$appimagetool.part" https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage
  chmod +x "$appimagetool.part"
  mv "$appimagetool.part" "$appimagetool"
fi
echo "appimagetool $(sha256sum "$appimagetool" | cut -c1-16)…"
app="$stage/Prem-dl.AppDir"
install -Dm755 "$stage/prem-dl-linux-x64-appimage" "$app/usr/bin/prem-dl"
ln -s usr/bin/prem-dl "$app/AppRun"
desktop_entry prem-dl >"$app/prem-dl.desktop"
validate "$app/prem-dl.desktop"
XDG_DATA_HOME="$app/usr/share" go run scripts/icons.go
cp "$app/usr/share/icons/hicolor/256x256/apps/prem-dl.png" "$app/prem-dl.png"
ln -s prem-dl.png "$app/.DirIcon"
# The tool is an AppImage itself: extracting it runs it without FUSE.
APPIMAGE_EXTRACT_AND_RUN=1 ARCH=x86_64 "$appimagetool" --no-appstream "$app" "$out/Prem-dl-x86_64.AppImage" >"$stage/appimagetool.log" 2>&1 ||
  { cat "$stage/appimagetool.log" >&2; fail "appimagetool failed"; }

(cd "$out" && sha256sum Prem-dl-x86_64.AppImage "prem-dl_${version}_amd64.deb" >SHA256SUMS-linux.txt)

echo
printf '\033[32mPrem-dl %s from commit %s, yt-dlp %s built in:\033[0m\n' "$version" "$commit" "$(tr -d '[:space:]' <bundle/yt-dlp.version)"
for f in "$out"/*; do stat -c '%n %s' "$f"; done | awk '{n = split($1, p, "/"); printf "  %-32s %7.1f MB\n", p[n], $2 / 1e6}'
echo
sed 's/^/  /' "$out/SHA256SUMS-linux.txt"
