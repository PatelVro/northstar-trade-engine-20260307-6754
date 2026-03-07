Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$pidFile = Join-Path $repoRoot "runtime\\ibkr_paper.pid"
$stopped = $false

if (Test-Path $pidFile) {
    $pidText = (Get-Content -Path $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
    if ($pidText -and $pidText -match "^\d+$") {
        $targetPid = [int]$pidText
        $proc = Get-Process -Id $targetPid -ErrorAction SilentlyContinue
        if ($proc) {
            Stop-Process -Id $targetPid -Force
            $stopped = $true
            Write-Output "Stopped IBKR paper process (PID $targetPid)."
        }
    }
    Remove-Item -Path $pidFile -Force -ErrorAction SilentlyContinue
}

$candidates = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
    Where-Object {
        ($_.Name -in @("go.exe", "main.exe") -and $_.CommandLine -match "config_ibkr\.json") -or
        ($_.Name -eq "AegisTrade_ibkr_paper.exe")
    }

foreach ($p in $candidates) {
    try {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction Stop
        $stopped = $true
        Write-Output "Stopped fallback candidate PID $($p.ProcessId)."
    } catch {
        Write-Output "Failed to stop PID $($p.ProcessId): $($_.Exception.Message)"
    }
}

if (-not $stopped) {
    Write-Output "No IBKR paper process found."
}
