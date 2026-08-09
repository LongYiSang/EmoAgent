$launcherDir = Split-Path -Parent $PSScriptRoot
. (Join-Path $launcherDir 'emo-common.ps1')

Get-ChildItem $env:TEMP -Filter 'emo-test-*' -Directory -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue

function New-EmoTestPaths {
    $tmp = Join-Path $env:TEMP ('emo-test-' + [guid]::NewGuid().ToString('N'))
    $p = Get-EmoLauncherPaths -ProjectDir $tmp
    New-Item -ItemType Directory -Force -Path $p.DataDir | Out-Null
    $p
}

Describe 'Get-EmoLauncherPaths' {
    It '从 ProjectDir 推导各路径' {
        $p = Get-EmoLauncherPaths -ProjectDir 'C:\proj'
        $p.DataDir | Should Be 'C:\proj\data\launcher'
        $p.StatePath | Should Be 'C:\proj\data\launcher\state.json'
    }
}

Describe 'Get-EmoConfigValue' {
    $obj = '{"a":{"b":{"c":42}},"s":"x"}' | ConvertFrom-Json
    It '按点路径取嵌套值' { Get-EmoConfigValue $obj 'a.b.c' | Should Be 42 }
    It '路径不存在返回 null' { Get-EmoConfigValue $obj 'a.zz.c' | Should BeNullOrEmpty }
    It '取顶层值' { Get-EmoConfigValue $obj 's' | Should Be 'x' }
}

Describe 'Get-EmoLauncherConfig' {
    It '配置缺失时复制样例并抛出引导' {
        $paths = New-EmoTestPaths
        { Get-EmoLauncherConfig $paths } | Should Throw '已生成配置文件'
        Test-Path $paths.ConfigPath | Should Be $true
    }
    It '必填项缺失时抛出' {
        $paths = New-EmoTestPaths
        '{"snowluma":{"dir":"C:\\x"}}' | Out-File -Encoding utf8 $paths.ConfigPath
        { Get-EmoLauncherConfig $paths } | Should Throw 'snowluma.base_url'
    }
    It '路径存在性校验（snowluma.dir 不存在时抛出）' {
        $paths = New-EmoTestPaths
        $cfg = Get-Content -Raw -Encoding UTF8 $paths.ExamplePath | ConvertFrom-Json
        $cfg.snowluma.dir = 'C:\definitely\not\exist\dir'
        $cfg | ConvertTo-Json -Depth 5 | Out-File -Encoding utf8 $paths.ConfigPath
        { Get-EmoLauncherConfig $paths } | Should Throw 'snowluma.dir'
    }
    It 'JSON 解析失败时抛出' {
        $paths = New-EmoTestPaths
        'not-json{{{' | Out-File -Encoding utf8 $paths.ConfigPath
        { Get-EmoLauncherConfig $paths } | Should Throw '解析失败'
    }
    It 'base_url 非 http 时抛出' {
        $paths = New-EmoTestPaths
        $cfg = Get-Content -Raw -Encoding UTF8 $paths.ExamplePath | ConvertFrom-Json
        $cfg.snowluma.base_url = '127.0.0.1:5099'
        $cfg | ConvertTo-Json -Depth 5 | Out-File -Encoding utf8 $paths.ConfigPath
        { Get-EmoLauncherConfig $paths } | Should Throw 'http'
    }
}

Describe 'Get-EmoConfigInt / Get-EmoConfigString' {
    $obj = '{"a":{"n":7,"s":"hi"}}' | ConvertFrom-Json
    It '存在时取值' { Get-EmoConfigInt $obj 'a.n' 99 | Should Be 7 }
    It '缺失时用默认值' { Get-EmoConfigInt $obj 'a.zz' 99 | Should Be 99 }
    It '字符串默认值' { Get-EmoConfigString $obj 'a.zz' 'dft' | Should Be 'dft' }
    It '非数字时抛出' { { Get-EmoConfigInt $obj 'a.s' 99 } | Should Throw '需为整数' }
    It '空字符串时用默认值' {
        $objEmpty = '{"a":{"e":""}}' | ConvertFrom-Json
        Get-EmoConfigInt $objEmpty 'a.e' 99 | Should Be 99
    }
}

Describe 'Read-EmoState / Save-EmoState' {
    It '无文件时返回空对象' {
        $paths = New-EmoTestPaths
        $s = Read-EmoState $paths
        Get-EmoConfigValue $s 'qq_pid' | Should BeNullOrEmpty
    }
    It '合并写入并保留其他键' {
        $paths = New-EmoTestPaths
        Save-EmoState $paths @{ qq_pid = 111 }
        Save-EmoState $paths @{ emoagent_pid = 222 }
        $s = Read-EmoState $paths
        Get-EmoConfigValue $s 'qq_pid' | Should Be 111
        Get-EmoConfigValue $s 'emoagent_pid' | Should Be 222
    }
    It '损坏的 state.json 按空状态处理' {
        $paths = New-EmoTestPaths
        'not-json{{{' | Out-File -Encoding utf8 $paths.StatePath
        $s = Read-EmoState $paths
        Get-EmoConfigValue $s 'qq_pid' | Should BeNullOrEmpty
    }
    It '无文件时返回真实空对象而非 null' {
        $paths = New-EmoTestPaths
        $s = Read-EmoState $paths
        $s | Should Not BeNullOrEmpty
        @($s.PSObject.Properties).Count | Should Be 0
    }
    It '0 字节 state.json 恢复为空状态且后续保存不丢数据' {
        $paths = New-EmoTestPaths
        [IO.File]::WriteAllText($paths.StatePath, '')
        $s = Read-EmoState $paths
        @($s.PSObject.Properties).Count | Should Be 0
        Save-EmoState $paths @{ qq_pid = 7 }
        Get-EmoConfigValue (Read-EmoState $paths) 'qq_pid' | Should Be 7
    }
}

Describe 'Resolve-EmoKillTarget' {
    $table = @(
        [pscustomobject]@{ ProcessId = 100; Name = 'QQ.exe';   CommandLine = 'C:\qq\QQ.exe' },
        [pscustomobject]@{ ProcessId = 200; Name = 'node.exe'; CommandLine = 'D:\Dev\Deploy\SnowLuma-v1.11.4-win-x64\node.exe index.mjs' },
        [pscustomobject]@{ ProcessId = 300; Name = 'emoagent.exe'; CommandLine = 'D:\proj\bin\emoagent.exe --config config.yaml' },
        [pscustomobject]@{ ProcessId = 0;   Name = 'QQ.exe';   CommandLine = 'x' },
        [pscustomobject]@{ ProcessId = 400; Name = 'QQEX.exe'; CommandLine = 'C:\t\QQEX.exe' },
        [pscustomobject]@{ ProcessId = 500; Name = 'QQ.exe';   CommandLine = 'C:\qq\QQ.exe'; CreationDate = (Get-Date '2026-07-26 10:00:00') }
    )
    It 'PID 不在进程表中返回 null' {
        Resolve-EmoKillTarget -ProcessId 999 -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It 'PID 为 null/0 返回 null' {
        Resolve-EmoKillTarget -ProcessId $null -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
        Resolve-EmoKillTarget -ProcessId 0 -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It '进程名不匹配返回 null（防 PID 复用误杀）' {
        Resolve-EmoKillTarget -ProcessId 200 -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It '要求命令行匹配而不匹配时返回 null' {
        Resolve-EmoKillTarget -ProcessId 200 -ExpectedName 'node' -CommandLineMatch 'OtherApp' -ProcessTable $table | Should BeNullOrEmpty
    }
    It '名称+命令行都匹配时返回条目' {
        $t = Resolve-EmoKillTarget -ProcessId 200 -ExpectedName 'node' -CommandLineMatch 'SnowLuma' -ProcessTable $table
        $t.ProcessId | Should Be 200
    }
    It '仅名称匹配（无命令行要求）时返回条目' {
        (Resolve-EmoKillTarget -ProcessId 100 -ExpectedName 'QQ' -ProcessTable $table).ProcessId | Should Be 100
    }
    It 'PID 为 0 即使表中存在该行也返回 null' {
        Resolve-EmoKillTarget -ProcessId 0 -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It '相似进程名不放行（QQEX.exe 不等于 QQ）' {
        Resolve-EmoKillTarget -ProcessId 400 -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It '非数字 PID 返回 null 而非抛出' {
        Resolve-EmoKillTarget -ProcessId 'abc' -ExpectedName 'QQ' -ProcessTable $table | Should BeNullOrEmpty
    }
    It 'emoagent 条目按名称放行' {
        (Resolve-EmoKillTarget -ProcessId 300 -ExpectedName 'emoagent' -ProcessTable $table).ProcessId | Should Be 300
    }
    It '要求开始时间但不符时返回 null' {
        Resolve-EmoKillTarget -ProcessId 500 -ExpectedName 'QQ' -ExpectedStartTime (Get-Date '2026-07-26 11:00:00') -ProcessTable $table | Should BeNullOrEmpty
    }
    It '开始时间在容差内时返回条目' {
        (Resolve-EmoKillTarget -ProcessId 500 -ExpectedName 'QQ' -ExpectedStartTime (Get-Date '2026-07-26 10:00:01') -ProcessTable $table).ProcessId | Should Be 500
    }
    It '要求开始时间但表行无 CreationDate 时返回 null' {
        Resolve-EmoKillTarget -ProcessId 100 -ExpectedName 'QQ' -ExpectedStartTime (Get-Date) -ProcessTable $table | Should BeNullOrEmpty
    }
}

Describe 'SnowLuma 凭据存取' {
    It '无凭据且不允许提示时返回 null' {
        $paths = New-EmoTestPaths
        Get-SnowLumaPassword -Paths $paths | Should BeNullOrEmpty
    }
    It 'DPAPI 往返：写入后可读回明文' {
        $paths = New-EmoTestPaths
        $sec = ConvertTo-SecureString 'p@ss中文' -AsPlainText -Force
        $sec | Export-Clixml -Path $paths.CredPath
        Get-SnowLumaPassword -Paths $paths | Should Be 'p@ss中文'
    }
}

Describe 'SnowLuma 凭据存取（Set/Get 组合）' {
    Mock Read-Host { ConvertTo-SecureString 'mocked中文P@ss' -AsPlainText -Force }
    It 'Set 后 Get 读回明文（Set 真正执行）' {
        $paths = New-EmoTestPaths
        Set-SnowLumaPassword -Paths $paths
        Get-SnowLumaPassword -Paths $paths | Should Be 'mocked中文P@ss'
    }
    It 'Set 会创建缺失的 DataDir' {
        $tmp = Join-Path $env:TEMP ('emo-test-' + [guid]::NewGuid().ToString('N'))
        $paths = Get-EmoLauncherPaths -ProjectDir $tmp
        Test-Path $paths.DataDir | Should Be $false
        Set-SnowLumaPassword -Paths $paths
        Test-Path $paths.CredPath | Should Be $true
    }
    It '空解码凭据 + AllowPrompt：自愈后返回新密码' {
        $paths = New-EmoTestPaths
        (New-Object System.Security.SecureString) | Export-Clixml -Path $paths.CredPath
        Get-SnowLumaPassword -Paths $paths -AllowPrompt | Should Be 'mocked中文P@ss'
    }
}

Describe 'SnowLuma 凭据异常路径' {
    Mock Read-Host { New-Object System.Security.SecureString }
    It '空密码不写入并抛出' {
        $paths = New-EmoTestPaths
        { Set-SnowLumaPassword -Paths $paths } | Should Throw '密码不能为空'
        Test-Path $paths.CredPath | Should Be $false
    }
    It '损坏凭据文件给出可操作错误' {
        $paths = New-EmoTestPaths
        'garbage-not-clixml' | Out-File -Encoding utf8 $paths.CredPath
        { Get-SnowLumaPassword -Paths $paths } | Should Throw '无法读取'
    }
    It '0 字节凭据文件按缺失处理返回 null' {
        $paths = New-EmoTestPaths
        [IO.File]::WriteAllText($paths.CredPath, '')
        Get-SnowLumaPassword -Paths $paths | Should BeNullOrEmpty
    }
    It '递归有界：空解码自愈时 Set 只被调用一次' {
        Mock Set-SnowLumaPassword { param($Paths) (New-Object System.Security.SecureString) | Export-Clixml -Path $Paths.CredPath }
        $paths = New-EmoTestPaths
        (New-Object System.Security.SecureString) | Export-Clixml -Path $paths.CredPath
        Get-SnowLumaPassword -Paths $paths -AllowPrompt | Should BeNullOrEmpty
        Assert-MockCalled Set-SnowLumaPassword -Scope It -Times 1 -Exactly
    }
}

Describe 'Wait-EmoCondition' {
    It '探针立即成功时返回其结果' {
        Wait-EmoCondition -Probe { 'ready' } -TimeoutSeconds 5 -IntervalSeconds 0 | Should Be 'ready'
    }
    It '第 N 次才成功' {
        $script:emoTestCounter = 0
        $r = Wait-EmoCondition -TimeoutSeconds 10 -IntervalSeconds 0 -Probe {
            $script:emoTestCounter++
            if ($script:emoTestCounter -ge 3) { return 'ok' }
            return $null
        }
        $r | Should Be 'ok'
        $script:emoTestCounter | Should Be 3
    }
    It '超时返回 null' {
        Wait-EmoCondition -Probe { $null } -TimeoutSeconds 1 -IntervalSeconds 0 | Should BeNullOrEmpty
    }
    It '探针抛异常按未就绪处理而非中断等待' {
        $script:emoThrowCounter = 0
        $r = Wait-EmoCondition -TimeoutSeconds 10 -IntervalSeconds 0 -Label 'probe-throws-test' -Probe {
            $script:emoThrowCounter++
            if ($script:emoThrowCounter -lt 3) { throw 'transient' }
            return 'recovered'
        }
        $r | Should Be 'recovered'
        $script:emoThrowCounter | Should Be 3
    }
    It '持续抛异常时超时返回 null' {
        Wait-EmoCondition -Probe { throw 'always' } -TimeoutSeconds 1 -IntervalSeconds 0 -Label 'always-throws' -Paths (New-EmoTestPaths) | Should BeNullOrEmpty
    }
    It 'ThrowOnProbeError 时异常向上传播' {
        { Wait-EmoCondition -Probe { throw 'boom' } -TimeoutSeconds 1 -IntervalSeconds 0 -ThrowOnProbeError } | Should Throw 'boom'
    }
}

Describe 'Test-EmoHttp' {
    It '连接被拒时为 false' { Test-EmoHttp -Url 'http://127.0.0.1:59999/' -TimeoutSec 1 | Should Be $false }
    It '非 WebException 异常不抛出且为 false' {
        Mock Invoke-WebRequest { throw 'plain failure' }
        Test-EmoHttp -Url 'http://127.0.0.1:1/' | Should Be $false
    }
    It '无 Response 的 WebException 为 false' {
        Mock Invoke-WebRequest { throw (New-Object System.Net.WebException 'name resolve') }
        Test-EmoHttp -Url 'http://127.0.0.1:1/' | Should Be $false
    }
}

Describe 'ConvertTo-EmoQQEntry' {
    It '解析 processes 条目（pid/status/uin）' {
        $raw = '{"pid":11672,"name":"QQ.exe","status":"online","uin":"1765843429"}' | ConvertFrom-Json
        $e = ConvertTo-EmoQQEntry $raw
        $e.Uin | Should Be '1765843429'
        $e.Online | Should Be $true
        $e.Pid | Should Be 11672
    }
    It 'available（未注入，实测 T6 核验的真实状态值）视为未上线' {
        $raw = '{"pid":1972,"name":"QQ.exe","status":"available"}' | ConvertFrom-Json
        (ConvertTo-EmoQQEntry $raw).Online | Should Be $false
    }
    It '实测完整字段（injected/connected/loggedIn/uin="0"）未上线且 Uin 视为未关联（非"WRONG_ACCOUNT"）' {
        # 实测 emo up 曾在注入后 6 秒内误判"选错账号"：uin="0" 是字符串，
        # PowerShell 中非空字符串永远 truthy，导致 Uin 被当成"已选中的账号"参与比较。
        $raw = '{"pid":24896,"name":"QQ.exe","path":"","injected":false,"connected":false,"loggedIn":false,"uin":"0","status":"available","error":"","method":""}' | ConvertFrom-Json
        $e = ConvertTo-EmoQQEntry $raw
        $e.Online | Should Be $false
        $e.Uin | Should BeNullOrEmpty
    }
    It '兼容 selfId 字段名' {
        $raw = '{"selfId":"42","online":true}' | ConvertFrom-Json
        $e = ConvertTo-EmoQQEntry $raw
        $e.Uin | Should Be '42'
        $e.Online | Should Be $true
    }
    It 'uin 统一为字符串比较' {
        $raw = '{"uin":1765843429,"status":"online"}' | ConvertFrom-Json
        (ConvertTo-EmoQQEntry $raw).Uin | Should Be '1765843429'
    }
    It '标量条目直接作为 uin' {
        $e = ConvertTo-EmoQQEntry '1765843429'
        $e.Uin | Should Be '1765843429'
        $e.Online | Should Be $false
    }
    It '实测 qq-list 条目（仅 uin/nickname，无 status/online 字段）视为在线' {
        # emo up 曾因此永远检测不到登录完成：真实 QQ 已登录，qq-list 里也出现了该条目，
        # 但函数一直判定 Online=false，脚本一直显示"仍在等待登录"。
        $raw = '{"uin":"1765843429","nickname":"某昵称"}' | ConvertFrom-Json
        $e = ConvertTo-EmoQQEntry $raw
        $e.Uin | Should Be '1765843429'
        $e.Online | Should Be $true
    }
}

Describe 'Expand-EmoListResponse' {
    It '裸数组原样返回' {
        $r = '[{"pid":1},{"pid":2}]' | ConvertFrom-Json
        @(Expand-EmoListResponse $r).Count | Should Be 2
    }
    It '解包 {processes:[...]} 包装' {
        $r = '{"processes":[{"pid":1}]}' | ConvertFrom-Json
        @(Expand-EmoListResponse $r).Count | Should Be 1
    }
    It 'null 返回空数组' {
        @(Expand-EmoListResponse $null).Count | Should Be 0
    }
    It '解包 items 包装' { @(Expand-EmoListResponse ('{"items":[{"pid":1},{"pid":2}]}' | ConvertFrom-Json)).Count | Should Be 2 }
    It '已知字段存在但为 null 时返回空数组' { @(Expand-EmoListResponse ('{"processes":null}' | ConvertFrom-Json)).Count | Should Be 0 }
    It '无已知字段的单对象包成单元素数组' { @(Expand-EmoListResponse ('{"pid":7}' | ConvertFrom-Json)).Count | Should Be 1 }
    It '空字段不遮蔽后续数组字段' {
        @(Expand-EmoListResponse ('{"items":null,"processes":[{"pid":1},{"pid":2}]}' | ConvertFrom-Json)).Count | Should Be 2
    }
}

Describe 'Test-EmoAdapterPayloadConnected' {
    It 'transport.connected 为 true 时判定已连接' {
        $s = '{"adapters":[{"id":"qq-main","transport":{"connected":true,"state":"connected"}}]}' | ConvertFrom-Json
        Test-EmoAdapterPayloadConnected $s | Should Be $true
    }
    It '全部未连接时为 false' {
        $s = '{"adapters":[{"id":"qq-main","transport":{"connected":false}}]}' | ConvertFrom-Json
        Test-EmoAdapterPayloadConnected $s | Should Be $false
    }
    It '无 adapters 字段为 false' {
        Test-EmoAdapterPayloadConnected ('{}' | ConvertFrom-Json) | Should Be $false
    }
    It 'adapters 为单对象而非数组时也能判定' {
        $s = '{"adapters":{"id":"qq-main","transport":{"connected":true}}}' | ConvertFrom-Json
        Test-EmoAdapterPayloadConnected $s | Should Be $true
    }
    It 'adapters 为空数组时为 false' {
        Test-EmoAdapterPayloadConnected ('{"adapters":[]}' | ConvertFrom-Json) | Should Be $false
    }
}

Describe 'Start-EmoHiddenProcess' {
    It '自动创建重定向目录并返回进程' {
        $dir = Join-Path $env:TEMP ('emo-test-' + [guid]::NewGuid().ToString('N'))
        $out = Join-Path $dir 'sub\out.log'
        $err = Join-Path $dir 'sub\err.log'
        $p = Start-EmoHiddenProcess -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-Command','exit') -StdOutPath $out -StdErrPath $err
        $p | Should Not BeNullOrEmpty
        Test-Path (Split-Path -Parent $out) | Should Be $true
        $p.WaitForExit(10000) | Out-Null
        Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    }
}
