param([string]$GoImage='docker.m.daocloud.io/library/golang:1.25')
$ErrorActionPreference='Stop'
$repoRoot=Split-Path $PSScriptRoot -Parent
Push-Location $repoRoot
try {
 New-Item -ItemType Directory -Force docs/evidence | Out-Null
 pnpm check
 if ($LASTEXITCODE) {throw 'TypeScript check failed'}
 node deploy/test-topology.cjs 2>&1 | Tee-Object docs/evidence/topology-tests.log
 if ($LASTEXITCODE) {throw 'Topology relationship tests failed'}
 $moduleCache=(go env GOMODCACHE).Trim()
 docker run --rm --mount "type=bind,source=$repoRoot,target=/src,readonly" --mount "type=bind,source=$moduleCache,target=/go/pkg/mod" -v migate-go-build-cache:/root/.cache/go-build -w /src -e MIGATE_LIFECYCLE_INTEGRATION=1 $GoImage sh -c 'sh -n deploy/install.sh && go test ./...' 2>&1 | Tee-Object docs/evidence/linux-tests.log
 if ($LASTEXITCODE) {throw 'Linux tests failed'}
 docker compose -f deploy/test.compose.yml down
 if ($LASTEXITCODE) {throw 'Cannot reset dedicated test stack'}
 try {
  docker compose -f deploy/test.compose.yml up -d --wait
  if ($LASTEXITCODE) {throw 'Cannot start dedicated test stack'}
  $env:MIGATE_INTEGRATION='1'
  go test ./internal/control -run TestIntegration -count=1 -v 2>&1 | Tee-Object docs/evidence/integration-tests.log
  if ($LASTEXITCODE) {throw 'Integration tests failed'}
 } finally { Remove-Item Env:MIGATE_INTEGRATION -ErrorAction SilentlyContinue; docker compose -f deploy/test.compose.yml down }
 $env:MIGATE_UPSTREAM_TESTS='1'
 go test ./services/subscriptions -count=1 -v 2>&1 | Tee-Object docs/evidence/upstream-tests.log
 if ($LASTEXITCODE) {throw 'Upstream comparison failed'}
 go test ./internal/control -run 'TestINIUpstreamNodeComparison|TestEgernUpstreamNodeComparison|TestSourceDropUpstreamComparison' -count=1 -v 2>&1 | Tee-Object docs/evidence/upstream-ini-tests.log
 if ($LASTEXITCODE) {throw 'Upstream INI comparison failed'}
 go test ./internal/subscription -run 'TestNamePlusUpstreamComparison|TestSourceFiltersUpstreamComparison' -count=1 -v 2>&1 | Tee-Object docs/evidence/upstream-name-options.log
 if ($LASTEXITCODE) {throw 'Upstream name option comparison failed'}
} finally {Remove-Item Env:MIGATE_UPSTREAM_TESTS -ErrorAction SilentlyContinue;Pop-Location}
