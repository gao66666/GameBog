# Agent 测评一键入口（PowerShell）
# 用法:
#   .\run_eval.ps1
#   .\run_eval.ps1 -E2e -Token $env:AGENT_EVAL_TOKEN
#   .\run_eval.ps1 -Chat "现在几点"

param(
    [switch]$E2e,
    [switch]$Rag,
    [switch]$SkipUnit,
    [string]$Token = $env:AGENT_EVAL_TOKEN,
    [string]$Chat = ""
)

Set-Location $PSScriptRoot

# 优先使用 langchain_agent conda 环境（与日常开发一致）
$LangchainPy = "M:\conda_envs\langchain_agent\python.exe"
if (Test-Path $LangchainPy) {
    $python = $LangchainPy
} else {
    $python = "python"
}

$argsList = @("run_eval.py")
if ($E2e) { $argsList += "--e2e" }
if ($Rag) { $argsList += "--rag" }
if ($SkipUnit) { $argsList += "--skip-unit" }
if ($Token) { $argsList += "--token"; $argsList += $Token }
if ($Chat) { $argsList += "--chat"; $argsList += $Chat }

& $python @argsList
exit $LASTEXITCODE
