param([string]$Batch = 'B0')
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
Push-Location $repoRoot
$Version = (python deploy/version.py check VERSION).Trim()
if ($LASTEXITCODE) { throw 'VERSION refused' }
$oldGoos = $env:GOOS; $oldGoarch = $env:GOARCH; $oldCgo = $env:CGO_ENABLED
try {
 pnpm build
 if ($LASTEXITCODE) { throw 'Frontend build failed' }
 $releaseDir = Join-Path $repoRoot "dist/migate-$Version-linux-amd64"
 New-Item -ItemType Directory -Force $releaseDir | Out-Null
 $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
 $sourceRevision = (git rev-parse --short=12 HEAD).Trim() + '-working-tree'
 $linkFlags = "-s -w -X github.com/imzyb/MiGate/internal/buildinfo.Version=$Version -X github.com/imzyb/MiGate/internal/buildinfo.Revision=$sourceRevision"
 foreach ($app in @('control','gate','manage')) {
  go build -trimpath -ldflags $linkFlags -o (Join-Path $releaseDir "migate-$app") "./apps/$app"
  if ($LASTEXITCODE) { throw "$app compilation failed" }
 }
 Copy-Item deploy/install.sh,deploy/sources.json,deploy/github-update.sh,deploy/release_fetch.py,deploy/version.py $releaseDir
 python deploy/package-cores.py $releaseDir
 if ($LASTEXITCODE) {throw 'Pinned core packaging failed'}
 New-Item -ItemType Directory -Force (Join-Path $releaseDir 'third_party') | Out-Null
 Copy-Item third_party/* (Join-Path $releaseDir 'third_party') -Recurse -Force
 Copy-Item packages/ui/LICENSE (Join-Path $releaseDir 'UIAble-LICENSE')
 [IO.File]::WriteAllText((Join-Path $releaseDir 'VERSION'), $Version + "`n")
 $fingerprint = (python deploy/source_fingerprint.py).Trim()
 if ($LASTEXITCODE) { throw 'Source fingerprint failed' }
 @{version=$Version; batch=$Batch; source_fingerprint=$fingerprint; built_at=[DateTime]::UtcNow.ToString('o')} | ConvertTo-Json | Set-Content -Encoding utf8NoBOM (Join-Path $releaseDir 'BUILD.json')
 # Ship corresponding modified source and lockfiles alongside the LGPL modules.
 # Evidence screenshots and historical snapshots remain in the workspace/report.
 # They are not corresponding source and otherwise grow every binary release.
 tar -czf (Join-Path $releaseDir 'source.tar.gz') --exclude=node_modules --exclude=__pycache__ --exclude=docs/evidence -C $repoRoot apps internal ent packages services deploy docs third_party go.mod go.sum go.work go.work.sum package.json pnpm-lock.yaml pnpm-workspace.yaml AGENTS.md .gitignore .gitattributes
 if ($LASTEXITCODE) { throw 'Source packaging failed' }
 $entries = Get-ChildItem $releaseDir -File -Recurse | Where-Object Name -ne 'SHA256SUMS' | Sort-Object FullName | ForEach-Object {
  $relative = $_.FullName.Substring($releaseDir.Length + 1).Replace('\','/')
  "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower())  $relative"
 }
 [IO.File]::WriteAllText((Join-Path $releaseDir 'SHA256SUMS'), ($entries -join "`n") + "`n")
 $archive = Join-Path $repoRoot "dist/migate-$Version-linux-amd64.tar.gz"
 tar -czf $archive -C $releaseDir .
 if ($LASTEXITCODE) { throw 'Packaging failed' }
 Get-FileHash $archive -Algorithm SHA256
} finally { $env:GOOS=$oldGoos; $env:GOARCH=$oldGoarch; $env:CGO_ENABLED=$oldCgo; Pop-Location }
