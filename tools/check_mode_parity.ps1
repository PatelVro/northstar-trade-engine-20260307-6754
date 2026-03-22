param(
  [Parameter(Mandatory = $true)]
  [string]$BaselineConfig,

  [Parameter(Mandatory = $true)]
  [string]$CandidateConfig
)

$ErrorActionPreference = "Stop"

function Resolve-ConfigPath {
  param([string]$PathValue)
  if ([System.IO.Path]::IsPathRooted($PathValue)) {
    return $PathValue
  }
  return Join-Path (Get-Location) $PathValue
}

function Get-ComparableConfig {
  param([string]$PathValue)

  $resolved = Resolve-ConfigPath $PathValue
  if (-not (Test-Path $resolved)) {
    throw "Config file not found: $resolved"
  }

  $json = Get-Content $resolved -Raw | ConvertFrom-Json
  return $json
}

function Is-ScalarValue {
  param([object]$Value)
  return $null -eq $Value -or
    $Value -is [string] -or
    $Value -is [bool] -or
    $Value -is [int] -or
    $Value -is [long] -or
    $Value -is [double] -or
    $Value -is [decimal]
}

function Flatten-JsonValue {
  param(
    [object]$Value,
    [string]$PathPrefix,
    [hashtable]$Sink
  )

  if (Is-ScalarValue $Value) {
    $Sink[$PathPrefix] = if ($null -eq $Value) { "<null>" } else { [string]$Value }
    return
  }

  if (($Value -is [System.Array] -or $Value -is [System.Collections.IList]) -and -not ($Value -is [string])) {
    $index = 0
    foreach ($item in $Value) {
      $childPath = "{0}[{1}]" -f $PathPrefix, $index
      Flatten-JsonValue -Value $item -PathPrefix $childPath -Sink $Sink
      $index++
    }
    if ($index -eq 0) {
      $Sink[$PathPrefix] = "<empty-array>"
    }
    return
  }

  $properties = $Value.PSObject.Properties
  if ($properties.Count -eq 0) {
    $Sink[$PathPrefix] = [string]$Value
    return
  }

  foreach ($property in $properties) {
    $childPath = if ([string]::IsNullOrWhiteSpace($PathPrefix)) {
      $property.Name
    } else {
      "{0}.{1}" -f $PathPrefix, $property.Name
    }
    Flatten-JsonValue -Value $property.Value -PathPrefix $childPath -Sink $Sink
  }
}

function Test-IgnoredPath {
  param(
    [string]$PathValue,
    [string[]]$IgnorePatterns
  )

  foreach ($pattern in $IgnorePatterns) {
    if ($PathValue -eq $pattern) {
      return $true
    }
  }

  return $false
}

$ignorePatterns = @(
  "traders[0].id",
  "traders[0].name",
  "traders[0].mode",
  "traders[0].live_promotion_approved",
  "traders[0].promotion_source_trader_id",
  "traders[0].min_paper_session_reports",
  "traders[0].require_backtest_summary",
  "traders[0].require_release_build_for_live",
  "traders[0].promotion_max_evidence_age_days",
  "traders[0].strict_live_mode",
  "api_server_port"
)

$baseline = Get-ComparableConfig -PathValue $BaselineConfig
$candidate = Get-ComparableConfig -PathValue $CandidateConfig

$baselineFlat = @{}
$candidateFlat = @{}
Flatten-JsonValue -Value $baseline -PathPrefix "" -Sink $baselineFlat
Flatten-JsonValue -Value $candidate -PathPrefix "" -Sink $candidateFlat

$allPaths = @($baselineFlat.Keys + $candidateFlat.Keys | Sort-Object -Unique)
$differences = New-Object System.Collections.Generic.List[string]

foreach ($path in $allPaths) {
  if ([string]::IsNullOrWhiteSpace($path)) {
    continue
  }
  if (Test-IgnoredPath -PathValue $path -IgnorePatterns $ignorePatterns) {
    continue
  }

  $baselineValue = if ($baselineFlat.ContainsKey($path)) { $baselineFlat[$path] } else { "<missing>" }
  $candidateValue = if ($candidateFlat.ContainsKey($path)) { $candidateFlat[$path] } else { "<missing>" }

  if ($baselineValue -ne $candidateValue) {
    $differences.Add(("{0}`n  baseline : {1}`n  candidate: {2}" -f $path, $baselineValue, $candidateValue))
  }
}

if ($differences.Count -gt 0) {
  Write-Host "[PARITY FAIL] Candidate config drifts from baseline beyond allowed live/paper differences." -ForegroundColor Red
  Write-Host ""
  foreach ($difference in $differences) {
    Write-Host $difference
    Write-Host ""
  }
  exit 1
}

Write-Host "[PARITY OK] Candidate config matches baseline for strategy, market, and sizing controls." -ForegroundColor Green
