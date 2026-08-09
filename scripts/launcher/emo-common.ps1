# emo-common.ps1 — EmoAgent 一键启动器函数库（Windows PowerShell 5.1 + Pester 3.4）
Set-StrictMode -Version 2.0
# 本库所有 HTTP 调用都指向 127.0.0.1——禁用系统代理，避免 Clash/TUN 等代理设置劫持环回探测
[System.Net.WebRequest]::DefaultWebProxy = $null

function Get-EmoLauncherPaths {
    param([string]$ProjectDir)
    if (-not $ProjectDir) {
        # 本文件位于 <root>\scripts\launcher\ → 上两级即项目根
        $ProjectDir = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    }
    $dataDir = Join-Path $ProjectDir 'data\launcher'
    [pscustomobject]@{
        ProjectDir  = $ProjectDir
        DataDir     = $dataDir
        LogsDir     = Join-Path $dataDir 'logs'
        ConfigPath  = Join-Path $dataDir 'launcher.config.json'
        ExamplePath = Join-Path $PSScriptRoot 'launcher.config.example.json'
        CredPath    = Join-Path $dataDir 'snowluma.cred'
        StatePath   = Join-Path $dataDir 'state.json'
    }
}

function Write-EmoLog {
    param(
        [ValidateSet('INFO','WARN','ERROR')][string]$Level = 'INFO',
        [Parameter(Mandatory)][string]$Message,
        $Paths
    )
    if (-not $Paths) { $Paths = Get-EmoLauncherPaths }
    if (-not (Test-Path $Paths.LogsDir)) { New-Item -ItemType Directory -Force -Path $Paths.LogsDir | Out-Null }
    $line = '{0} [{1}] {2}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Level, $Message
    try {
        Add-Content -Path (Join-Path $Paths.LogsDir ('emo-{0}.log' -f (Get-Date -Format 'yyyyMMdd'))) -Value $line -Encoding UTF8
    } catch {
    }
    Write-Host $line
}

function Get-EmoConfigValue {
    param($Object, [Parameter(Mandatory)][string]$Path)
    $cur = $Object
    foreach ($seg in $Path.Split('.')) {
        if ($null -eq $cur) { return $null }
        if ($cur -isnot [System.Management.Automation.PSCustomObject]) { return $null }
        $prop = $cur.PSObject.Properties[$seg]
        if ($null -eq $prop) { return $null }
        $cur = $prop.Value
    }
    return $cur
}

function Get-EmoConfigInt {
    param($Object, [string]$Path, [Parameter(Mandatory)][int]$Default)
    $v = Get-EmoConfigValue $Object $Path
    if ($null -eq $v -or [string]$v -eq '') { return $Default }
    $parsed = 0
    if (-not [int]::TryParse([string]$v, [ref]$parsed)) {
        throw ('配置无效：{0} 需为整数，当前值为 "{1}"' -f $Path, $v)
    }
    return $parsed
}

function Get-EmoConfigString {
    param($Object, [string]$Path, [string]$Default)
    $v = Get-EmoConfigValue $Object $Path
    if ($null -eq $v -or [string]$v -eq '') { return $Default }
    return [string]$v
}

function Get-EmoLauncherConfig {
    param([Parameter(Mandatory)]$Paths)
    if (-not (Test-Path $Paths.ConfigPath)) {
        if (-not (Test-Path $Paths.ExamplePath)) { throw ('样例配置缺失：{0}（仓库文件被移动或删除？）' -f $Paths.ExamplePath) }
        New-Item -ItemType Directory -Force -Path $Paths.DataDir | Out-Null
        Copy-Item $Paths.ExamplePath $Paths.ConfigPath
        throw ('已生成配置文件 {0}，请核对其中 qq.exe_path 等路径后重新运行。' -f $Paths.ConfigPath)
    }
    try { $cfg = Get-Content -Raw -Encoding UTF8 $Paths.ConfigPath | ConvertFrom-Json }
    catch { throw ('配置文件 JSON 解析失败：{0}（注意 Windows 路径中的反斜杠需写成 \\）：{1}' -f $Paths.ConfigPath, $_.Exception.Message) }
    foreach ($required in @('snowluma.dir','snowluma.base_url','qq.exe_path','qq.bot_uin','emoagent.project_dir','emoagent.base_url')) {
        $v = Get-EmoConfigValue $cfg $required
        if ($null -eq $v -or ([string]$v).Trim() -eq '') { throw ('配置无效：{0} 缺失（{1}）' -f $required, $Paths.ConfigPath) }
    }
    foreach ($key in @('snowluma.base_url','emoagent.base_url')) {
        $u = [string](Get-EmoConfigValue $cfg $key)
        if ($u -notmatch '^https?://') { throw ('配置无效：{0} 需以 http:// 或 https:// 开头（{1}）' -f $key, $Paths.ConfigPath) }
    }
    foreach ($mustExist in @('snowluma.dir','qq.exe_path','emoagent.project_dir')) {
        $p = [string](Get-EmoConfigValue $cfg $mustExist)
        if (-not (Test-Path $p)) { throw ('配置无效：{0} 指向的路径不存在：{1}' -f $mustExist, $p) }
    }
    return $cfg
}

function Read-EmoState {
    param([Parameter(Mandatory)]$Paths)
    if (-not (Test-Path $Paths.StatePath)) { return [pscustomobject]@{} }
    try {
        $raw = Get-Content -Raw -Encoding UTF8 -ErrorAction Stop $Paths.StatePath
        if ([string]::IsNullOrWhiteSpace($raw)) { return [pscustomobject]@{} }
        $obj = $raw | ConvertFrom-Json
        if ($obj -isnot [System.Management.Automation.PSCustomObject]) { return [pscustomobject]@{} }
        return $obj
    } catch { return [pscustomobject]@{} }
}

function Save-EmoState {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)][hashtable]$Set)
    $state = Read-EmoState $Paths
    foreach ($k in $Set.Keys) {
        if ($state.PSObject.Properties[$k]) { $state.$k = $Set[$k] }
        else { $state | Add-Member -NotePropertyName $k -NotePropertyValue $Set[$k] }
    }
    if ($state.PSObject.Properties['updated_at']) { $state.updated_at = (Get-Date -Format o) }
    else { $state | Add-Member -NotePropertyName 'updated_at' -NotePropertyValue (Get-Date -Format o) }
    New-Item -ItemType Directory -Force -Path $Paths.DataDir | Out-Null
    $tmp = '{0}.{1}.tmp' -f $Paths.StatePath, [guid]::NewGuid().ToString('N')
    try {
        $state | ConvertTo-Json -Depth 5 | Out-File -Encoding utf8 $tmp
        Move-Item -Force $tmp $Paths.StatePath -ErrorAction Stop
    } finally {
        if (Test-Path $tmp) { Remove-Item -Force $tmp -ErrorAction SilentlyContinue }
    }
}

function Resolve-EmoKillTarget {
    # 返回可安全终止的进程条目，任何一项无法证明归属即返回 $null（绝不猜杀）
    # ProcessTable 条目形状：@{ ProcessId; Name; CommandLine }，可选 CreationDate（配合 -ExpectedStartTime 校验）
    param(
        $ProcessId,
        [Parameter(Mandatory)][string]$ExpectedName,
        [string]$CommandLineMatch,
        [Nullable[datetime]]$ExpectedStartTime,
        $ProcessTable
    )
    $parsedPid = 0
    if ($null -eq $ProcessId -or -not [int]::TryParse([string]$ProcessId, [ref]$parsedPid)) { return $null }
    if ($parsedPid -le 0) { return $null }
    if ($null -eq $ProcessTable) {
        $ProcessTable = Get-CimInstance Win32_Process | Select-Object ProcessId, Name, CommandLine, CreationDate
    }
    $entry = $ProcessTable | Where-Object { $_.ProcessId -eq $parsedPid } | Select-Object -First 1
    if (-not $entry) { return $null }
    if ($entry.Name -ne $ExpectedName -and $entry.Name -ne ($ExpectedName + '.exe')) { return $null }
    if ($CommandLineMatch) {
        if (-not $entry.CommandLine) { return $null }
        if ($entry.CommandLine -notlike ('*' + $CommandLineMatch + '*')) { return $null }
    }
    if ($null -ne $ExpectedStartTime) {
        $prop = $entry.PSObject.Properties['CreationDate']
        if ($null -eq $prop -or $null -eq $prop.Value) { return $null }
        $delta = [math]::Abs(([datetime]$prop.Value - [datetime]$ExpectedStartTime).TotalSeconds)
        if ($delta -gt 2) { return $null }
    }
    return $entry
}

function Set-SnowLumaPassword {
    param([Parameter(Mandatory)]$Paths)
    New-Item -ItemType Directory -Force -Path $Paths.DataDir | Out-Null
    $sec = Read-Host -Prompt '请输入 SnowLuma 控制台密码（DPAPI 加密后仅保存在本机）' -AsSecureString
    if ($null -eq $sec -or $sec.Length -eq 0) { throw '密码不能为空：未写入凭据，请重新运行并输入密码。' }
    $sec | Export-Clixml -Path $Paths.CredPath
}

function Get-SnowLumaPassword {
    # 返回明文密码字符串——切勿裸调用（未赋值时会回显到控制台/transcript）
    param([Parameter(Mandatory)]$Paths, [switch]$AllowPrompt)
    if ((Test-Path $Paths.CredPath) -and (Get-Item $Paths.CredPath).Length -eq 0) {
        Remove-Item -Force $Paths.CredPath -ErrorAction SilentlyContinue
        if (Test-Path $Paths.CredPath) { Write-EmoLog -Level WARN -Message ('凭据文件删除失败（可能被占用）：{0}' -f $Paths.CredPath) -Paths $Paths }
        else { Write-EmoLog -Level WARN -Message ('0 字节凭据文件已删除，请重新录入：{0}' -f $Paths.CredPath) -Paths $Paths }
    }
    if (-not (Test-Path $Paths.CredPath)) {
        if (-not $AllowPrompt) { return $null }
        Set-SnowLumaPassword -Paths $Paths
    }
    try { $sec = Import-Clixml -Path $Paths.CredPath -ErrorAction Stop }
    catch { throw ('凭据文件无法读取（可能损坏，或由其他 Windows 用户/机器加密）：{0}。请删除该文件后重新运行以重新录入。原因：{1}' -f $Paths.CredPath, $_.Exception.Message) }
    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    try { $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
    if ($plain -eq '') {
        Remove-Item -Force $Paths.CredPath -ErrorAction SilentlyContinue
        if (Test-Path $Paths.CredPath) { Write-EmoLog -Level WARN -Message ('凭据文件删除失败（可能被占用）：{0}' -f $Paths.CredPath) -Paths $Paths }
        else { Write-EmoLog -Level WARN -Message ('凭据解码为空，已删除以便重新录入：{0}' -f $Paths.CredPath) -Paths $Paths }
        if (-not $AllowPrompt) { return $null }
        Set-SnowLumaPassword -Paths $Paths
        return (Get-SnowLumaPassword -Paths $Paths)
    }
    return $plain
}

function Wait-EmoCondition {
    param(
        [Parameter(Mandatory)][scriptblock]$Probe,
        [int]$TimeoutSeconds = 30,
        [int]$IntervalSeconds = 2,
        [string]$Label = 'condition',
        [switch]$ThrowOnProbeError,
        $Paths
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = $null
    while ($true) {
        try { $result = & $Probe }
        catch {
            if ($ThrowOnProbeError) { throw }
            $lastError = $_; $result = $null
        }
        if ($result) { return $result }
        $remaining = ($deadline - (Get-Date)).TotalSeconds
        if ($remaining -le 0) { break }
        if ($IntervalSeconds -gt 0) {
            Start-Sleep -Seconds ([Math]::Min($IntervalSeconds, [Math]::Ceiling($remaining)))
        }
    }
    if ($lastError) {
        Write-EmoLog -Level WARN -Message ('等待 {0} 超时，最后一次探测错误：{1}' -f $Label, $lastError.Exception.Message) -Paths $Paths
    }
    return $null
}

function Test-EmoHttp {
    param([Parameter(Mandatory)][string]$Url, [int]$TimeoutSec = 3)
    try {
        $null = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec $TimeoutSec -ErrorAction Stop
        return $true
    } catch {
        # 401/404 等 HTTP 错误说明服务在监听；连接拒绝/超时才算不在
        if ($_.Exception.PSObject.Properties['Response'] -and $_.Exception.Response -is [System.Net.HttpWebResponse]) { return $true }
        return $false
    }
}

function Expand-EmoListResponse {
    param($Resp)
    if ($null -eq $Resp) { return @() }
    if ($Resp -is [array]) { return $Resp }
    if ($Resp -is [System.Management.Automation.PSCustomObject]) {
        foreach ($f in 'items','list','processes','instances','data') {
            $prop = $Resp.PSObject.Properties[$f]
            if ($null -ne $prop -and $prop.Value -is [array]) { return $prop.Value }
        }
        foreach ($f in 'items','list','processes','instances','data') {
            $prop = $Resp.PSObject.Properties[$f]
            if ($null -ne $prop) {
                if ($null -eq $prop.Value) { return @() }
                return @($prop.Value)
            }
        }
    }
    return @($Resp)
}

function ConvertTo-EmoQQEntry {
    param($Raw)
    $uin = $null
    foreach ($f in 'uin','selfId','self_id','account','qq') {
        $v = Get-EmoConfigValue $Raw $f
        # SnowLuma 用 uin="0" 表示该进程槽位尚未关联账号（实测确认），不是真实 UIN
        if ($null -ne $v -and [string]$v -ne '' -and [string]$v -ne '0') { $uin = [string]$v; break }
    }
    if ($null -eq $uin -and $null -ne $Raw -and $Raw -isnot [System.Management.Automation.PSCustomObject] -and $Raw -isnot [hashtable]) {
        $uin = [string]$Raw
    }
    $status = [string](Get-EmoConfigString $Raw 'status' '')
    $onlineFlag = Get-EmoConfigValue $Raw 'online'
    $isPSObject = $Raw -is [System.Management.Automation.PSCustomObject]
    $hasStatusOrOnlineField = $isPSObject -and (($null -ne $Raw.PSObject.Properties['status']) -or ($null -ne $Raw.PSObject.Properties['online']))
    # 实测 /api/qq-list 条目形如 {"uin":"...","nickname":"..."}，完全没有 status/online 字段——
    # 出现在该列表本身即代表在线，否则会被误判为离线（emo up 曾因此永远等不到"已登录"）
    $isOnline = ($status -in @('online','connected','已在线')) -or ($onlineFlag -eq $true) -or
        ($isPSObject -and -not $hasStatusOrOnlineField -and $null -ne $uin)
    [pscustomobject]@{
        Uin    = $uin
        Online = $isOnline
        Status = $status
        Pid    = Get-EmoConfigValue $Raw 'pid'
        Raw    = $Raw
    }
}

function Test-EmoAdapterPayloadConnected {
    param($Status)
    $adapters = Get-EmoConfigValue $Status 'adapters'
    if ($null -eq $adapters) { return $false }
    foreach ($a in @($adapters)) {
        if ((Get-EmoConfigValue $a 'transport.connected') -eq $true) { return $true }
    }
    return $false
}

function Test-EmoAdapterConnected {
    param([Parameter(Mandatory)][string]$BaseUrl)
    try {
        $status = Invoke-RestMethod -Uri ($BaseUrl.TrimEnd('/') + '/api/platforms/status') -TimeoutSec 3 -ErrorAction Stop
    } catch { return $false }
    return (Test-EmoAdapterPayloadConnected $status)
}

function Start-EmoHiddenProcess {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [string[]]$ArgumentList = @(),
        [string]$WorkingDirectory,
        [string]$StdOutPath,
        [string]$StdErrPath
    )
    foreach ($rp in @($StdOutPath, $StdErrPath)) {
        if ($rp) {
            $parent = Split-Path -Parent $rp
            if ($parent -and -not (Test-Path $parent)) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
        }
    }
    $params = @{ FilePath = $FilePath; PassThru = $true; WindowStyle = 'Hidden' }
    if ($ArgumentList.Count -gt 0) { $params.ArgumentList = $ArgumentList }
    if ($WorkingDirectory) { $params.WorkingDirectory = $WorkingDirectory }
    if ($StdOutPath) { $params.RedirectStandardOutput = $StdOutPath }
    if ($StdErrPath) { $params.RedirectStandardError = $StdErrPath }
    $p = Start-Process @params -ErrorAction Stop
    if (-not $p) { throw ('进程启动失败：{0}' -f $FilePath) }
    return $p
}

function Connect-SnowLuma {
    param([Parameter(Mandatory)][string]$BaseUrl, [Parameter(Mandatory)][string]$Password)
    $base = $BaseUrl.TrimEnd('/')
    $bodyBytes = [Text.Encoding]::UTF8.GetBytes((ConvertTo-Json @{ password = $Password }))
    try {
        $resp = Invoke-RestMethod -Method Post -Uri ($base + '/api/login') -ContentType 'application/json; charset=utf-8' `
            -Body $bodyBytes -TimeoutSec 10 -ErrorAction Stop
    } catch {
        throw ('SnowLuma 登录失败（{0}）：{1}' -f $base, $_.Exception.Message)
    }
    $token = Get-EmoConfigValue $resp 'token'
    if (-not $token) { throw 'SnowLuma 登录响应中没有 token 字段（密码错误或版本不符）。' }
    [pscustomobject]@{ BaseUrl = $base; Token = [string]$token }
}

function Invoke-SnowLuma {
    param(
        [Parameter(Mandatory)]$Session,
        [string]$Method = 'Get',
        [Parameter(Mandatory)][string]$Path,
        $Body
    )
    $req = @{
        Method      = $Method
        Uri         = ($Session.BaseUrl + $Path)
        Headers     = @{ Authorization = ('Bearer ' + $Session.Token) }
        TimeoutSec  = 10
        ErrorAction = 'Stop'
    }
    # Body 仅用于 POST；GET 参数请直接拼进 $Path（PS 5.1 对 GET+字节 Body 会抛错）
    if ($null -ne $Body) {
        $req.ContentType = 'application/json; charset=utf-8'
        $req.Body = [Text.Encoding]::UTF8.GetBytes((ConvertTo-Json $Body))
    }
    try { return (Invoke-RestMethod @req) }
    catch {
        $resp = $null
        if ($_.Exception.PSObject.Properties['Response']) { $resp = $_.Exception.Response }
        if ($resp -is [System.Net.HttpWebResponse] -and [int]$resp.StatusCode -eq 401) {
            throw ('SnowLuma 会话已失效（401）：{0}，请重新运行 emo up' -f $Path)
        }
        throw
    }
}

function Get-SnowLumaQQList {
    param([Parameter(Mandatory)]$Session)
    return @(@(Expand-EmoListResponse (Invoke-SnowLuma -Session $Session -Path '/api/qq-list')) | ForEach-Object { ConvertTo-EmoQQEntry $_ })
}

function Get-SnowLumaProcesses {
    param([Parameter(Mandatory)]$Session)
    return @(@(Expand-EmoListResponse (Invoke-SnowLuma -Session $Session -Path '/api/processes')) | ForEach-Object { ConvertTo-EmoQQEntry $_ })
}

function Invoke-SnowLumaLoad {
    param([Parameter(Mandatory)]$Session, [Parameter(Mandatory)][int]$TargetPid)
    Invoke-SnowLuma -Session $Session -Method Post -Path ('/api/processes/{0}/load' -f $TargetPid)
}

function Invoke-SnowLumaUnload {
    param([Parameter(Mandatory)]$Session, [Parameter(Mandatory)][int]$TargetPid)
    Invoke-SnowLuma -Session $Session -Method Post -Path ('/api/processes/{0}/unload' -f $TargetPid)
}

function Show-EmoToast {
    param([Parameter(Mandatory)][string]$Title, [string]$Body = '', $Paths)
    try {
        $null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
        $null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
        $appId = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
        $safeTitle = [Security.SecurityElement]::Escape($Title)
        $safeBody = [Security.SecurityElement]::Escape($Body)
        $xmlText = '<toast><visual><binding template="ToastGeneric"><text>' + $safeTitle + '</text><text>' + $safeBody + '</text></binding></visual></toast>'
        $xml = New-Object Windows.Data.Xml.Dom.XmlDocument
        $xml.LoadXml($xmlText)
        [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId).Show(
            (New-Object Windows.UI.Notifications.ToastNotification $xml))
    } catch {
        # 最常见原因：在 PowerShell 7 (pwsh) 下运行——WinRT 类型只有 Windows PowerShell 5.1 能加载。
        # 桌面快捷方式固定调用 powershell.exe（5.1），手动运行也请用 powershell 而非 pwsh。
        Write-EmoLog -Level WARN -Message ('Toast 不可用（{0}），仅记录：{1} — {2}' -f $_.Exception.Message, $Title, $Body) -Paths $Paths
    }
}

function Send-EmoCtrlC {
    # 通过独立 powershell 进程附着目标控制台并发送 Ctrl+C（不影响当前会话）
    param([Parameter(Mandatory)][int]$TargetPid)
    $helperPath = Join-Path $PSScriptRoot 'send-ctrlc.ps1'
    $p = Start-Process powershell -ArgumentList @(
        '-NoProfile','-ExecutionPolicy','Bypass','-File', ('"{0}"' -f $helperPath), '-TargetPid', $TargetPid
    ) -WindowStyle Hidden -PassThru -Wait
    return ($p.ExitCode -eq 0)
}
