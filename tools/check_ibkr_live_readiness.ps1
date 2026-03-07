param(
    [string]$GatewayUrl = "https://127.0.0.1:5002/v1/api",
    [string]$AccountId = "",
    [int]$Iterations = 5,
    [int]$DelaySeconds = 3
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($AccountId)) {
    Write-Error "AccountId is required. Example: -AccountId DUP200062"
    exit 1
}

function Invoke-CurlJson {
    param(
        [string]$Url,
        [string]$CookieJar
    )

    $raw = & curl.exe -k -s -m 20 -b $CookieJar -c $CookieJar -w "`n%{http_code}" $Url
    if ($LASTEXITCODE -ne 0) {
        return [pscustomobject]@{
            Code = 0
            Body = "curl failed with exit code $LASTEXITCODE"
        }
    }

    $parts = $raw -split "`n"
    $code = 0
    [void][int]::TryParse($parts[-1], [ref]$code)
    $body = ""
    if ($parts.Length -gt 1) {
        $body = ($parts[0..($parts.Length - 2)] -join "`n")
    }

    return [pscustomobject]@{
        Code = $code
        Body = $body
    }
}

$cookieJar = Join-Path $env:TEMP "AegisTrade_ibkr_live_readiness.cookies.txt"
if (Test-Path $cookieJar) {
    Remove-Item $cookieJar -Force
}

Write-Host "IBKR live readiness check"
Write-Host "Gateway: $GatewayUrl"
Write-Host "Account: $AccountId"
Write-Host "Iterations: $Iterations"
Write-Host ""

$allPassed = $true

for ($i = 1; $i -le $Iterations; $i++) {
    $auth = Invoke-CurlJson -Url "$GatewayUrl/iserver/auth/status" -CookieJar $cookieJar
    $accounts = Invoke-CurlJson -Url "$GatewayUrl/portfolio/accounts" -CookieJar $cookieJar
    $summary = Invoke-CurlJson -Url "$GatewayUrl/portfolio/$AccountId/summary" -CookieJar $cookieJar
    $positions = Invoke-CurlJson -Url "$GatewayUrl/portfolio/$AccountId/positions" -CookieJar $cookieJar
    $orders = Invoke-CurlJson -Url "$GatewayUrl/iserver/account/orders" -CookieJar $cookieJar

    $authOk = $false
    if ($auth.Code -eq 200) {
        try {
            $authObj = $auth.Body | ConvertFrom-Json
            $authOk = ($authObj.authenticated -eq $true) -and ($authObj.connected -eq $true)
        } catch {
            $authOk = $false
        }
    }

    $iterationPass = $authOk -and
        ($accounts.Code -eq 200) -and
        ($summary.Code -eq 200) -and
        ($positions.Code -eq 200) -and
        ($orders.Code -eq 200)

    if (-not $iterationPass) {
        $allPassed = $false
    }

    $stamp = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
    Write-Host ("[{0}] Iteration {1}/{2} | auth={3} accounts={4} summary={5} positions={6} orders={7} | pass={8}" -f `
        $stamp, $i, $Iterations, $auth.Code, $accounts.Code, $summary.Code, $positions.Code, $orders.Code, $iterationPass)

    if (-not $iterationPass) {
        if ($summary.Code -ne 200) {
            Write-Host "  summary body: $($summary.Body)"
        }
        if ($positions.Code -ne 200) {
            Write-Host "  positions body: $($positions.Body)"
        }
    }

    if ($i -lt $Iterations) {
        Start-Sleep -Seconds $DelaySeconds
    }
}

if (Test-Path $cookieJar) {
    Remove-Item $cookieJar -Force
}

Write-Host ""
if ($allPassed) {
    Write-Host "PASS: IBKR live readiness check succeeded."
    exit 0
}

Write-Host "FAIL: IBKR live readiness check failed. Keep strict_live_mode enabled and do not start live trading."
exit 1
