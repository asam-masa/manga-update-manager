[CmdletBinding()]
param(
    [ValidateSet('production','development')][string]$Mode = 'production',
    [ValidateRange(10,120)][int]$TimeoutSeconds = 40
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'PowerShell 7・Windows専用です。' }
# Fail before starting a GUI if process ownership cannot be inspected.
$null = Get-CimInstance Win32_Process -Filter 'ProcessId = 0'
if ($Mode -eq 'development') {
    foreach ($port in @(34115,34116)) {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port)
        try { $listener.Start() } finally { $listener.Stop() }
    }
}
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$artifacts = Join-Path $repository ('build/bin/image-delivery-run-' + [guid]::NewGuid().ToString('N'))
$null = [IO.Directory]::CreateDirectory($artifacts)
$profile = Join-Path $artifacts 'appdata'
$temporary = Join-Path $artifacts 'temporary'
$null = [IO.Directory]::CreateDirectory($profile)
$null = [IO.Directory]::CreateDirectory($temporary)
if ($Mode -eq 'production') {
    $command = Join-Path $repository 'build/bin/image-delivery-build/bin/image-delivery-spike.exe'
    if (!(Test-Path -LiteralPath $command)) { throw '検証用exeを先にビルドしてください。' }
} else {
    $command = (Get-Command wails -CommandType Application).Source
}
$start = [Diagnostics.ProcessStartInfo]::new($command)
$start.UseShellExecute = $false
$start.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
$start.WorkingDirectory = $PSScriptRoot
$start.Environment['APPDATA'] = $profile
$start.Environment['TEMP'] = $temporary
$start.Environment['TMP'] = $temporary
if ($Mode -eq 'development') {
    $start.ArgumentList.Add('dev')
    $start.ArgumentList.Add('-m')
    $start.ArgumentList.Add('-nosyncgomod')
}
# Do not inherit the caller's development-server override.
$null = $start.Environment.Remove('WAILS_ASSETDIR')
$null = $start.Environment.Remove('WAILS_DEVSERVER')
$process = [Diagnostics.Process]::Start($start)
$owned = @{}
$owned[$process.Id] = $process
function Update-OwnedProcesses {
    $snapshot = @(Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,CreationDate)
    do {
        $added = $false
        foreach ($candidate in $snapshot) {
            if ($owned.ContainsKey([int]$candidate.ProcessId) -or !$owned.ContainsKey([int]$candidate.ParentProcessId)) { continue }
            $parent = $owned[[int]$candidate.ParentProcessId]
            if ($parent.HasExited -or $candidate.CreationDate -lt $parent.StartTime) { continue }
            try {
                $child = [Diagnostics.Process]::GetProcessById([int]$candidate.ProcessId)
                # Keep a handle to this incarnation, not merely a reusable PID.
                $null = $child.Handle
                if ([Math]::Abs(($child.StartTime - $candidate.CreationDate).TotalSeconds) -gt 1) { $child.Dispose(); continue }
                $owned[$child.Id] = $child
                $added = $true
            } catch [ArgumentException] { } # Child exited before it could be captured.
        }
    } while ($added)
}
$result = $null
try {
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $deadline -and -not $process.HasExited) {
        Update-OwnedProcesses
        $reports = @(Get-ChildItem -LiteralPath $temporary -Filter result.json -Recurse -File)
        if ($reports.Count -eq 1) {
            try { $result = Get-Content -LiteralPath $reports[0].FullName -Raw | ConvertFrom-Json; break }
            catch { } # The typed report may still be being written.
        }
        Start-Sleep -Milliseconds 250
    }
} finally {
    if (-not $process.HasExited) {
        $null = $process.CloseMainWindow()
        if (-not $process.WaitForExit(3000)) { $process.Kill($true); $process.WaitForExit() }
    }
    # wails dev may exit before its npm/Vite children. Retained handles identify
    # only descendants observed while their parent was still owned and alive.
    foreach ($child in $owned.Values) {
        if (-not $child.HasExited) { $child.Kill($true); $child.WaitForExit() }
        $child.Dispose()
    }
}
[pscustomobject]@{ mode=$Mode; observations=$result; artifacts=$artifacts } | ConvertTo-Json -Depth 4
if ($null -eq $result) { exit 2 }
$keys = @('initial','replacement','missing','broken','unset','methodRejected','encodedRejected','unknownRejected','noStore')
foreach ($key in $keys) { if ($result.$key -ne $true) { exit 2 } }
