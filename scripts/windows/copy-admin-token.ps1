$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

$config = Join-Path $PSScriptRoot 'config.json'
if (-not (Test-Path -LiteralPath $config)) {
    throw 'config.json was not found. Start MCP Manager once first.'
}

if ((Get-Item -LiteralPath $config).Length -gt 1048576) { throw 'config.json exceeds the 1 MiB size limit.' }

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

if ($token.Contains('${')) { throw 'The Admin Token uses an environment reference. Retrieve it from the configured environment, not the literal placeholder in config.json.' }

Set-Clipboard -Value $token
Write-Host 'Admin Token copied to the Windows clipboard.'
Write-Host 'Return to the Admin login page and paste the token. Clipboard history or other local applications may retain copied secrets.'
