param(
    [string]$GatewayUrl = "https://127.0.0.1:5002/v1/api",
    [string]$AccountId = "",
    [int]$TimeoutSeconds = 8
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-CurlJson {
    param(
        [string]$Url,
        [string]$CookieJar,
        [int]$TimeoutSeconds
    )

    $raw = & curl.exe -k -s -m $TimeoutSeconds -b $CookieJar -c $CookieJar -w "`n%{http_code}" $Url
    if ($LASTEXITCODE -ne 0) {
        return [pscustomobject]@{
            Url  = $Url
            Code = 0
            Body = "curl failed with exit code $LASTEXITCODE"
            Json = $null
        }
    }

    $parts = $raw -split "`n"
    $code = 0
    [void][int]::TryParse($parts[-1], [ref]$code)
    $body = ""
    if ($parts.Length -gt 1) {
        $body = ($parts[0..($parts.Length - 2)] -join "`n")
    }

    $json = $null
    if (-not [string]::IsNullOrWhiteSpace($body)) {
        try {
            $json = $body | ConvertFrom-Json
        } catch {
            $json = $null
        }
    }

    return [pscustomobject]@{
        Url  = $Url
        Code = $code
        Body = $body
        Json = $json
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

function Resolve-AccountId {
    param(
        [string]$GatewayUrl,
        [string]$CookieJar,
        [int]$TimeoutSeconds,
        [string]$InitialAccountId
    )

    if (-not [string]::IsNullOrWhiteSpace($InitialAccountId)) {
        return $InitialAccountId
    }

    $accounts = Invoke-CurlJson -Url "$GatewayUrl/iserver/accounts" -CookieJar $CookieJar -TimeoutSeconds $TimeoutSeconds
    if ($accounts.Code -ne 200 -or $null -eq $accounts.Json) {
        return ""
    }

    if ($accounts.Json.selectedAccount) {
        return [string]$accounts.Json.selectedAccount
    }

    if ($accounts.Json.accounts -and $accounts.Json.accounts.Count -gt 0) {
        return [string]$accounts.Json.accounts[0]
    }

    return ""
}

$cookieJar = Join-Path $env:TEMP "Northstar_ibkr_gateway_state.cookies.txt"
if (Test-Path $cookieJar) {
    Remove-Item $cookieJar -Force
}

try {
    $auth = Invoke-CurlJson -Url "$GatewayUrl/iserver/auth/status" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
    $tickle = Invoke-CurlJson -Url "$GatewayUrl/tickle" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
    $resolvedAccountId = Resolve-AccountId -GatewayUrl $GatewayUrl -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds -InitialAccountId $AccountId

    $accounts = if ([string]::IsNullOrWhiteSpace($resolvedAccountId)) {
        Invoke-CurlJson -Url "$GatewayUrl/iserver/accounts" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
    } else {
        Invoke-CurlJson -Url "$GatewayUrl/portfolio/accounts" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
    }

    $summary = $null
    $positions = $null
    $history = $null

    if (-not [string]::IsNullOrWhiteSpace($resolvedAccountId)) {
        $summary = Invoke-CurlJson -Url "$GatewayUrl/portfolio/$resolvedAccountId/summary" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
        $positions = Invoke-CurlJson -Url "$GatewayUrl/portfolio/$resolvedAccountId/positions" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds
    }

    $history = Invoke-CurlJson -Url "$GatewayUrl/iserver/marketdata/history?conid=265598&bar=1min&period=1d" -CookieJar $cookieJar -TimeoutSeconds $TimeoutSeconds

    $authJson = $auth.Json
    $authenticated = $false
    $connected = $false
    $established = $false
    if ($null -ne $authJson) {
        $authenticated = ($authJson.authenticated -eq $true)
        $connected = ($authJson.connected -eq $true)
        $established = ($authJson.established -eq $true)
    }

    $classification = "ready"
    $reason = "gateway authenticated and account/history endpoints reachable"
    if ($auth.Code -eq 0 -or $tickle.Code -eq 0) {
        $classification = "gateway_unreachable"
        $reason = "gateway did not respond cleanly"
    } elseif ($auth.Code -ne 200) {
        $classification = "gateway_error"
        $reason = "auth status returned HTTP $($auth.Code)"
    } elseif (-not $authenticated) {
        $classification = "not_authenticated"
        $reason = "gateway reachable but not authenticated"
    } elseif (-not $connected) {
        $classification = "not_connected"
        $reason = "gateway authenticated session is not connected"
    } elseif ($null -ne $summary -and $summary.Code -ne 200) {
        $classification = "account_state_unavailable"
        $reason = "account summary endpoint returned HTTP $($summary.Code)"
    } elseif ($null -ne $positions -and $positions.Code -ne 200) {
        $classification = "account_state_unavailable"
        $reason = "account positions endpoint returned HTTP $($positions.Code)"
    } elseif ($history.Code -ne 200) {
        $classification = "market_data_unavailable"
        $reason = "market-data history endpoint returned HTTP $($history.Code)"
    }

    $result = [pscustomobject]@{
        checked_at        = (Get-Date).ToString("o")
        gateway_url       = $GatewayUrl
        account_id        = $resolvedAccountId
        classification    = $classification
        reason            = $reason
        authenticated     = $authenticated
        connected         = $connected
        established       = $established
        auth_http_code    = $auth.Code
        tickle_http_code  = $tickle.Code
        accounts_http_code = $accounts.Code
        summary_http_code = if ($null -ne $summary) { $summary.Code } else { 0 }
        positions_http_code = if ($null -ne $positions) { $positions.Code } else { 0 }
        history_http_code = $history.Code
        auth_summary      = Get-BodySummary -Body $auth.Body
        accounts_summary  = Get-BodySummary -Body $accounts.Body
        summary_summary   = if ($null -ne $summary) { Get-BodySummary -Body $summary.Body } else { "<skipped>" }
        positions_summary = if ($null -ne $positions) { Get-BodySummary -Body $positions.Body } else { "<skipped>" }
        history_summary   = Get-BodySummary -Body $history.Body
    }

    $result | ConvertTo-Json -Depth 4
} finally {
    if (Test-Path $cookieJar) {
        Remove-Item $cookieJar -Force
    }
}
