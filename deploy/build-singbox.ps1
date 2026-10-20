param()
$ErrorActionPreference='Stop'
$repoRoot=Split-Path $PSScriptRoot -Parent
$coreVersion='v1.14.3'
$coreOutput=Join-Path $repoRoot 'dist/cores/sing-box-1.14.3-stats'
$oldGoos=$env:GOOS; $oldGoarch=$env:GOARCH; $oldCgo=$env:CGO_ENABLED; $oldWork=$env:GOWORK; $oldToolchain=$env:GOTOOLCHAIN
try {
 $env:GOTOOLCHAIN='go1.26.9'
 $download=go mod download -json "github.com/sagernet/sing-box@$coreVersion" | ConvertFrom-Json
 if ($LASTEXITCODE -or $download.Sum -ne 'h1:cBg8TP5SPWoMAlWqTqbSNdHGKPUciZisjQbLtqFlMZA=') {throw 'Pinned sing-box module checksum mismatch'}
 New-Item -ItemType Directory -Force $coreOutput | Out-Null
 Push-Location $download.Dir
 try {
  $env:GOWORK='off';$env:CGO_ENABLED='0';$env:GOARCH='amd64'
  foreach($target in @('linux','windows')) {
   $env:GOOS=$target
   $binary=if($target -eq 'windows'){'sing-box.exe'}else{'sing-box'}
   go build -mod=readonly -trimpath -tags 'with_quic,with_utls,with_v2ray_api' -ldflags '-s -w -X github.com/sagernet/sing-box/constant.Version=1.14.3-migate.stats1' -o (Join-Path $coreOutput $binary) ./cmd/sing-box
   if($LASTEXITCODE){throw "sing-box $target cross compilation failed"}
  }
 } finally {Pop-Location}
 Copy-Item $download.Zip (Join-Path $coreOutput 'upstream-source.zip')
 Copy-Item (Join-Path $download.Dir 'LICENSE') $coreOutput
 Copy-Item (Join-Path $download.Dir 'go.mod'),(Join-Path $download.Dir 'go.sum') $coreOutput
 Copy-Item $PSCommandPath (Join-Path $coreOutput 'build-singbox.ps1')
 @{upstream='https://github.com/SagerNet/sing-box';version=$coreVersion;sum=$download.Sum;toolchain='go1.26.9';tags=@('with_quic','with_utls','with_v2ray_api');changes='No source changes; metering build tag enabled; version label changed';license='GPL-3.0-or-later; source, dependency checksums and build instructions included';targets=@('linux/amd64','windows/amd64');cgo=$false} | ConvertTo-Json | Set-Content (Join-Path $coreOutput 'provenance.json')
 Get-ChildItem $coreOutput -File | Where-Object Name -ne 'SHA256SUMS' | Sort-Object Name | ForEach-Object {"$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower())  $($_.Name)"} | Set-Content (Join-Path $coreOutput 'SHA256SUMS')
} finally {$env:GOOS=$oldGoos;$env:GOARCH=$oldGoarch;$env:CGO_ENABLED=$oldCgo;$env:GOWORK=$oldWork;$env:GOTOOLCHAIN=$oldToolchain}
