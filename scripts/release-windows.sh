#!/usr/bin/env bash
# Builds the Windows release files on Linux into build/release, both with yt-dlp and ffmpeg built in:
#   Prem-dl.exe        the app on its own (portable)
#   Prem-dl-Setup.exe  an installer for the current user
# plus SHA256SUMS.txt, which is what release.ps1 makes on Windows. Wails' Windows code is pure Go,
# so the exe cross-compiles; NSIS (sudo apt install nsis) makes the installer.
#
#   scripts/release-windows.sh               # fetches the newest Windows yt-dlp and ffmpeg first
#   scripts/release-windows.sh --skip-fetch  # uses what build/bundle-windows already holds
#   scripts/release-windows.sh --no-updates  # a build without the build secret: it never checks for updates
#
# Windows' tools are fetched into build/bundle-windows (cmd/bundle -os windows) and swapped into
# bundle/ for the build only; Linux's tools are put back afterwards. They can't be run here, so
# the built-in-tools test that release.ps1 runs is left to a Windows machine: start the new
# Prem-dl.exe there once before publishing. The build secret is read like release.sh does.
# Publish it with the TrueRip API repo's scripts/project.py: one release on "stable" with a build
# per target (windows-x64=Prem-dl-Setup.exe, linux-x64-appimage=..., linux-x64-deb=...).
set -euo pipefail
umask 022

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
fail() { printf 'release-windows: %s\n' "$*" >&2; exit 1; }

for tool in go npm wails makensis sha256sum; do
  command -v "$tool" >/dev/null || fail "$tool isn't installed or isn't on PATH (NSIS: sudo apt install nsis)"
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
rm -f "$out/Prem-dl.exe" "$out/Prem-dl-Setup.exe" "$out/SHA256SUMS.txt"

win="$root/build/bundle-windows"
if ! $skip_fetch || [ ! -f "$win/yt-dlp.zip" ]; then
  step "Fetching Windows' yt-dlp and ffmpeg"
  go run ./cmd/bundle -os windows -dir "$win"
fi
[ -f "$win/yt-dlp.zip" ] && [ -f "$win/ffmpeg.zip" ] || fail "build/bundle-windows is incomplete. Run without --skip-fetch."

# bundle/ is what the build embeds: Windows' tools go in for this build, and Linux's come back.
host="$root/build/bundle-host"
secret_file="$root/internal/selfupdate/secret_release.go"
target_file="$root/internal/selfupdate/target_release.go"
restore() {
  rm -f "$secret_file" "$target_file"
  if [ -d "$host" ]; then
    rm -rf "$root/bundle"
    mv "$host" "$root/bundle"
  fi
  git checkout -- build/windows/installer/wails_tools.nsh 2>/dev/null || true
}
trap restore EXIT
[ ! -e "$host" ] || fail "build/bundle-host is left from an earlier run: move it back to bundle/ (it holds this machine's tools) and run this again"
if [ -d "$root/bundle" ]; then
  mv "$root/bundle" "$host"
fi
mkdir -p "$root/bundle"
cp "$win/yt-dlp.zip" "$win/yt-dlp.version" "$win/ffmpeg.zip" "$root/bundle/"

if $no_updates; then
  echo "warning: --no-updates: this build never checks for updates." >&2
else
  api=${TRUERIP_API:-$HOME/Coding/API}
  [ -f "$api/scripts/project.py" ] || fail "the TrueRip API repo isn't at $api (set TRUERIP_API), so the build can't get its build secret. --no-updates builds without it."
  step "Writing the build secret"
  (cd "$api" && uv run --project backend python scripts/project.py secret-file prem-dl --format raw --out -) | go run ./cmd/buildsecret
  # The update target (internal/selfupdate): the installer is what Windows copies update to.
  go run ./cmd/buildsecret -target windows-x64
fi

step "Building Prem-dl.exe and Prem-dl-Setup.exe"
# -trimpath keeps this machine's folders (and the user name in them) out of the exe.
wails build -platform windows/amd64 -tags bundled -trimpath -nsis
restore
trap - EXIT

exe="$root/build/bin/Prem-dl.exe"
setup="$root/build/bin/Prem-dl-Setup.exe"
[ -f "$exe" ] && [ -f "$setup" ] || fail "wails build made no Prem-dl.exe and Prem-dl-Setup.exe in build/bin"
# The secret is in there XORed: its plain form must not be.
# (The updater's own pattern for a secret, trb_[0-9A-Za-z]{12}_..., is in there too: look for a real one.)
if grep -aqE 'trb_[0-9A-Za-z]{12}_' "$exe"; then fail "the build secret is readable in Prem-dl.exe"; fi
cp "$exe" "$setup" "$out/"
(cd "$out" && sha256sum Prem-dl.exe Prem-dl-Setup.exe > SHA256SUMS.txt)

echo
printf '\033[32mPrem-dl %s for Windows, from commit %s, yt-dlp %s built in:\033[0m\n' "$version" "$commit" "$(tr -d '[:space:]' < "$win/yt-dlp.version")"
(cd "$out" && for f in Prem-dl.exe Prem-dl-Setup.exe; do stat -c '%n %s' "$f"; done | awk '{printf "  %-20s %7.1f MB\n", $1, $2 / 1048576}')
echo
sed 's/^/  /' "$out/SHA256SUMS.txt"
