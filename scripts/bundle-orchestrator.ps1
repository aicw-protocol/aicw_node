# Bundle Linux orchestrator + configs for VPS upload.
# Usage (from aicw_node repo root):
#   .\scripts\bundle-orchestrator.ps1
# Then scp deployments/orchestrator-bundle/* to VPS and run install-orchestrator-on-vps.sh

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$Bundle = Join-Path $Root "deployments\orchestrator-bundle"
$Secrets = Join-Path $Root ".secrets"

New-Item -ItemType Directory -Force -Path $Bundle | Out-Null

# Cross-compile Linux binary
Push-Location $Root
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -o (Join-Path $Bundle "reshare-orchestrator") ./cmd/reshare-orchestrator
Pop-Location

Copy-Item (Join-Path $Root "deployments\orchestrator\network-config.yaml") $Bundle -Force
Copy-Item (Join-Path $Root "deployments\orchestrator\orchestrator-config.yaml") $Bundle -Force
Copy-Item (Join-Path $Root "deployments\orchestrator\reshare-orchestrator.service") $Bundle -Force

$KeySrc = Join-Path $Secrets "reshare_initiator.key"
if (-not (Test-Path $KeySrc)) {
  Write-Error "Missing $KeySrc — run mpcium-cli generate-initiator first."
}
Copy-Item $KeySrc $Bundle -Force

Write-Host "Bundle ready: $Bundle"
Get-ChildItem $Bundle | Format-Table Name, Length

Write-Host ""
Write-Host "Upload to VPS (replace USER and HOST):"
Write-Host "  scp -r $Bundle USER@158.247.251.191:/tmp/orchestrator-bundle"
Write-Host "  ssh USER@158.247.251.191 'sudo BUNDLE_DIR=/tmp/orchestrator-bundle bash /path/to/install-orchestrator-on-vps.sh'"
