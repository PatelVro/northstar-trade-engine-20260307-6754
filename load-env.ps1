# load-env.ps1 — Load variables from .env into the current PowerShell session.
#
# .env is gitignored. Put your secrets in .env, then dot-source this script
# before running northstar.exe, aster-preflight.exe, or any other tool that
# reads NORTHSTAR_* environment variables:
#
#   . .\load-env.ps1
#   .\aster-preflight.exe
#
# The dot-source (leading ". ") is important — it runs in the current scope
# so environment variables persist after the script returns.

$envPath = Join-Path $PSScriptRoot ".env"
if (-not (Test-Path $envPath)) {
    Write-Host "No .env file at $envPath" -ForegroundColor Yellow
    Write-Host "Create one with NORTHSTAR_* values (see .env.example)" -ForegroundColor Yellow
    return
}

$loaded = 0
Get-Content $envPath | ForEach-Object {
    # Skip blank lines and comments; parse KEY=VALUE
    if ($_ -match '^\s*#') { return }
    if ($_ -match '^\s*$') { return }
    if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
        $key = $Matches[1]
        $value = $Matches[2]
        # Strip optional surrounding quotes
        if ($value -match '^"(.*)"$' -or $value -match "^'(.*)'$") {
            $value = $Matches[1]
        }
        [System.Environment]::SetEnvironmentVariable($key, $value, 'Process')
        $loaded++
    }
}

Write-Host "Loaded $loaded environment variables from .env" -ForegroundColor Green
