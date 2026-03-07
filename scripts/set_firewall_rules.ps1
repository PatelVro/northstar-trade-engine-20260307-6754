Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$exePath = Join-Path $repoRoot "runtime\AegisTrade_ibkr_paper.exe"

if (-not (Test-Path $exePath)) {
    Write-Error "Runner binary not found at $exePath"
    exit 1
}

$rules = @(
    @{
        Name = "AegisTrade_IBKR_PAPER_EXE_TCP_IN_ANY"
        Args = @(
            "dir=in",
            "action=allow",
            "enable=yes",
            "profile=any",
            "protocol=TCP",
            "localport=8080",
            "program=""$exePath"""
        )
    },
    @{
        Name = "AegisTrade_IBKR_PAPER_EXE_UDP_IN_ANY"
        Args = @(
            "dir=in",
            "action=allow",
            "enable=yes",
            "profile=any",
            "protocol=UDP",
            "program=""$exePath"""
        )
    },
    @{
        Name = "AegisTrade_IBKR_PORT_8080_TCP_IN_ANY"
        Args = @(
            "dir=in",
            "action=allow",
            "enable=yes",
            "profile=any",
            "protocol=TCP",
            "localport=8080"
        )
    }
)

foreach ($rule in $rules) {
    # Make reruns idempotent.
    & netsh advfirewall firewall delete rule name="$($rule.Name)" | Out-Null
    & netsh advfirewall firewall add rule name="$($rule.Name)" $rule.Args | Out-Null
    Write-Output "Applied firewall rule: $($rule.Name)"
}

Write-Output ""
Write-Output "Firewall rule summary:"
foreach ($rule in $rules) {
    & netsh advfirewall firewall show rule name="$($rule.Name)" | Out-Host
}
