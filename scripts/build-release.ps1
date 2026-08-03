param(
    [string]$Version = "0.1.0-dev"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$buildDir = Join-Path $root "dist\windows-amd64"
$outputDir = Join-Path $root "dist"
$isccCandidates = @(
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
    (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFilesX86)) "Inno Setup 6\ISCC.exe")
)
$iscc = $isccCandidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
if (-not $iscc) {
    throw "Inno Setup 6 was not found. Install it before building the installer."
}

New-Item -ItemType Directory -Force -Path $buildDir, $outputDir | Out-Null
Push-Location $root
try {
    go test ./...
    go vet ./...
    pwsh -NoProfile -File .\scripts\Test-ThirdPartyLicenses.ps1
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o "$buildDir\gitconverge.exe" .\cmd\gitconverge
    go build -trimpath -ldflags "-s -w -H=windowsgui" -o "$buildDir\gitconverge-tray.exe" .\cmd\gitconverge-tray
    & $iscc "/DAppVersion=$Version" "/DBuildDir=$buildDir" "/DOutputDir=$outputDir" ".\installer\GitConverge.iss"
    if ($LASTEXITCODE -ne 0) {
        throw "Inno Setup failed with exit code $LASTEXITCODE."
    }
    $installer = Join-Path $outputDir "GitConverge-$Version-windows-x64-setup.exe"
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $installer).Hash.ToLowerInvariant()
    "$hash  $(Split-Path -Leaf $installer)" | Set-Content -Encoding ascii -LiteralPath "$installer.sha256"
} finally {
    Pop-Location
}
