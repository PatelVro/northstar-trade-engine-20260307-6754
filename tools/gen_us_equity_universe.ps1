param(
    [string]$OutputPath = "data/universe/us_companies.txt",
    [switch]$IncludeETFs
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$nasdaqUrl = "https://www.nasdaqtrader.com/dynamic/SymDir/nasdaqlisted.txt"
$otherUrl = "https://www.nasdaqtrader.com/dynamic/SymDir/otherlisted.txt"

$tmpNasdaq = Join-Path $env:TEMP ("nasdaqlisted_{0}.txt" -f [guid]::NewGuid().ToString("N"))
$tmpOther = Join-Path $env:TEMP ("otherlisted_{0}.txt" -f [guid]::NewGuid().ToString("N"))

try {
    Write-Host "Downloading symbol directories..."
    Invoke-WebRequest -Uri $nasdaqUrl -OutFile $tmpNasdaq -UseBasicParsing
    Invoke-WebRequest -Uri $otherUrl -OutFile $tmpOther -UseBasicParsing

    $symbols = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)

    function Add-Symbol([string]$raw) {
        if ([string]::IsNullOrWhiteSpace($raw)) { return }
        $symbol = $raw.Trim().ToUpper()
        if ($symbol -notmatch '^[A-Z0-9]{1,5}(\.[A-Z0-9]{1,2})?$') { return }
        [void]$symbols.Add($symbol)
    }

    # Format:
    # Symbol|Security Name|Market Category|Test Issue|Financial Status|Round Lot Size|ETF|NextShares
    foreach ($line in Get-Content $tmpNasdaq) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        if ($line.StartsWith("Symbol|") -or $line.StartsWith("File Creation Time")) { continue }

        $cols = $line -split '\|'
        if ($cols.Count -lt 8) { continue }

        $testIssue = $cols[3]
        $isEtf = $cols[6]
        if ($testIssue -ne "N") { continue }
        if (-not $IncludeETFs -and $isEtf -eq "Y") { continue }

        Add-Symbol $cols[0]
    }

    # Format:
    # ACT Symbol|Security Name|Exchange|CQS Symbol|ETF|Round Lot Size|Test Issue|NASDAQ Symbol
    foreach ($line in Get-Content $tmpOther) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        if ($line.StartsWith("ACT Symbol|") -or $line.StartsWith("File Creation Time")) { continue }

        $cols = $line -split '\|'
        if ($cols.Count -lt 8) { continue }

        $testIssue = $cols[6]
        $isEtf = $cols[4]
        if ($testIssue -ne "N") { continue }
        if (-not $IncludeETFs -and $isEtf -eq "Y") { continue }

        $symbol = if ([string]::IsNullOrWhiteSpace($cols[3])) { $cols[0] } else { $cols[3] }
        Add-Symbol $symbol
    }

    $sorted = @($symbols) | Sort-Object
    if ($sorted.Count -eq 0) {
        throw "No symbols parsed from source files."
    }

    $outDir = Split-Path -Parent $OutputPath
    if ($outDir -and -not (Test-Path $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    }

    Set-Content -Path $OutputPath -Value $sorted -Encoding ASCII

    Write-Host ("Wrote {0} symbols to {1}" -f $sorted.Count, $OutputPath)
    Write-Host ("Sample: {0}" -f (($sorted | Select-Object -First 15) -join ", "))
}
finally {
    Remove-Item -ErrorAction SilentlyContinue $tmpNasdaq, $tmpOther
}
