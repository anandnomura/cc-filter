param(
    [Parameter(Mandatory)][string]$InputDirectory,
    [string]$OutputPath = '',
    [ValidateRange(1, 100000)][int]$MinCount = 2,
    [switch]$DisableML
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $InputDirectory -PathType Container)) {
    throw "Shadow input directory does not exist: $InputDirectory"
}
$python = Get-Command py -ErrorAction SilentlyContinue
$arguments = @('-3', (Join-Path $PSScriptRoot 'scripts\analyze_shadow.py'), (Resolve-Path -LiteralPath $InputDirectory).Path, '--min-count', $MinCount)
if ($null -eq $python) {
    $python = Get-Command python -ErrorAction SilentlyContinue
    $arguments = @((Join-Path $PSScriptRoot 'scripts\analyze_shadow.py'), (Resolve-Path -LiteralPath $InputDirectory).Path, '--min-count', $MinCount)
}
if ($null -eq $python) { throw 'Python 3 is required for shadow analysis.' }
if ($OutputPath) { $arguments += @('--output', $OutputPath) }
if ($DisableML) { $arguments += '--disable-ml' }
& $python.Source @arguments
if ($LASTEXITCODE -ne 0) { throw 'Shadow analysis failed.' }
if ($OutputPath) { Write-Host "Human-review shadow suggestions: $OutputPath" }
