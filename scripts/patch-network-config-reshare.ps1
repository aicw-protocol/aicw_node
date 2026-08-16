# Add reshare_initiator_pubkey to each node's network-config.yaml and remind to restart.
# Usage: .\scripts\patch-network-config-reshare.ps1 -ConfigPath "C:\path\to\node\dir\network-config.yaml"

param(
  [Parameter(Mandatory = $true)]
  [string]$ConfigPath
)

$PubkeyLine = 'reshare_initiator_pubkey: "50eb1f85764d23d1828cd2a274b8b21189e05ebfe13ab54264a803e2e8d76232"'

if (-not (Test-Path $ConfigPath)) {
  Write-Error "Not found: $ConfigPath"
}

$content = Get-Content $ConfigPath -Raw
if ($content -match 'reshare_initiator_pubkey:') {
  Write-Host "Already patched: $ConfigPath"
  exit 0
}

if ($content -match '(?m)^event_initiator_pubkey:.*$') {
  $content = [regex]::Replace(
    $content,
    '(?m)^event_initiator_pubkey:.*$',
    "`$0`n$PubkeyLine",
    1
  )
} else {
  Write-Error "event_initiator_pubkey line not found in $ConfigPath"
}

Set-Content -Path $ConfigPath -Value $content -NoNewline
Write-Host "Patched: $ConfigPath"
Write-Host "Restart the node (GUI Stop -> Start, or restart aicw-node process)."
