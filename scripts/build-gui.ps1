$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$GuiDir = Join-Path $Root "aicw-node-gui"
$DistDir = Join-Path $Root "dist"
$Goos = "windows"
$Goarch = "amd64"

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

$NodeLocal = Join-Path $GuiDir "aicw-node.exe"
$SetupDist = Join-Path $DistDir "aicw-node-setup-windows-amd64.exe"
$SetupLocal = Join-Path $DistDir "aicw-node-setup.exe"
$EngineDist = Join-Path $DistDir "aicw-node.exe"
$ZipDist = Join-Path $DistDir "aicw-node-setup-windows-amd64.zip"

Write-Host "Building aicw-node.exe..."
Push-Location $Root
$env:GOOS = $Goos
$env:GOARCH = $Goarch
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o $NodeLocal ./cmd/aicw-node
Pop-Location

Write-Host "Generating Wails bindings..."
Push-Location $GuiDir
if (Get-Command wails -ErrorAction SilentlyContinue) {
  wails generate module
} else {
  go run github.com/wailsapp/wails/v2/cmd/wails@v2.10.1 generate module
}
go mod tidy
Pop-Location

Write-Host "Building aicw-node-setup.exe..."
Push-Location $GuiDir
$env:CGO_ENABLED = "1"
$env:GOOS = $Goos
$env:GOARCH = $Goarch
go build -tags production -trimpath -ldflags="-H windowsgui -s -w" -o $SetupDist .
Pop-Location

Copy-Item $NodeLocal $EngineDist -Force
Copy-Item $SetupDist $SetupLocal -Force

if (Test-Path $ZipDist) { Remove-Item $ZipDist -Force }
Compress-Archive -Path $SetupDist, $EngineDist -DestinationPath $ZipDist -Force

Write-Host "Done:"
Write-Host "  $ZipDist"
Write-Host "  $SetupLocal"
Write-Host "  $EngineDist"
