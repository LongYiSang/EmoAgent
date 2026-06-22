[CmdletBinding()]
param(
    [string]$ReleaseDir = (Get-Location).Path,
    [string]$ExePath = "",
    [string]$ConfigPath = "",
    [switch]$SkipPrivatePython
)

$ErrorActionPreference = "Stop"
$RequirePrivatePython = -not $SkipPrivatePython
$GeneratedSmokeDir = ""

function Fail([string]$Message) {
    throw "[fresh-install-smoke] $Message"
}

function Require-CheckStatus($Checks, [string]$ID, [string]$Status) {
    if (-not $Checks.ContainsKey($ID)) {
        Fail "missing diagnostic check '$ID'"
    }
    if ($Checks[$ID].status -ne $Status) {
        $message = $Checks[$ID].message
        Fail "diagnostic '$ID' status is '$($Checks[$ID].status)', want '$Status': $message"
    }
}

try {
$release = (Resolve-Path -LiteralPath $ReleaseDir).Path
if ([string]::IsNullOrWhiteSpace($ExePath)) {
    $ExePath = Join-Path $release "emoagent.exe"
}
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    Fail "emoagent executable not found: $ExePath"
}

if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $smokeRoot = Join-Path $release ".smoke"
    New-Item -ItemType Directory -Force -Path $smokeRoot | Out-Null
    $smokeDir = Join-Path $smokeRoot ([guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Force -Path $smokeDir | Out-Null
    $GeneratedSmokeDir = $smokeDir
    $ConfigPath = Join-Path $smokeDir "config.yaml"
    $pluginRootYaml = (Join-Path $smokeDir "plugins").Replace("\", "/")
    $dbPathYaml = (Join-Path $smokeDir "emo-smoke.db").Replace("\", "/")
    $config = @"
server:
  host: "127.0.0.1"
  port: 18080

chat:
  turn_pipeline:
    enabled: true
    rollout_percent: 100

plugins:
  enabled: false
  store:
    root_dir: "$pluginRootYaml"
    allow_dev_dirs: false
  runtime:
    process_enabled: true
    default_kind: managed_python_process
    startup_timeout_ms: 5000
    shutdown_timeout_ms: 3000
    max_processes: 64
    memory_mb: 1024
  installer:
    require_signature: true
    allow_unsigned_dev: false
  admin:
    enabled: true

memory:
  enabled: false

db:
  path: "$dbPathYaml"

personas:
  dir: personas

llm_providers:
  - id: smoke
    name: Smoke
    protocol: openai_compatible
    base_url: "http://127.0.0.1/unused"
    api_key_env: EMO_SMOKE_PROVIDER_KEY
    model_discovery: manual
    enabled: true

agent_configs:
  - id: default
    name: Default
    persona_key: default
    context_overrides: {}
    emotion:
      main:
        provider_id: smoke
        model: smoke-model
      summary:
        provider_id: smoke
        model: smoke-model
    work:
      main:
        provider_id: smoke
        model: smoke-model
      summary:
        provider_id: smoke
        model: smoke-model

agent:
  active_config: default
"@
    [System.IO.File]::WriteAllText($ConfigPath, $config, [System.Text.UTF8Encoding]::new($false))
}

$ConfigPath = (Resolve-Path -LiteralPath $ConfigPath).Path
$env:EMO_SMOKE_PROVIDER_KEY = "smoke-key"
$env:SMOKE_API_KEY = "must-not-leak-to-private-python"
Remove-Item Env:PYTHONHOME -ErrorAction SilentlyContinue
Remove-Item Env:PYTHONPATH -ErrorAction SilentlyContinue
Remove-Item Env:PYTHONUSERBASE -ErrorAction SilentlyContinue
Remove-Item Env:VIRTUAL_ENV -ErrorAction SilentlyContinue

$systemRoot = $env:SystemRoot
if ([string]::IsNullOrWhiteSpace($systemRoot)) {
    $systemRoot = "C:\Windows"
}
$env:PATH = (@(
    $release,
    (Join-Path $systemRoot "System32"),
    $systemRoot
) -join [System.IO.Path]::PathSeparator)

$args = @("-config", $ConfigPath, "-self-test-json")
if ($RequirePrivatePython) {
    $args += "-self-test-strict"
}

Push-Location $release
try {
    $stdout = & $ExePath @args
    $exitCode = $LASTEXITCODE
} finally {
    Pop-Location
}

if ($exitCode -ne 0) {
    Fail "emoagent self-test exited with code $exitCode"
}

try {
    $report = ($stdout -join "`n") | ConvertFrom-Json
} catch {
    Fail "self-test did not return valid JSON: $stdout"
}

$checks = @{}
foreach ($check in $report.plugin_diagnostics.checks) {
    $checks[$check.id] = $check
}

$errorChecks = @($report.plugin_diagnostics.checks | Where-Object { $_.status -eq "error" })
if ($errorChecks.Count -gt 0) {
    $ids = ($errorChecks | ForEach-Object { $_.id }) -join ", "
    Fail "diagnostic error checks: $ids"
}

Require-CheckStatus $checks "process_guard" "ok"
if ($RequirePrivatePython) {
    Require-CheckStatus $checks "private_python" "ok"
    Require-CheckStatus $checks "python_self_test" "ok"
}

[pscustomobject]@{
    status = "ok"
    release_dir = $release
    config_path = $ConfigPath
    require_private_python = $RequirePrivatePython
    diagnostics_status = $report.plugin_diagnostics.status
    checked = @($checks.Keys | Sort-Object)
} | ConvertTo-Json -Depth 4
} finally {
    if (-not [string]::IsNullOrWhiteSpace($GeneratedSmokeDir) -and (Test-Path -LiteralPath $GeneratedSmokeDir)) {
        Remove-Item -Recurse -Force -LiteralPath $GeneratedSmokeDir -ErrorAction SilentlyContinue
        $smokeRoot = Split-Path -Parent $GeneratedSmokeDir
        if ((Test-Path -LiteralPath $smokeRoot) -and -not (Get-ChildItem -Force -LiteralPath $smokeRoot -ErrorAction SilentlyContinue)) {
            Remove-Item -Force -LiteralPath $smokeRoot -ErrorAction SilentlyContinue
        }
    }
}
