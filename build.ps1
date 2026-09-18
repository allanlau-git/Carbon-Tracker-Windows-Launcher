param(
    [string]$Version = '4.4.0'
)

$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dist = Join-Path $Root 'dist'
$Unsigned = Join-Path $Dist 'CarbonTrackerLauncher-unsigned.exe'
$Output = Join-Path $Dist ("CarbonTrackerLauncher-v{0}.exe" -f $Version)

New-Item -ItemType Directory -Force -Path $Dist | Out-Null

$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

go build -trimpath -ldflags '-H=windowsgui -s -w -buildid=' -o $Unsigned ./cmd/carbon-tracker-launcher
if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }

python ./tools/embed_icon.py $Unsigned ./assets/Carbon-Tracker.ico -o $Output
if ($LASTEXITCODE -ne 0) { throw 'Icon embedding failed.' }

Remove-Item $Unsigned -Force

Write-Host "Built: $Output"
Get-FileHash $Output -Algorithm SHA256
