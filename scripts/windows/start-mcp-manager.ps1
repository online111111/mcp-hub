param(
    [switch]$NoBrowser,
    [switch]$ExitAfterReady
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$exe = Join-Path $PSScriptRoot 'mcp-manager.exe'
$config = Join-Path $PSScriptRoot 'config.json'

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Content
    )
    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function Try-CopyAdminToken {
    param([Parameter(Mandatory = $true)][string]$Token)
    try {
        Set-Clipboard -Value $Token
        Write-Host 'Admin Token copied to the Windows clipboard. Paste it into the Admin login page.'
        return $true
    } catch {
        Write-Warning 'Could not access the Windows clipboard. Run copy-admin-token.cmd later, or read hub.admin.token from config.json.'
        return $false
    }
}

if (-not (Test-Path $exe)) {
    throw 'mcp-manager.exe was not found next to the launcher.'
}

$createdConfig = $false
if (-not (Test-Path $config)) {
    Write-Host 'First run: creating a local-only MCP Manager configuration...'
    $token = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
    $cfg = [ordered]@{
        version = 1
        hub = [ordered]@{
            listen = '127.0.0.1:8080'
            admin = [ordered]@{
                enabled = $true
                token = $token
                sessionTimeout = '30m'
            }
        }
        defaults = [ordered]@{
            startupTimeout = '20s'
            callTimeout = '60s'
            maxConcurrency = 8
        }
        mcpServers = [ordered]@{}
    }
    Write-Utf8NoBom -Path $config -Content ($cfg | ConvertTo-Json -Depth 8)
    $createdConfig = $true
    Write-Host 'Created config.json with a random local Admin token (UTF-8 without BOM).'
    [void](Try-CopyAdminToken -Token $token)
} else {
    $bytes = [System.IO.File]::ReadAllBytes($config)
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
        $text = [System.Text.Encoding]::UTF8.GetString($bytes, 3, $bytes.Length - 3)
        Write-Utf8NoBom -Path $config -Content $text
        Write-Host 'Normalized existing config.json from UTF-8 BOM to UTF-8 without BOM.'
    }
}

& $exe validate --config $config
if ($LASTEXITCODE -ne 0) {
    throw "Configuration validation failed with exit code $LASTEXITCODE."
}

Write-Host 'Starting MCP Manager...'
$quotedConfig = '"' + $config + '"'
$process = Start-Process -FilePath $exe -ArgumentList @('serve', '--config', $quotedConfig) -PassThru -NoNewWindow

try {
    $ready = $false
    for ($i = 0; $i -lt 100; $i++) {
        if ($process.HasExited) {
            throw "MCP Manager exited during startup with code $($process.ExitCode)."
        }
        try {
            $response = Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:8080/readyz' -TimeoutSec 1
            if ($response.StatusCode -eq 200) {
                $ready = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 100
    }

    if (-not $ready) {
        throw 'MCP Manager did not become ready within 10 seconds.'
    }

    $admin = Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:8080/admin/' -TimeoutSec 2
    if ($admin.StatusCode -ne 200) {
        throw "Admin UI returned HTTP $($admin.StatusCode)."
    }

    if ($ExitAfterReady) {
        Write-Host 'Windows launcher smoke test passed.'
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        exit 0
    }

    if ($createdConfig) {
        Write-Host 'First-run Admin Token is in your clipboard. If needed later, double-click copy-admin-token.cmd.'
    }

    if (-not $NoBrowser) {
        Write-Host 'Opening Admin UI: http://127.0.0.1:8080/admin/'
        Start-Process 'http://127.0.0.1:8080/admin/'
    } else {
        Write-Host 'Admin UI: http://127.0.0.1:8080/admin/'
    }

    Wait-Process -Id $process.Id
    exit $process.ExitCode
} catch {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    }
    Write-Error $_
    exit 1
}
