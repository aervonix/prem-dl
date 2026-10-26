# Builds the two release files into build\release:
#   Prem-dl.exe        the app on its own; it downloads yt-dlp (and offers ffmpeg) on first start
#   Prem-dl-Setup.exe  an installer for the current user, with yt-dlp and ffmpeg built in
# plus SHA256SUMS.txt. Needs Go, Node, the Wails CLI and NSIS (winget install NSIS.NSIS).
#
#   .\scripts\release.ps1             # fetches the newest yt-dlp and ffmpeg first
#   .\scripts\release.ps1 -SkipFetch  # uses what bundle\ already holds
param([switch]$SkipFetch)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# New shells don't see tools installed since they started (CLAUDE.md).
$env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' +
    [Environment]::GetEnvironmentVariable('Path', 'User') + ';' +
    "$env:USERPROFILE\go\bin;${env:ProgramFiles(x86)}\NSIS"

function Run([string]$what, [scriptblock]$cmd) {
    Write-Host "==> $what" -ForegroundColor Cyan
    & $cmd
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

foreach ($tool in 'go', 'npm', 'wails', 'makensis') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool isn't installed or isn't on PATH" }
}
$bin = Join-Path $root 'build\bin'
$exe = Join-Path $bin 'Prem-dl.exe'
# wails build can't replace a running exe; never stop the user's copy, ask instead.
if (Get-Process -Name 'Prem-dl' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe }) {
    throw "build\bin\Prem-dl.exe is running. Quit it (tray icon > Quit Prem-dl) and run this again."
}

$version = (Get-Content wails.json -Raw | ConvertFrom-Json).info.productVersion
$appVersion = [regex]::Match((Get-Content app.go -Raw), 'appVersion = "([^"]+)"').Groups[1].Value
if ($version -ne $appVersion) { throw "wails.json says $version but app.go says $appVersion; make them match" }
$commit = git rev-parse --short HEAD
if (git status --porcelain) { Write-Warning "The working tree has uncommitted changes; the build won't match commit $commit." }

$out = Join-Path $root 'build\release'
if (Test-Path $out) { Remove-Item $out -Recurse -Force }
New-Item -ItemType Directory $out | Out-Null

Run 'Building Prem-dl.exe' { wails build }
Copy-Item $exe (Join-Path $out 'Prem-dl.exe')

if (-not $SkipFetch -or -not (Test-Path 'bundle\yt-dlp.exe')) {
    Run 'Fetching yt-dlp and ffmpeg' { go run ./cmd/bundle }
}
if (-not (Test-Path 'bundle\ffmpeg.zip')) { throw "bundle\ffmpeg.zip is missing; the installer must carry ffmpeg. Run without -SkipFetch." }
Run 'Checking the built-in tools unpack' { go test -tags bundled . -run BuiltIn -count=1 }
Run 'Building Prem-dl-Setup.exe' { wails build -tags bundled -nsis }
Copy-Item (Join-Path $bin 'Prem-dl-Setup.exe') $out
# wails fills in its template with this build's values; the template is what's committed.
git checkout -- build/windows/installer/wails_tools.nsh

# The bundled build replaced build\bin\Prem-dl.exe; leave the plain one there, as after `wails build`.
Copy-Item (Join-Path $out 'Prem-dl.exe') $exe -Force

$sums = Get-ChildItem $out -Filter *.exe | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
$sums | Set-Content (Join-Path $out 'SHA256SUMS.txt') -Encoding ascii

Write-Host ''
Write-Host "Prem-dl $version from commit $commit, yt-dlp $((Get-Content bundle\yt-dlp.version).Trim()) built in:" -ForegroundColor Green
Get-ChildItem $out | ForEach-Object { '  {0,-20} {1,7:N1} MB' -f $_.Name, ($_.Length / 1MB) }
Write-Host ''
$sums | ForEach-Object { "  $_" }
