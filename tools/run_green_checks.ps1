[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

try {
    Write-Host "Running deterministic Go green checks from $repoRoot"
    & go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    Write-Host "Go green checks passed."
}
finally {
    Pop-Location
}
