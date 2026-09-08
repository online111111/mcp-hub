$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$exe = Join-Path $PSScriptRoot 'mcp-manager.exe'
$config = Join-Path $PSScriptRoot 'config.json'

if (-not (Test-Path $exe)) {
    throw 'mcp-manager.exe was not found next to the launcher.'
}

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
    $cfg | ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 $config
    Write-Host 'Created config.json with a random local Admin token.'
}

Write-Host 'Starting MCP Manager...'
$process = Start-Process -FilePath $exe -ArgumentList @('serve', '--config', $config) -PassThru -NoNewWindow

try {
    $ready = $false
    for ($i = 0; $i -lt 50; $i++) {
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
        throw 'MCP Manager did not become ready within 5 seconds.'
    }

    Write-Host 'Opening Admin UI: http://127.0.0.1:8080/admin/'
    Start-Process 'http://127.0.0.1:8080/admin/'
    Wait-Process -Id $process.Id
    exit $process.ExitCode
} catch {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    }
    Write-Error $_
    exit 1
}
