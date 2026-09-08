param(
    [switch]$NoBrowser,
    [switch]$ExitAfterReady
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$exe = Join-Path $PSScriptRoot 'mcp-manager.exe'
$config = Join-Path $PSScriptRoot 'config.json'
$process = $null
$processStarted = $false

function Write-NewUtf8Config {
    param([string]$Path, [string]$Content)
    # Never overwrite a config created by another launcher in the meantime.
    $stream = [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
    try {
        $bytes = (New-Object System.Text.UTF8Encoding($false)).GetBytes($Content)
        $stream.Write($bytes, 0, $bytes.Length)
        $stream.Flush($true)
    } finally { $stream.Dispose() }
}

try {
    if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) {
        throw 'mcp-manager.exe was not found next to the launcher.'
    }

    $copiedToken = $false
    if (-not (Test-Path -LiteralPath $config)) {
        Write-Host 'First run: creating a local-only MCP Manager configuration...'
        $token = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
        $cfg = [ordered]@{
            version = 1
            hub = [ordered]@{
                listen = '127.0.0.1:8080'
                admin = [ordered]@{ enabled = $true; token = $token; sessionTimeout = '30m' }
            }
            defaults = [ordered]@{ startupTimeout = '20s'; callTimeout = '60s'; maxConcurrency = 8 }
            mcpServers = [ordered]@{}
        }
        Write-NewUtf8Config -Path $config -Content ($cfg | ConvertTo-Json -Depth 8)
        Write-Host 'Created config.json with a random Admin token (UTF-8 without BOM).'
        try {
            Set-Clipboard -Value $token
            $copiedToken = $true
        } catch {
            Write-Warning 'Clipboard unavailable. Use copy-admin-token.cmd later or read hub.admin.token from config.json.'
        }
        $token = $null
    }

    # The binary supports an existing UTF-8 BOM. Do not rewrite user config just
    # to normalize its encoding: another running instance may be updating it.
    & $exe validate --config $config
    if ($LASTEXITCODE -ne 0) { throw "Configuration validation failed with exit code $LASTEXITCODE." }
    $settings = [System.IO.File]::ReadAllText($config, [System.Text.Encoding]::UTF8) | ConvertFrom-Json
    $listen = [string]$settings.hub.listen
    if ([string]::IsNullOrWhiteSpace($listen)) { $listen = '127.0.0.1:8080' }
    $endpoint = [uri]('http://' + $listen)
    if (-not $endpoint.IsLoopback -or $endpoint.Port -lt 1 -or $settings.hub.publicMode) {
        throw 'This launcher is for local loopback use. For a public/server deployment, run mcp-manager.exe serve --config config.json from a terminal.'
    }
    if (-not $settings.hub.admin.enabled) {
        throw 'The desktop launcher requires hub.admin.enabled=true. Use the serve command for a headless instance.'
    }
    $baseUrl = $endpoint.GetLeftPart([System.UriPartial]::Authority)
    $adminUrl = $baseUrl + '/admin/'

    Write-Host ('Starting MCP Manager on ' + $baseUrl + ' ...')
    # Windows PowerShell 5.1 Start-Process resolves its working directory as
    # a wildcard path, even when omitted. Use literal .NET process properties
    # so installation directories containing brackets remain valid.
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo.FileName = $exe
    $process.StartInfo.Arguments = 'serve --config "' + $config + '"'
    $process.StartInfo.WorkingDirectory = $PSScriptRoot
    $process.StartInfo.UseShellExecute = $false
    $processStarted = $process.Start()
    if (-not $processStarted) { throw 'Could not create the MCP Manager process.' }
    # Retain the process handle so ExitCode remains available after termination.
    $null = $process.Handle
    $timer = [System.Diagnostics.Stopwatch]::StartNew()
    $healthy = $false
    while ($timer.Elapsed.TotalSeconds -lt 30) {
        $process.Refresh()
        if ($process.HasExited) { throw "MCP Manager exited during startup with code $($process.ExitCode)." }
        try {
            # Liveness is intentionally independent of MCP bearer auth and
            # downstream availability. The Admin UI must stay usable to repair
            # an unavailable downstream or an existing authenticated config.
            $response = Invoke-WebRequest -UseBasicParsing ($baseUrl + '/healthz') -TimeoutSec 1 -MaximumRedirection 0
            if ($response.StatusCode -eq 200) { $healthy = $true; break }
        } catch {}
        Start-Sleep -Milliseconds 100
    }
    if (-not $healthy) { throw 'MCP Manager did not start its local HTTP service within 30 seconds.' }
    Start-Sleep -Milliseconds 200
    $process.Refresh()
    if ($process.HasExited) { throw 'MCP Manager exited after startup; check for an occupied port or invalid configuration.' }
    $admin = Invoke-WebRequest -UseBasicParsing $adminUrl -TimeoutSec 3 -MaximumRedirection 0
    if ($admin.StatusCode -ne 200) { throw 'The Admin UI did not return HTTP 200.' }

    if ($copiedToken) {
        Write-Host 'Admin Token copied to the Windows clipboard. Paste it into the Admin login page.'
    } else {
        Write-Host 'To copy the existing Admin Token, double-click copy-admin-token.cmd.'
    }
    Write-Host 'Keep config.json private. Clipboard history or other local applications may retain copied secrets.'
    Write-Host ('Admin UI: ' + $adminUrl)
    if ($ExitAfterReady) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        $process.WaitForExit()
        Write-Host 'Windows launcher smoke test passed.'
        exit 0
    }
    if (-not $NoBrowser) {
        try {
            $browserInfo = New-Object System.Diagnostics.ProcessStartInfo
            $browserInfo.FileName = $adminUrl
            $browserInfo.WorkingDirectory = $PSScriptRoot
            $browserInfo.UseShellExecute = $true
            $browser = [System.Diagnostics.Process]::Start($browserInfo)
            if ($null -ne $browser) { $browser.Dispose() }
        } catch { Write-Warning ('Could not open a browser. Open ' + $adminUrl + ' manually; the service remains running.') }
    }
    $process.WaitForExit()
    exit $process.ExitCode
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
} finally {
    if ($null -ne $process) {
        if ($processStarted -and -not $process.HasExited) { Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue }
        $process.Dispose()
    }
}
