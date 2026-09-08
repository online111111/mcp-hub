$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$config = Join-Path $PSScriptRoot 'config.json'
if (-not (Test-Path $config)) {
    throw 'config.json was not found. Start MCP Manager once first.'
}

$text = [System.IO.File]::ReadAllText($config, [System.Text.Encoding]::UTF8)
if ($text.Length -gt 0 -and $text[0] -eq [char]0xFEFF) {
    $text = $text.Substring(1)
}

try {
    $cfg = $text | ConvertFrom-Json
} catch {
    throw 'config.json is not valid JSON.'
}

$token = [string]$cfg.hub.admin.token
if ([string]::IsNullOrWhiteSpace($token)) {
    throw 'No inline hub.admin.token was found in config.json. This helper only exposes the local Windows first-run token.'
}

Set-Clipboard -Value $token
Write-Host 'Admin Token copied to the Windows clipboard.'
Write-Host 'Return to http://127.0.0.1:8080/admin/ and paste it into the Admin Token field.'
