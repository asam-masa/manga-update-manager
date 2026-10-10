# CDPの接続前提だけを確認する。PlaywrightのE2Eテストではない。
[CmdletBinding()]
param(
    [string]$Executable = 'build/bin/issue35-check.exe',
    [ValidateRange(1, 60)][int]$TimeoutSeconds = 15
)

$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'Windows専用の検証です。PowerShell 7で実行してください。' }
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$exePath = (Resolve-Path -LiteralPath (Join-Path $repoRoot $Executable)).Path
if (-not $exePath.StartsWith((Join-Path $repoRoot 'build/bin/'), [StringComparison]::OrdinalIgnoreCase)) {
    throw 'build/bin内の確認専用exeだけを指定してください。'
}
$runRoot = Join-Path $repoRoot ('build/bin/webview2-spike-' + [guid]::NewGuid().ToString('N'))
$appData = Join-Path $runRoot 'appdata'
[IO.Directory]::CreateDirectory($appData) | Out-Null

# 空きポートの確認後に解放するため、小さな競合余地は残る。並列実行しない。
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = $listener.LocalEndpoint.Port
$listener.Stop()
$endpoint = "http://127.0.0.1:$port/json/version"
$startInfo = [Diagnostics.ProcessStartInfo]::new($exePath)
$startInfo.UseShellExecute = $false
$startInfo.WorkingDirectory = $repoRoot
$startInfo.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
$startInfo.Environment['APPDATA'] = $appData
$startInfo.Environment['WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS'] = "--remote-debugging-port=$port"
# WailsのGo loaderではこの変数は消去される。実際の分離はAPPDATAで行う。
$startInfo.Environment['WEBVIEW2_USER_DATA_FOLDER'] = Join-Path $runRoot 'requested-webview2'

$process = $null
$cdpReady = $false
$aliveAtEnd = $false
$dbCreated = $false
$profileCreated = $false
try {
    $process = [Diagnostics.Process]::Start($startInfo)
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $deadline -and -not $process.HasExited) {
        try {
            $version = Invoke-RestMethod -Uri $endpoint -TimeoutSec 1 -NoProxy
            if ($version.webSocketDebuggerUrl) {
                # ポート解放後に別のアプリが取得しても、そちらを成功と扱わない。
                $owners = @(Get-NetTCPConnection -LocalPort $port -State Listen |
                    ForEach-Object { Get-CimInstance Win32_Process -Filter "ProcessId = $($_.OwningProcess)" } |
                    Where-Object { $_.CommandLine -and $_.CommandLine.Contains($runRoot, [StringComparison]::OrdinalIgnoreCase) })
                if ($owners.Count -gt 0) { $cdpReady = $true; break }
            }
        } catch {
            # 接続拒否もタイムアウトも、期限内は接続口の未準備として扱う。
        }
        Start-Sleep -Milliseconds 250
    }
    $aliveAtEnd = -not $process.HasExited
    $dbCreated = @(Get-ChildItem -LiteralPath $appData -Filter manga.sqlite -Recurse).Count -eq 1
    $profileCreated = Test-Path -LiteralPath (Join-Path $appData ([IO.Path]::GetFileName($exePath)))
} finally {
    if ($null -ne $process -and -not $process.HasExited) {
        $null = $process.CloseMainWindow()
        if (-not $process.WaitForExit(5000)) {
            # この呼び出しで作成したプロセスとその子だけを終了する。
            $process.Kill($true)
            $process.WaitForExit()
        }
    }
    if ($null -ne $process) { $process.Dispose() }
}

# 他のアプリには触れず、この実行専用のprofileパスを持つ残存プロセスだけを数える。
$remaining = @()
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    $remaining = @(Get-CimInstance Win32_Process -Filter "Name = 'msedgewebview2.exe'" |
        Where-Object { $_.CommandLine -and $_.CommandLine.Contains($runRoot, [StringComparison]::OrdinalIgnoreCase) })
    if ($remaining.Count -eq 0) { break }
    Start-Sleep -Milliseconds 250
}
[pscustomobject]@{
    cdpReachable = $cdpReady
    appAliveAtProbeEnd = $aliveAtEnd
    isolatedDatabaseCreated = $dbCreated
    isolatedWebViewProfileCreated = $profileCreated
    remainingOwnedWebViewProcesses = $remaining.Count
    artifacts = $runRoot
    timeoutSeconds = $TimeoutSeconds
} | ConvertTo-Json

# 接続不可もSpikeの有効な結果。起動・分離・終了に問題がある場合だけ非0で返す。
if (-not $aliveAtEnd -or -not $dbCreated -or -not $profileCreated -or $remaining.Count -gt 0) { exit 2 }
