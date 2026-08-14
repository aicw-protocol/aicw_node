$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$GuiDir = Join-Path $Root "aicw-node-gui"
$DistDir = Join-Path $Root "dist"
$Goos = "windows"
$Goarch = "amd64"

$InstallerDist = Join-Path $DistDir "aicw-node-setup-windows-amd64-installer.exe"
$SetupLocal = Join-Path $DistDir "aicw-node-setup.exe"
$NsisInstaller = Join-Path $GuiDir "build\bin\aicw-node-setup-amd64-installer.exe"

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

$NodeLocal = Join-Path $GuiDir "aicw-node.exe"

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

Write-Host "Building NSIS installer..."
Push-Location $GuiDir
$env:CGO_ENABLED = "1"
$env:GOOS = $Goos
$env:GOARCH = $Goarch
if (Get-Command wails -ErrorAction SilentlyContinue) {
  wails build -platform windows/amd64 -clean -skipbindings -nsis
} else {
  go run github.com/wailsapp/wails/v2/cmd/wails@v2.10.1 build -platform windows/amd64 -clean -skipbindings -nsis
}
Pop-Location

if (-not (Test-Path $NsisInstaller)) {
  throw "NSIS installer was not produced at $NsisInstaller"
}

Copy-Item $NsisInstaller $InstallerDist -Force
Copy-Item $InstallerDist $SetupLocal -Force

Write-Host "Done:"
Write-Host "  $InstallerDist"
Write-Host "  $SetupLocal"
