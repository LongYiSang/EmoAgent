[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)][ValidateSet('up','down','status')][string]$Command,
    [switch]$Rebuild,
    [switch]$Quiet
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'emo-common.ps1')

function Format-EmoCheck { param([bool]$Ok) if ($Ok) { '√' } else { '×' } }

function Invoke-EmoStatus {
    param([Parameter(Mandatory)]$Paths)
    $cfg = Get-EmoLauncherConfig $Paths
    $botUin = [string]$cfg.qq.bot_uin
    $problems = 0

    $slUp = Test-EmoHttp -Url (([string]$cfg.snowluma.base_url).TrimEnd('/') + '/')
    Write-Host ('[{0}] SnowLuma 服务         {1}' -f (Format-EmoCheck $slUp), $cfg.snowluma.base_url)
    if (-not $slUp) { $problems++ }

    $botOnline = $false
    if ($slUp) {
        $password = Get-SnowLumaPassword -Paths $Paths
        if ($password) {
            try {
                $session = Connect-SnowLuma -BaseUrl $cfg.snowluma.base_url -Password $password
                $botOnline = @(Get-SnowLumaQQList -Session $session | Where-Object { $_.Uin -eq $botUin -and $_.Online }).Count -gt 0
            } catch {
                Write-Host ('    （SnowLuma 登录失败：{0}）' -f $_.Exception.Message)
            }
        } else {
            Write-Host '    （尚无凭据，跳过 Bot 检查；请先在终端运行 emo.ps1 up 录入密码）'
        }
    }
    Write-Host ('[{0}] Bot QQ 已注入并在线   UIN {1}' -f (Format-EmoCheck $botOnline), $botUin)
    if (-not $botOnline) { $problems++ }

    $eaStatus = $null
    try {
        $eaStatus = Invoke-RestMethod -Uri ($cfg.emoagent.base_url.TrimEnd('/') + '/api/platforms/status') -TimeoutSec 5 -ErrorAction Stop
    } catch { }
    $eaUp = ($null -ne $eaStatus)
    Write-Host ('[{0}] EmoAgent 服务         {1}' -f (Format-EmoCheck $eaUp), $cfg.emoagent.base_url)
    if (-not $eaUp) { $problems++ }

    $connected = $false
    if ($eaUp) { $connected = Test-EmoAdapterPayloadConnected $eaStatus }
    Write-Host ('[{0}] Adapter WS 已连接' -f (Format-EmoCheck $connected))
    if (-not $connected) { $problems++ }

    if ($problems -gt 0) {
        Write-Host ''
        Write-Host ('有 {0} 项未就绪 → 运行 scripts\launcher\emo.ps1 up' -f $problems)
        exit 1
    }
    Write-Host ''
    Write-Host '全部就绪，直接在 QQ 发消息即可。'
}

function Stop-EmoAgentProcess {
    # up -Rebuild 与 down 共用：优雅停止 EmoAgent（Ctrl+C → 15s → 强杀兜底）
    param([Parameter(Mandatory)]$Paths)
    $state = Read-EmoState $Paths
    $candidate = Get-EmoConfigValue $state 'emoagent_pid'
    $target = Resolve-EmoKillTarget -ProcessId $candidate -ExpectedName 'emoagent'
    if (-not $target) {
        $running = @(Get-Process -Name 'emoagent' -ErrorAction SilentlyContinue)
        if ($running.Count -gt 0) {
            $target = [pscustomobject]@{ ProcessId = $running[0].Id; Name = 'emoagent.exe' }
        }
    }
    if (-not $target) { Write-EmoLog -Message 'EmoAgent 未在运行' -Paths $Paths; return }
    $targetPid = [int]$target.ProcessId
    Write-EmoLog -Message ('停止 EmoAgent PID={0}（尝试优雅 Ctrl+C）' -f $targetPid) -Paths $Paths
    $null = Send-EmoCtrlC -TargetPid $targetPid
    $gone = Wait-EmoCondition -TimeoutSeconds 15 -IntervalSeconds 1 -Label 'EmoAgent 退出' -Paths $Paths -Probe {
        if (Get-Process -Id $targetPid -ErrorAction SilentlyContinue) { return $null }
        return $true
    }
    if (-not $gone) {
        Write-EmoLog -Level WARN -Message '优雅停止超时，强制结束' -Paths $Paths
        Stop-Process -Id $targetPid -Force -ErrorAction SilentlyContinue
    }
}

function Start-EmoLegSnowLuma {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)]$Config)
    $base = ([string]$Config.snowluma.base_url).TrimEnd('/')
    if (Test-EmoHttp -Url ($base + '/')) { Write-EmoLog -Message 'SnowLuma 已在运行' -Paths $Paths; return }
    Write-EmoLog -Message '启动 SnowLuma…' -Paths $Paths
    New-Item -ItemType Directory -Force -Path $Paths.LogsDir | Out-Null
    $node = Join-Path $Config.snowluma.dir 'node.exe'
    $p = Start-EmoHiddenProcess -FilePath $node -ArgumentList @('index.mjs') -WorkingDirectory $Config.snowluma.dir `
        -StdOutPath (Join-Path $Paths.LogsDir 'snowluma-console.log') -StdErrPath (Join-Path $Paths.LogsDir 'snowluma-console.err.log')
    Save-EmoState $Paths @{ snowluma_pid = $p.Id }
    $ok = Wait-EmoCondition -TimeoutSeconds (Get-EmoConfigInt $Config 'snowluma.ready_timeout_seconds' 30) `
        -Label 'SnowLuma 就绪' -Paths $Paths -Probe { Test-EmoHttp -Url ($base + '/') }
    if (-not $ok) { throw 'SnowLuma 启动超时（检查 data\launcher\logs\snowluma-console.err.log）' }
}

function Start-EmoLegBotQQ {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)]$Config, [Parameter(Mandatory)]$Session)
    $botUin = [string]$Config.qq.bot_uin
    $already = @(Get-SnowLumaQQList -Session $Session | Where-Object { $_.Uin -eq $botUin -and $_.Online })
    if ($already.Count -gt 0) { Write-EmoLog -Message ('Bot QQ（{0}）已在线' -f $botUin) -Paths $Paths; return }

    Write-EmoLog -Message '启动 Bot QQ 进程…' -Paths $Paths
    $qqProc = Start-Process -FilePath $Config.qq.exe_path -PassThru   # 登录窗必须可见，不隐藏
    $qqPid = [int]$qqProc.Id
    # 同时记录启动时间：down 回退到状态文件杀进程时用它证明"这个 PID 还是当初那个进程"
    $qqStart = $null
    try { $qqStart = $qqProc.StartTime.ToString('o') } catch { }
    Save-EmoState $Paths @{ qq_pid = $qqPid; qq_start_time = $qqStart }

    $seen = Wait-EmoCondition -TimeoutSeconds 60 -Label 'SnowLuma 发现 QQ 进程' -Paths $Paths -Probe {
        $hit = @(Get-SnowLumaProcesses -Session $Session | Where-Object { $_.Pid -eq $qqPid })
        if ($hit.Count -gt 0) { return $true }
        return $null
    }
    if (-not $seen) { throw ('SnowLuma 未发现刚启动的 QQ 进程 PID={0}' -f $qqPid) }

    Invoke-SnowLumaLoad -Session $Session -TargetPid $qqPid | Out-Null
    Write-EmoLog -Message ('已注入 PID={0}，等待登录…' -f $qqPid) -Paths $Paths
    Show-EmoToast -Title '请登录 Agent 账号' -Body '已注入。请在 QQ 登录窗中选择 Agent 使用的账号完成登录。'

    # 登录等待期若完全静默，容易被误判为"卡住"而被人工强行终止（实测发生过：
    # 登录其实已成功，却因为看起来像卡死而被关掉），故每 ~30s 打一次心跳日志。
    $script:emoQqWaitPoll = 0
    $result = Wait-EmoCondition -TimeoutSeconds (Get-EmoConfigInt $Config 'qq.login_wait_seconds' 300) -IntervalSeconds 3 `
        -Label 'QQ 登录' -Paths $Paths -Probe {
        $script:emoQqWaitPoll++
        if ($script:emoQqWaitPoll % 10 -eq 0) {
            Write-EmoLog -Message ('仍在等待 QQ 登录完成（脚本未卡住，请在弹出的 QQ 窗口选择 Agent 账号；已等待约 {0}s）' -f ($script:emoQqWaitPoll * 3)) -Paths $Paths
        }
        $entry = @(Get-SnowLumaProcesses -Session $Session | Where-Object { $_.Pid -eq $qqPid })
        if ($entry.Count -gt 0 -and $entry[0].Uin -and $entry[0].Uin -ne $botUin) { return 'WRONG_ACCOUNT' }
        $online = @(Get-SnowLumaQQList -Session $Session | Where-Object { $_.Uin -eq $botUin -and $_.Online })
        if ($online.Count -gt 0) { return 'ONLINE' }
        return $null
    }
    if ($result -eq 'WRONG_ACCOUNT') {
        Invoke-SnowLumaUnload -Session $Session -TargetPid $qqPid | Out-Null
        Show-EmoToast -Title '登录的不是 Agent 账号' -Body '已卸载注入，该 QQ 进程未被关闭。请重新运行 emo up。'
        throw '检测到误选账号，已卸载注入（进程保留）'
    }
    if ($result -ne 'ONLINE') { throw ('等待 Bot QQ 登录超时（{0}s）' -f (Get-EmoConfigInt $Config 'qq.login_wait_seconds' 300)) }
    Write-EmoLog -Message 'Bot QQ 已上线' -Paths $Paths
}

function Start-EmoLegEmoAgent {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)]$Config, [switch]$Rebuild)
    $base = $Config.emoagent.base_url
    $projectDir = [string]$Config.emoagent.project_dir
    $exeRel = Get-EmoConfigString $Config 'emoagent.exe' 'bin\emoagent.exe'
    $cfgRel = Get-EmoConfigString $Config 'emoagent.config' 'config.yaml'

    if ($Rebuild) {
        Write-EmoLog -Message '重建前端与后端…' -Paths $Paths
        Stop-EmoAgentProcess -Paths $Paths
        Push-Location (Join-Path $projectDir 'web')
        try {
            & npx --yes pnpm run build
            if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
        } finally { Pop-Location }
        Push-Location $projectDir
        try {
            & go build -o (Join-Path $projectDir $exeRel) ./cmd/emoagent
            if ($LASTEXITCODE -ne 0) { throw '后端构建失败' }
        } finally { Pop-Location }
    }

    if (Test-EmoHttp -Url ($base.TrimEnd('/') + '/api/platforms/status')) {
        Write-EmoLog -Message 'EmoAgent 已在运行' -Paths $Paths
    } else {
        Write-EmoLog -Message '启动 EmoAgent…' -Paths $Paths
        New-Item -ItemType Directory -Force -Path $Paths.LogsDir | Out-Null
        $p = Start-EmoHiddenProcess -FilePath (Join-Path $projectDir $exeRel) `
            -ArgumentList @('--config', (Join-Path $projectDir $cfgRel)) -WorkingDirectory $projectDir `
            -StdOutPath (Join-Path $Paths.LogsDir 'emoagent-console.log') -StdErrPath (Join-Path $Paths.LogsDir 'emoagent-console.err.log')
        Save-EmoState $Paths @{ emoagent_pid = $p.Id }
    }

    $connected = Wait-EmoCondition -TimeoutSeconds (Get-EmoConfigInt $Config 'emoagent.ready_timeout_seconds' 120) `
        -Label 'EmoAgent adapter 连接' -Paths $Paths -Probe {
        if (Test-EmoAdapterConnected -BaseUrl $base) { return $true }
        return $null
    }
    if (-not $connected) { throw 'EmoAgent adapter 连接超时（检查 data\launcher\logs\emoagent-console.err.log 与 WebUI 日志页）' }
}

function Invoke-EmoUp {
    param([Parameter(Mandatory)]$Paths, [switch]$Rebuild, [switch]$Quiet)
    $cfg = Get-EmoLauncherConfig $Paths
    $password = Get-SnowLumaPassword -Paths $Paths -AllowPrompt:(-not $Quiet)
    if (-not $password) {
        Show-EmoToast -Title '首次运行需要录入密码' -Body '请在终端运行 scripts\launcher\emo.ps1 up 完成初始化。'
        throw '缺少 SnowLuma 凭据（快捷方式静默模式无法交互录入）'
    }
    Start-EmoLegSnowLuma -Paths $Paths -Config $cfg
    $session = Connect-SnowLuma -BaseUrl $cfg.snowluma.base_url -Password $password
    Start-EmoLegBotQQ -Paths $Paths -Config $cfg -Session $session
    Start-EmoLegEmoAgent -Paths $Paths -Config $cfg -Rebuild:$Rebuild
    Show-EmoToast -Title 'EmoAgent 已上线' -Body '直接在 QQ 给它发消息即可。'
    Write-EmoLog -Message '全链路就绪' -Paths $Paths
}

function Stop-EmoBotQQ {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)]$Config, $Session)
    $botUin = [string]$Config.qq.bot_uin
    $candidatePid = $null
    if ($Session) {
        try {
            $entry = @(Get-SnowLumaProcesses -Session $Session | Where-Object { $_.Uin -eq $botUin }) | Select-Object -First 1
            if ($entry -and $entry.Pid) {
                $candidatePid = [int]$entry.Pid
                Invoke-SnowLumaUnload -Session $Session -TargetPid $candidatePid | Out-Null
                Write-EmoLog -Message ('已卸载注入 PID={0}' -f $candidatePid) -Paths $Paths
            }
        } catch {
            Write-EmoLog -Level WARN -Message ('查询/卸载注入失败：{0}' -f $_.Exception.Message) -Paths $Paths
        }
    }
    # SnowLuma 报出的 PID 是实时权威信息，无需再校验启动时间；只有回退到状态文件时才需要——
    # 那份记录可能是几小时前写的，PID 可能已被系统复用，仅靠进程名会放行"恰好复用该 PID 的个人 QQ"。
    $expectedStart = $null
    if (-not $candidatePid) {
        $state = Read-EmoState $Paths
        $candidatePid = Get-EmoConfigValue $state 'qq_pid'
        $savedStart = Get-EmoConfigValue $state 'qq_start_time'
        if ($savedStart) {
            $parsedStart = [datetime]::MinValue
            if ([datetime]::TryParse([string]$savedStart, [ref]$parsedStart)) { $expectedStart = $parsedStart }
        }
    }
    $target = Resolve-EmoKillTarget -ProcessId $candidatePid -ExpectedName 'QQ' -ExpectedStartTime $expectedStart
    if (-not $target) {
        Write-EmoLog -Level WARN -Message '无法确认 Bot QQ 进程归属，跳过（绝不猜杀）' -Paths $Paths
        return
    }
    Stop-Process -Id ([int]$target.ProcessId) -Force -ErrorAction SilentlyContinue
    Write-EmoLog -Message ('已结束 Bot QQ PID={0}' -f $target.ProcessId) -Paths $Paths
}

function Stop-EmoSnowLuma {
    param([Parameter(Mandatory)]$Paths, [Parameter(Mandatory)]$Config)
    $statePid = Get-EmoConfigValue (Read-EmoState $Paths) 'snowluma_pid'
    $target = Resolve-EmoKillTarget -ProcessId $statePid -ExpectedName 'node' -CommandLineMatch 'index.mjs'
    if (-not $target) {
        $table = Get-CimInstance Win32_Process -Filter "Name = 'node.exe'" | Select-Object ProcessId, Name, CommandLine
        $target = $table | Where-Object { $_.CommandLine -like ('*' + $Config.snowluma.dir + '*') } | Select-Object -First 1
    }
    if (-not $target) { Write-EmoLog -Message 'SnowLuma 未在运行' -Paths $Paths; return }
    Stop-Process -Id ([int]$target.ProcessId) -Force -ErrorAction SilentlyContinue
    Write-EmoLog -Message ('已停止 SnowLuma PID={0}' -f $target.ProcessId) -Paths $Paths
}

function Invoke-EmoDown {
    param([Parameter(Mandatory)]$Paths)
    $cfg = Get-EmoLauncherConfig $Paths
    $session = $null
    if (Test-EmoHttp -Url (([string]$cfg.snowluma.base_url).TrimEnd('/') + '/')) {
        $password = Get-SnowLumaPassword -Paths $Paths
        if ($password) {
            try { $session = Connect-SnowLuma -BaseUrl $cfg.snowluma.base_url -Password $password } catch { }
        }
    }
    Stop-EmoAgentProcess -Paths $Paths
    Stop-EmoBotQQ -Paths $Paths -Config $cfg -Session $session
    Stop-EmoSnowLuma -Paths $Paths -Config $cfg
    Save-EmoState $Paths @{ emoagent_pid = $null; qq_pid = $null; qq_start_time = $null; snowluma_pid = $null }
    Show-EmoToast -Title 'EmoAgent 已全部停止' -Body '个人 QQ 未受影响。'
}

$paths = Get-EmoLauncherPaths
switch ($Command) {
    'status' { Invoke-EmoStatus -Paths $paths }
    'up' {
        try {
            Invoke-EmoUp -Paths $paths -Rebuild:$Rebuild -Quiet:$Quiet
        } catch {
            Write-EmoLog -Level ERROR -Message $_.Exception.Message -Paths $paths
            Show-EmoToast -Title 'EmoAgent 启动失败' -Body ($_.Exception.Message + '（详见 data\launcher\logs）')
            exit 1
        }
    }
    'down' {
        try {
            Invoke-EmoDown -Paths $paths
        } catch {
            Write-EmoLog -Level ERROR -Message $_.Exception.Message -Paths $paths
            Show-EmoToast -Title 'EmoAgent 停止失败' -Body ($_.Exception.Message + '（详见 data\launcher\logs）')
            exit 1
        }
    }
}
