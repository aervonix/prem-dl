# Builds the two release files into build\release, both with yt-dlp and ffmpeg built in:
#   Prem-dl.exe        the app on its own (portable)
#   Prem-dl-Setup.exe  an installer for the current user
# plus SHA256SUMS.txt. The app runs only the tools it carries, so yt-dlp changes with releases. Needs Go, Node, the Wails CLI and NSIS (winget install NSIS.NSIS).
#
#   .\scripts\release.ps1             # fetches the newest yt-dlp and ffmpeg first
#   .\scripts\release.ps1 -SkipFetch  # uses what bundle\ already holds
#   .\scripts\release.ps1 -NoUpdates  # a build without the build secret: it never checks for updates
#
# The build carries Prem-dl's build secret for the release server (internal\selfupdate): from
# $env:PREMDL_BUILD_SECRET when set, else read with the TrueRip API repo's scripts\project.py
# ($env:TRUERIP_API, default ~\Coding\API). It goes into a gitignored file for the build only.
# Publish Prem-dl-Setup.exe on the "stable" channel afterwards (see CLAUDE.md, "Releases").
param([switch]$SkipFetch, [switch]$NoUpdates)

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

if (-not $SkipFetch -or -not (Test-Path 'bundle\yt-dlp.zip')) {
    Run 'Fetching yt-dlp and ffmpeg' { go run ./cmd/bundle }
}
if (-not (Test-Path 'bundle\ffmpeg.zip')) { throw "bundle\ffmpeg.zip is missing; releases must carry ffmpeg. Run without -SkipFetch." }
# Also fails when bundle\ holds another platform's tools (fetched on Linux): they don't run here.
Run 'Checking the built-in tools unpack' { go test -tags bundled . -run BuiltIn -count=1 }
$secretFile = Join-Path $root 'internal\selfupdate\secret_release.go'
try {
    if ($NoUpdates) {
        Write-Warning '-NoUpdates: this build never checks for updates.'
    } elseif ($env:PREMDL_BUILD_SECRET) {
        Run 'Writing the build secret' { $env:PREMDL_BUILD_SECRET | go run ./cmd/buildsecret }
    } else {
        $api = if ($env:TRUERIP_API) { $env:TRUERIP_API } else { Join-Path $HOME 'Coding\API' }
        if (-not (Test-Path (Join-Path $api 'scripts\project.py'))) {
            throw "The TrueRip API repo isn't at $api (set TRUERIP_API, or PREMDL_BUILD_SECRET), so the build can't get its build secret. -NoUpdates builds without it."
        }
        Run 'Writing the build secret' {
            Push-Location $api
            try { $s = uv run --project backend python scripts/project.py secret-file prem-dl --format raw --out - } finally { Pop-Location }
            if ($LASTEXITCODE -ne 0) { throw 'project.py secret-file failed' }
            $s | go run ./cmd/buildsecret
        }
    }
    # -trimpath keeps this PC's folders (and the user name in them) out of the exe.
    Run 'Building Prem-dl.exe and Prem-dl-Setup.exe' { wails build -trimpath -tags bundled -nsis }
} finally {
    Remove-Item $secretFile -ErrorAction SilentlyContinue
}
# The secret is in there XORed: its plain form must not be.
# (The updater's own pattern for a secret, trb_[0-9A-Za-z]{12}_..., is in there too: look for a real one.)
if (Select-String -Path $exe -Pattern 'trb_[0-9A-Za-z]{12}_' -CaseSensitive -Quiet) { throw 'The build secret is readable in build\bin\Prem-dl.exe' }
Copy-Item $exe, (Join-Path $bin 'Prem-dl-Setup.exe') $out
# wails fills in its template with this build's values; the template is what's committed.
git checkout -- build/windows/installer/wails_tools.nsh

$sums = Get-ChildItem $out -Filter *.exe | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
$sums | Set-Content (Join-Path $out 'SHA256SUMS.txt') -Encoding ascii

Write-Host ''
Write-Host "Prem-dl $version from commit $commit, yt-dlp $((Get-Content bundle\yt-dlp.version).Trim()) built in:" -ForegroundColor Green
Get-ChildItem $out | ForEach-Object { '  {0,-20} {1,7:N1} MB' -f $_.Name, ($_.Length / 1MB) }
Write-Host ''
$sums | ForEach-Object { "  $_" }
