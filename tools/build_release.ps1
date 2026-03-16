param(
    [string]$Version = "",
    [string]$Commit = "",
    [string]$BuildTime = "",
    [string]$Channel = "release",
    [string]$Dirty = "",
    [string]$OutFile = "",
    [string]$Package = "."
)

$ErrorActionPreference = "Stop"

function Get-GitValue {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Args
    )

    $git = Get-Command git -ErrorAction SilentlyContinue
    if (-not $git) {
        return ""
    }

    $output = & $git.Source @Args 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ""
    }

    return ($output -join "`n").Trim()
}

function Get-GitDirtyState {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if (-not $git) {
        return ""
    }

    $statusLines = & $git.Source status --porcelain --untracked-files=normal 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ""
    }

    if (($statusLines -join "").Trim().Length -gt 0) {
        return "dirty"
    }

    return "clean"
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = Get-GitValue -Args @("describe", "--tags", "--always", "--dirty")
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = "dev"
}

if ([string]::IsNullOrWhiteSpace($Commit)) {
    $Commit = Get-GitValue -Args @("rev-parse", "HEAD")
}
if ([string]::IsNullOrWhiteSpace($Commit)) {
    $Commit = "unknown"
}

if ([string]::IsNullOrWhiteSpace($BuildTime)) {
    $BuildTime = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
}

if ([string]::IsNullOrWhiteSpace($Dirty)) {
    $Dirty = Get-GitDirtyState
}
if ([string]::IsNullOrWhiteSpace($Dirty)) {
    $Dirty = "unknown"
}

if ([string]::IsNullOrWhiteSpace($OutFile)) {
    if ($env:OS -eq "Windows_NT") {
        $OutFile = "northstar.exe"
    } else {
        $OutFile = "northstar"
    }
}

$ldflags = @(
    "-s",
    "-w",
    "-X", "northstar/buildinfo.Version=$Version",
    "-X", "northstar/buildinfo.Commit=$Commit",
    "-X", "northstar/buildinfo.BuildTime=$BuildTime",
    "-X", "northstar/buildinfo.Channel=$Channel",
    "-X", "northstar/buildinfo.Dirty=$Dirty"
) -join " "

Write-Host "Building Northstar release binary..."
Write-Host "  Version:   $Version"
Write-Host "  Commit:    $Commit"
Write-Host "  BuildTime: $BuildTime"
Write-Host "  Channel:   $Channel"
Write-Host "  Dirty:     $Dirty"
Write-Host "  OutFile:   $OutFile"

go build -trimpath -ldflags $ldflags -o $OutFile $Package
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}

Write-Host ""
Write-Host "Build complete."
