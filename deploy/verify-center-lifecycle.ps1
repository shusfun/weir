param()
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
$oldGoos = $env:GOOS; $oldGoarch = $env:GOARCH; $oldCgo = $env:CGO_ENABLED
Push-Location $repoRoot
try {
 New-Item -ItemType Directory -Force dist,docs/evidence | Out-Null
 $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
 go test -c -o dist/lifecycle.test ./internal/lifecycle
 if ($LASTEXITCODE) { throw 'Lifecycle test compilation failed' }
 # This compose project is disposable and must never point at shared services.
 docker compose -f deploy/test.compose.yml up -d --wait
 if ($LASTEXITCODE) { throw 'Cannot start dedicated test services' }
 docker run --rm --network migate-local-test_default --mount "type=bind,source=$repoRoot/dist/lifecycle.test,target=/tests/lifecycle.test,readonly" -e MIGATE_CENTER_PURGE_TEST=1 --entrypoint /tests/lifecycle.test postgres:18-alpine '-test.run=TestIsolatedCenterPurgeAndPGRestore' '-test.v' 2>&1 | Tee-Object docs/evidence/center-lifecycle-tests.log
 if ($LASTEXITCODE) { throw 'Center backup/purge/restore test failed' }
} finally {
 docker compose -f deploy/test.compose.yml down
 $env:GOOS=$oldGoos; $env:GOARCH=$oldGoarch; $env:CGO_ENABLED=$oldCgo
 Pop-Location
}
