$ErrorActionPreference='Stop'
$repoRoot=Split-Path $PSScriptRoot -Parent
$output=Join-Path $repoRoot 'dist/cores/sing-box-1.14.3-wireguard-test'
$oldWork=$env:GOWORK; $oldCgo=$env:CGO_ENABLED; $oldToolchain=$env:GOTOOLCHAIN
try {
 $env:GOTOOLCHAIN='go1.26.9'
 $source=go mod download -json github.com/sagernet/sing-box@v1.14.3 | ConvertFrom-Json
 if($LASTEXITCODE -or $source.Sum -ne 'h1:cBg8TP5SPWoMAlWqTqbSNdHGKPUciZisjQbLtqFlMZA='){throw 'Pinned client source mismatch'}
 New-Item -ItemType Directory -Force $output | Out-Null
 Push-Location $source.Dir
 try {
  $env:GOWORK='off';$env:CGO_ENABLED='0'
  go build -mod=readonly -trimpath -tags 'with_quic,with_utls,with_v2ray_api,with_wireguard,with_gvisor' -ldflags '-s -w -X github.com/sagernet/sing-box/constant.Version=1.14.3-migate.wg-test' -o (Join-Path $output 'sing-box.exe') ./cmd/sing-box
  if($LASTEXITCODE){throw 'WireGuard test client build failed'}
 }finally{Pop-Location}
 @{upstream='github.com/sagernet/sing-box';version='v1.14.3';sum=$source.Sum;tags='with_quic,with_utls,with_v2ray_api,with_wireguard,with_gvisor';role='Isolated local test client only; no production kernel replacement';sha256=(Get-FileHash (Join-Path $output 'sing-box.exe')).Hash.ToLower()} | ConvertTo-Json | Set-Content (Join-Path $output 'provenance.json')
}finally{$env:GOWORK=$oldWork;$env:CGO_ENABLED=$oldCgo;$env:GOTOOLCHAIN=$oldToolchain}
