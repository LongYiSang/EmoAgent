param([Parameter(Mandatory)][int]$TargetPid)
# 附着到目标进程的控制台，向该控制台的所有进程发送 Ctrl+C；
# 先屏蔽自身的 Ctrl+C 处理，随后自然退出。必须在独立进程中运行。
Add-Type -Namespace Emo -Name Killer -MemberDefinition @'
[DllImport("kernel32.dll", SetLastError=true)] public static extern bool FreeConsole();
[DllImport("kernel32.dll", SetLastError=true)] public static extern bool AttachConsole(uint dwProcessId);
[DllImport("kernel32.dll", SetLastError=true)] public static extern bool SetConsoleCtrlHandler(IntPtr handler, bool add);
[DllImport("kernel32.dll", SetLastError=true)] public static extern bool GenerateConsoleCtrlEvent(uint dwCtrlEvent, uint dwProcessGroupId);
'@
[Emo.Killer]::FreeConsole() | Out-Null
if (-not [Emo.Killer]::AttachConsole([uint32]$TargetPid)) { exit 1 }
[Emo.Killer]::SetConsoleCtrlHandler([IntPtr]::Zero, $true) | Out-Null
if (-not [Emo.Killer]::GenerateConsoleCtrlEvent(0, 0)) { exit 1 }
Start-Sleep -Milliseconds 300
exit 0
