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

$argsList = @("run_eval.py")
if ($E2e) { $argsList += "--e2e" }
if ($Rag) { $argsList += "--rag" }
if ($SkipUnit) { $argsList += "--skip-unit" }
if ($Token) { $argsList += "--token"; $argsList += $Token }
if ($Chat) { $argsList += "--chat"; $argsList += $Chat }

python @argsList
exit $LASTEXITCODE
