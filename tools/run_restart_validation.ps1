[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

try {
    Write-Host "Running restart/interruption validation harness from $repoRoot"
    & go test -count=1 -v ./trader -run '^TestRestartInterruptionValidationHarness$'
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    Write-Host "Restart/interruption validation harness passed."
}
finally {
    Pop-Location
}
