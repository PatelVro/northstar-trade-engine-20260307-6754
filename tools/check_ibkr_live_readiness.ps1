param(
    [string]$GatewayUrl = "https://127.0.0.1:5002/v1/api",
    [string]$AccountId = "",
    [int]$Iterations = 5,
    [int]$DelaySeconds = 3,
    [int]$RequestTimeoutSeconds = 8
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($AccountId)) {
    $AccountId = $env:NORTHSTAR_IBKR_ACCOUNT_ID
}

if ([string]::IsNullOrWhiteSpace($AccountId)) {
    try {
        $resolved = & powershell -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "resolve_ibkr_runtime_context.ps1") -GatewayUrl $GatewayUrl
        foreach ($line in $resolved) {
            if ($line -like "ACCOUNT_ID=*") {
                $AccountId = $line.Split("=", 2)[1]
                break
            }
        }
    } catch {
        $AccountId = ""
    }
}

if ([string]::IsNullOrWhiteSpace($AccountId)) {
    Write-Error "AccountId is required. Pass -AccountId or set NORTHSTAR_IBKR_ACCOUNT_ID."
    exit 1
}

function Warm-PortfolioSession {
    param(
        [string]$GatewayUrl,
        [string]$CookieJar,
        [int]$TimeoutSeconds
    )

    $warmups = @(
        "$GatewayUrl/portfolio/accounts",
        "$GatewayUrl/portfolio/subaccounts",
        "$GatewayUrl/portfolio/subaccounts2?page=0"
    )

    foreach ($url in $warmups) {
        $result = Invoke-CurlJson -Url $url -CookieJar $CookieJar -TimeoutSeconds $TimeoutSeconds
        if ($result.Code -eq 200) {
            continue
        }
        if ($result.Code -eq 0 -or $result.Code -ge 500) {
            Write-Host "  warmup endpoint returned $($result.Code): $url"
        }
    }
}

function Invoke-AccountEndpointWithRetry {
    param(
        [string]$GatewayUrl,
        [string]$AccountId,
        [string]$RelativePath,
        [string]$CookieJar,
        [int]$TimeoutSeconds,
        [int]$Attempts = 3
    )

    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        Warm-PortfolioSession -GatewayUrl $GatewayUrl -CookieJar $CookieJar -TimeoutSeconds $TimeoutSeconds
        $result = Invoke-CurlJson -Url "$GatewayUrl/$RelativePath" -CookieJar $CookieJar -TimeoutSeconds $TimeoutSeconds
        if ($result.Code -eq 200) {
            return $result
        }
        if ($attempt -lt $Attempts -and ($result.Code -eq 401 -or $result.Code -eq 503 -or $result.Code -eq 504)) {
            Start-Sleep -Milliseconds (400 * $attempt)
            continue
        }
        return $result
    }

    return Invoke-CurlJson -Url "$GatewayUrl/$RelativePath" -CookieJar $CookieJar -TimeoutSeconds $TimeoutSeconds
}

function Invoke-CurlJson {
    param(
        [string]$Url,
        [string]$CookieJar,
        [int]$TimeoutSeconds
    )

    $raw = & curl.exe -k -s -m $TimeoutSeconds -b $CookieJar -c $CookieJar -w "`n%{http_code}" $Url
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

function Get-BodySummary {
    param([string]$Body)

    if ([string]::IsNullOrWhiteSpace($Body)) {
        return "<empty>"
    }

    $singleLine = ($Body -replace "\s+", " ").Trim()
    if ($singleLine.Length -gt 180) {
        return $singleLine.Substring(0, 180) + "..."
    }

    return $singleLine
}

$cookieJar = Join-Path $env:TEMP "Northstar_ibkr_live_readiness.cookies.txt"
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
    $auth = Invoke-CurlJson -Url "$GatewayUrl/iserver/auth/status" -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds
    $accounts = Invoke-CurlJson -Url "$GatewayUrl/portfolio/accounts" -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds
    Warm-PortfolioSession -GatewayUrl $GatewayUrl -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds
    $summary = Invoke-AccountEndpointWithRetry -GatewayUrl $GatewayUrl -AccountId $AccountId -RelativePath "portfolio/$AccountId/summary" -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds
    $positions = Invoke-AccountEndpointWithRetry -GatewayUrl $GatewayUrl -AccountId $AccountId -RelativePath "portfolio/$AccountId/positions" -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds
    $orders = Invoke-CurlJson -Url "$GatewayUrl/iserver/account/orders" -CookieJar $cookieJar -TimeoutSeconds $RequestTimeoutSeconds

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
        if (-not $authOk) {
            Write-Host "  auth body: $($auth.Body)"
            if ($auth.Code -eq 200) {
                Write-Host "  auth summary: gateway reachable but not authenticated/connected yet"
            } else {
                Write-Host "  auth summary: gateway auth endpoint unavailable or returned an unexpected response"
            }
        }
        if ($accounts.Code -ne 200) {
            Write-Host "  accounts body: $(Get-BodySummary $accounts.Body)"
        }
        if ($summary.Code -ne 200) {
            Write-Host "  summary body: $(Get-BodySummary $summary.Body)"
        }
        if ($positions.Code -ne 200) {
            Write-Host "  positions body: $(Get-BodySummary $positions.Body)"
        }
        if ($orders.Code -ne 200) {
            Write-Host "  orders body: $(Get-BodySummary $orders.Body)"
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
