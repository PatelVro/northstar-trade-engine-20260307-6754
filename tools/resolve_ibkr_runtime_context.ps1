param(
  [string]$GatewayUrl = "https://127.0.0.1:5002/v1/api",
  [int]$TimeoutSeconds = 8
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-CurlHttpResponse {
  param(
    [string]$Url,
    [int]$TimeoutSeconds
  )

  $raw = & curl.exe -k -i -s -m $TimeoutSeconds $Url
  if ($LASTEXITCODE -ne 0) {
    throw "curl failed for $Url with exit code $LASTEXITCODE"
  }

  $rawText = if ($raw -is [System.Array]) { $raw -join "`r`n" } else { [string]$raw }
  if ([string]::IsNullOrWhiteSpace($rawText)) {
    throw "empty response from $Url"
  }

  $parts = [regex]::Split($rawText, "\r?\n\r?\n", 2)
  if ($parts.Length -lt 2) {
    throw "failed to parse HTTP response from $Url"
  }

  $headers = $parts[0]
  $body = $parts[1]
  $statusCode = 0
  $statusMatch = [regex]::Match($headers, "^HTTP/\S+\s+(\d+)", [System.Text.RegularExpressions.RegexOptions]::Multiline)
  if ($statusMatch.Success) {
    [void][int]::TryParse($statusMatch.Groups[1].Value, [ref]$statusCode)
  }

  $cookieMatch = [regex]::Match($headers, "(?im)^Set-Cookie:\s*(x-sess-uuid=[^;]+)")
  $sessionCookie = ""
  if ($cookieMatch.Success) {
    $sessionCookie = $cookieMatch.Groups[1].Value.Trim()
  }

  return [pscustomobject]@{
    Url           = $Url
    StatusCode    = $statusCode
    Headers       = $headers
    Body          = $body
    SessionCookie = $sessionCookie
  }
}

function ConvertFrom-JsonSafe {
  param([string]$Text)

  if ([string]::IsNullOrWhiteSpace($Text)) {
    return $null
  }

  try {
    return $Text | ConvertFrom-Json
  } catch {
    return $null
  }
}

function Get-BodySummary {
  param([string]$Text)

  if ([string]::IsNullOrWhiteSpace($Text)) {
    return "<empty>"
  }

  $singleLine = ($Text -replace "\s+", " ").Trim()
  if ($singleLine.Length -gt 180) {
    return $singleLine.Substring(0, 180) + "..."
  }

  return $singleLine
}

$base = $GatewayUrl.TrimEnd("/")
$authEndpoint = "$base/iserver/auth/status"
$authResponse = Invoke-CurlHttpResponse -Url $authEndpoint -TimeoutSeconds $TimeoutSeconds
if ($authResponse.StatusCode -ne 200) {
  throw "failed to query IBKR auth status ($($authResponse.StatusCode)) at ${authEndpoint}: $(Get-BodySummary $authResponse.Body)"
}

$authPayload = ConvertFrom-JsonSafe -Text $authResponse.Body
if ($null -eq $authPayload) {
  throw "IBKR auth status returned non-JSON response at ${authEndpoint}: $(Get-BodySummary $authResponse.Body)"
}

if (-not ($authPayload.authenticated -eq $true -and $authPayload.connected -eq $true)) {
  $established = if ($null -ne $authPayload.established) { [string]$authPayload.established } else { "unknown" }
  throw "IBKR gateway is reachable but not authenticated. authenticated=$($authPayload.authenticated) connected=$($authPayload.connected) established=$established. Reauthenticate the local Client Portal/IBeam session before starting Northstar."
}

$endpoint = "$base/iserver/accounts"
$accountsResponse = Invoke-CurlHttpResponse -Url $endpoint -TimeoutSeconds $TimeoutSeconds
if ($accountsResponse.StatusCode -ne 200) {
  throw "failed to query IBKR accounts ($($accountsResponse.StatusCode)) at ${endpoint}: $(Get-BodySummary $accountsResponse.Body)"
}

$payload = ConvertFrom-JsonSafe -Text $accountsResponse.Body
if ($null -eq $payload) {
  throw "IBKR accounts returned non-JSON response at ${endpoint}: $(Get-BodySummary $accountsResponse.Body)"
}

$accountId = ""
if ($payload.selectedAccount) {
  $accountId = [string]$payload.selectedAccount
}

if ([string]::IsNullOrWhiteSpace($accountId) -and $payload.accounts -and $payload.accounts.Count -gt 0) {
  $accountId = [string]$payload.accounts[0]
}

if ([string]::IsNullOrWhiteSpace($accountId)) {
  throw "failed to resolve IBKR account id from $endpoint"
}

$sessionCookie = $accountsResponse.SessionCookie
if ([string]::IsNullOrWhiteSpace($sessionCookie)) {
  $sessionCookie = $authResponse.SessionCookie
}

Write-Output ("ACCOUNT_ID={0}" -f $accountId)
if (-not [string]::IsNullOrWhiteSpace($sessionCookie)) {
  Write-Output ("SESSION_COOKIE={0}" -f $sessionCookie)
}
