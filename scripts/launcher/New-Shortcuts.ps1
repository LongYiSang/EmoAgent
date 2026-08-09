# 在桌面创建"启动/停止 EmoAgent"快捷方式（重复运行会覆盖更新）
$ErrorActionPreference = 'Stop'
$desktop = [Environment]::GetFolderPath('Desktop')
$ws = New-Object -ComObject WScript.Shell
foreach ($def in @(
    # up 需要 -Quiet：无窗口时不能弹交互式密码提示（宁可失败并 Toast 提示去终端补录）。
    # down 不接受交互输入本来就不会提示，无需 -Quiet。
    @{ Name = '启动 EmoAgent'; Args = 'up -Quiet'; Icon = 'shell32.dll,137' },
    @{ Name = '停止 EmoAgent'; Args = 'down';      Icon = 'shell32.dll,131' }
)) {
    $lnk = $ws.CreateShortcut((Join-Path $desktop ($def.Name + '.lnk')))
    $lnk.TargetPath = 'powershell.exe'
    # -NonInteractive：万一某次运行意外撞上交互提示，宁可直接失败也不要留一个隐藏窗口挂死
    $lnk.Arguments = ('-NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "{0}" {1}' -f (Join-Path $PSScriptRoot 'emo.ps1'), $def.Args)
    $lnk.WorkingDirectory = $PSScriptRoot
    $lnk.IconLocation = $def.Icon
    $lnk.Save()
    Write-Host ('已创建：{0}' -f (Join-Path $desktop ($def.Name + '.lnk')))
}
