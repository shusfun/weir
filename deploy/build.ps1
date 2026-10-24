# 本机打包 linux/amd64 发布包，步骤与 .github/workflows/release.yml 一致：
# 构建控制台 → CGO_ENABLED=0 交叉编译单个 migate → 复制安装 / 更新脚本 → package_release.py 生成校验和与归档。
# 内核不随发布包分发：由中心从上游拉取（或用 migate kernel import 离线导入）后推送给 Gate。
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
 if (Test-Path $releaseDir) { Remove-Item -Recurse -Force $releaseDir }
 New-Item -ItemType Directory -Force $releaseDir | Out-Null
 $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
 $revision = (git rev-parse HEAD).Trim()
 $dirty = git status --porcelain
 if ($dirty) { $revision += '-dirty' }
 $linkFlags = "-s -w -X github.com/shusfun/weir/internal/buildinfo.Version=$Version -X github.com/shusfun/weir/internal/buildinfo.Revision=$revision"
 go build -trimpath -ldflags $linkFlags -o (Join-Path $releaseDir 'migate') './apps/weir'
 if ($LASTEXITCODE) { throw 'migate compilation failed' }
 Copy-Item deploy/install.sh,deploy/sources.json,deploy/github-update.sh,deploy/release_fetch.py,deploy/version.py,VERSION $releaseDir
 python deploy/package_release.py $Version
 if ($LASTEXITCODE) { throw 'Packaging failed' }
 Get-FileHash (Join-Path $repoRoot "dist/migate-$Version-linux-amd64.tar.gz") -Algorithm SHA256
} finally { $env:GOOS=$oldGoos; $env:GOARCH=$oldGoarch; $env:CGO_ENABLED=$oldCgo; Pop-Location }
