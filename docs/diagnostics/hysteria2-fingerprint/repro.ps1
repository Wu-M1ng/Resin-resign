param([string]$TestPattern = '^TestDiagnosticHY2Fingerprint$')

$ErrorActionPreference = 'Stop'
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$overlayPath = [System.IO.Path]::GetTempFileName()
$testTarget = Join-Path $repoRoot 'internal/outbound/diagnostic_hy2_fingerprint_test.go'
$testSource = Join-Path $PSScriptRoot 'repro_test.go.txt'
$replacements = @{}
$replacements[$testTarget] = $testSource

try {
    $overlayJson = @{ Replace = $replacements } | ConvertTo-Json -Depth 3
    [System.IO.File]::WriteAllText($overlayPath, $overlayJson, (New-Object System.Text.UTF8Encoding($false)))
    Push-Location -LiteralPath $repoRoot
    try {
        # Go overlay 临时注入诊断测试，不改业务文件。所有拨号均限定在回环地址。
        & go test -overlay $overlayPath -tags 'with_quic,with_utls' ./internal/outbound -run $TestPattern -count=1 -v
        if ($LASTEXITCODE -ne 0) { throw "Go diagnostic failed: exit $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
} finally {
    Remove-Item -LiteralPath $overlayPath -Force
}
