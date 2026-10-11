$ErrorActionPreference = 'Stop'
$auditRoot = $PSScriptRoot
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $auditRoot '../..'))
$replacements = @{}
foreach ($item in @(
    @('cmd/resin/resin_security_audit_test.go', 'runtime_test.go.txt'),
    @('internal/api/resin_security_audit_test.go', 'api_test.go.txt'),
    @('internal/proxy/resin_security_audit_test.go', 'proxy_test.go.txt'),
    @('internal/netutil/resin_security_audit_test.go', 'netutil_test.go.txt')
)) {
    $virtualPath = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $item[0]))
    $replacements[$virtualPath] = Join-Path $auditRoot $item[1]
}
$overlayPath = Join-Path $auditRoot 'overlay.generated.json'
@{ Replace = $replacements } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $overlayPath -Encoding utf8
Push-Location $repoRoot
try {
    # overlay 将审计测试映射到原包；不改写任何业务源码。
    & go test -overlay $overlayPath ./cmd/resin ./internal/api ./internal/proxy ./internal/netutil -run '^TestAudit' -count=1 -timeout 60s -v 2>&1 |
        Tee-Object -FilePath (Join-Path $auditRoot 'checks-output.txt')
    $checkExit = $LASTEXITCODE
    if ($checkExit -ne 0) { throw "审计验证退出码：$checkExit" }
} finally {
    Pop-Location
}
