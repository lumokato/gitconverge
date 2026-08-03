$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$rows = go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}|{{.Dir}}{{end}}{{end}}' .\cmd\gitconverge .\cmd\gitconverge-tray |
    Where-Object { $_ } |
    Sort-Object -Unique

foreach ($row in $rows) {
    $module, $moduleDir = $row -split '\|', 2
    $licenses = Get-ChildItem -LiteralPath $moduleDir -Force |
        Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE)(\.|$)' } |
        Sort-Object Name
    if (-not $licenses) {
        throw "No root license file found for runtime module $module"
    }
    foreach ($license in $licenses) {
        $fileName = (($module + '_' + $license.Name) -replace '[^A-Za-z0-9._-]+', '_') + '.txt'
        $copyPath = Join-Path $root "third_party_licenses\$fileName"
        if (-not (Test-Path -LiteralPath $copyPath)) {
            throw "Missing third-party license copy: $fileName"
        }
        $sourceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $license.FullName).Hash
        $copyHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $copyPath).Hash
        if ($sourceHash -ne $copyHash) {
            throw "Third-party license copy is stale: $fileName"
        }
    }
}

Write-Output "Third-party license verification passed for $($rows.Count) runtime modules."

