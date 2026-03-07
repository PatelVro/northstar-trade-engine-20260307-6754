param(
    [string]$ConfigFile = "config_ibkr.json"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$runtimeDir = Join-Path $repoRoot "runtime"
if (-not (Test-Path $runtimeDir)) {
    New-Item -ItemType Directory -Path $runtimeDir | Out-Null
}

$pidFile = Join-Path $runtimeDir "ibkr_paper.pid"
$outLog = Join-Path $runtimeDir "ibkr_paper.out.log"
$errLog = Join-Path $runtimeDir "ibkr_paper.err.log"
$binaryPath = Join-Path $runtimeDir "AegisTrade_ibkr_paper.exe"

if (Test-Path $pidFile) {
    $pidText = (Get-Content -Path $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
    if ($pidText -and $pidText -match "^\d+$") {
        $existing = Get-Process -Id ([int]$pidText) -ErrorAction SilentlyContinue
        if ($existing) {
            Write-Output "IBKR paper is already running (PID $pidText)."
            exit 0
        }
    }
    Remove-Item -Path $pidFile -Force -ErrorAction SilentlyContinue
}

$universeFile = Join-Path $repoRoot "data\\universe\\us_companies.txt"
if (-not (Test-Path $universeFile)) {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $repoRoot "tools\\gen_us_equity_universe.ps1")
}

& go build -o $binaryPath main.go

$proc = Start-Process -FilePath $binaryPath `
    -ArgumentList @($ConfigFile) `
    -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $outLog `
    -RedirectStandardError $errLog `
    -PassThru `
    -WindowStyle Hidden

Set-Content -Path $pidFile -Value $proc.Id -Encoding ascii
Write-Output "Started IBKR paper (PID $($proc.Id))."
