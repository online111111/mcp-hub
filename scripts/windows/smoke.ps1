param(
    [Parameter(Mandatory = $true)][string]$ArchivePath,
    [Parameter(Mandatory = $true)][string]$ExpectedVersion
)
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'This regression must run under Windows PowerShell 5.1.'
}
Write-Host ('Windows release verification: PowerShell ' + $PSVersionTable.PSVersion)
$archive = [System.IO.Path]::GetFullPath($ArchivePath)
$root = Join-Path ([System.IO.Path]::GetTempPath()) ('mcp-manager audit ' + [guid]::NewGuid().ToString('N'))
$package = Join-Path $root ('[package] ' + [char]0x6D4B + [char]0x8BD5)
New-Item -ItemType Directory -Path $root | Out-Null
Expand-Archive -LiteralPath $archive -DestinationPath $package
$config = Join-Path $package 'config.json'
$utf8 = New-Object System.Text.UTF8Encoding($false)

function Invoke-Launcher {
    Push-Location -LiteralPath $package
    try {
        # Exercise the entrypoint users double-click, including CMD -> PS5.1.
        # Redirect stdin to avoid a failed wrapper pause blocking a CI runner.
        $output = & $env:ComSpec /d /c 'start-mcp-manager.cmd -NoBrowser -ExitAfterReady <nul' 2>&1
        $code = $LASTEXITCODE
        if ($code -ne 0) {
            $output | ForEach-Object { Write-Host $_ }
            throw ('Packaged CMD launcher failed with exit code ' + $code)
        }
        if (-not (($output | Out-String).Contains('Windows launcher smoke test passed.'))) {
            throw 'Launcher returned success without the completed smoke marker.'
        }
    } finally { Pop-Location }
}

function Write-TestConfig {
    param($Value, [bool]$Bom = $false)
    $text = $Value | ConvertTo-Json -Depth 12
    $encoding = New-Object System.Text.UTF8Encoding($Bom)
    [System.IO.File]::WriteAllText($config, $text, $encoding)
}

try {
    foreach ($required in @('mcp-manager.exe','README.md','README-WINDOWS.txt','start-mcp-manager.cmd','start-mcp-manager.ps1','copy-admin-token.cmd','copy-admin-token.ps1')) {
        if (-not (Test-Path -LiteralPath (Join-Path $package $required) -PathType Leaf)) {
            throw ('Release is missing ' + $required)
        }
    }
    $exe = Join-Path $package 'mcp-manager.exe'
    $version = & $exe version
    if ($LASTEXITCODE -ne 0 -or ($version | Out-String).Trim() -ne ('mcp-manager ' + $ExpectedVersion)) {
        throw 'Packaged version does not match the release version.'
    }
    Invoke-Launcher
    $bytes = [System.IO.File]::ReadAllBytes($config)
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
        throw 'First-run configuration has a UTF-8 BOM.'
    }
    $cfg = [System.IO.File]::ReadAllText($config, [System.Text.Encoding]::UTF8) | ConvertFrom-Json
    $adminToken = [string]$cfg.hub.admin.token
    if ($adminToken.Length -ne 64 -or (Get-Clipboard -Raw).Trim() -cne $adminToken) {
        throw 'First-run clipboard credential does not match the generated Admin Token.'
    }
    Set-Clipboard -Value 'audit-no-secret'
    Push-Location -LiteralPath $package
    try {
        & $env:ComSpec /d /c 'copy-admin-token.cmd <nul'
        if ($LASTEXITCODE -ne 0) { throw 'Packaged token-copy CMD failed.' }
    } finally { Pop-Location }
    if ((Get-Clipboard -Raw).Trim() -cne $adminToken) { throw 'Recovery helper copied the wrong credential.' }
    Write-Host 'PASS: final ZIP, CMD wrapper, first run, no BOM, clipboard recovery, spaces/Unicode/brackets in path.'

    $allocator = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
    $allocator.Start()
    $port = $allocator.LocalEndpoint.Port
    $allocator.Stop()
    $cfg.hub.listen = '127.0.0.1:' + $port
    $cfg.hub | Add-Member -NotePropertyName auth -NotePropertyValue ([ordered]@{bearerToken=('audit-mcp-' + [guid]::NewGuid().ToString('N'))})
    $cfg.mcpServers | Add-Member -NotePropertyName unavailable -NotePropertyValue ([ordered]@{type='stdio';command=(Join-Path $root 'missing-server.exe');startupTimeout='1s'})
    foreach ($bom in @($false, $true)) {
        Write-TestConfig -Value $cfg -Bom $bom
        $before = (Get-FileHash -LiteralPath $config -Algorithm SHA256).Hash
        Set-Clipboard -Value 'existing-clipboard-value'
        Invoke-Launcher
        $after = (Get-FileHash -LiteralPath $config -Algorithm SHA256).Hash
        if ($before -ne $after) { throw 'An existing user configuration was unexpectedly rewritten.' }
        if ((Get-Clipboard -Raw).Trim() -cne 'existing-clipboard-value') { throw 'Restart unexpectedly overwrote the clipboard.' }
        & $exe validate --config $config
        if ($LASTEXITCODE -ne 0) { throw 'Existing authenticated/BOM configuration failed validation.' }
    }
    Write-Host 'PASS: custom port, MCP authentication, unavailable downstream, existing BOM, config preservation.'

    $blocker = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, $port)
    $blocker.Start()
    try {
        $rejected = $false
        try { Invoke-Launcher } catch { $rejected = $true }
        if (-not $rejected) { throw 'An occupied port was incorrectly reported as a running Manager.' }
        if (-not $blocker.Server.IsBound) { throw 'Launcher disturbed a pre-existing listener.' }
    } finally { $blocker.Stop() }
    Write-Host 'PASS: occupied port fails without terminating the existing listener.'
} finally {
    Set-Clipboard -Value 'mcp-manager verification complete'
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
