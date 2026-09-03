<#
.SYNOPSIS
Checks PowerShell syntax, documented flags, compatibility aliases, and batch forwarding.
#>
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$failures = [Collections.Generic.List[string]]::new()
$parametersByScript = @{}
foreach ($file in Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.ps1' -File) {
    $tokens = $null
    $parseErrors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($file.FullName, [ref]$tokens, [ref]$parseErrors)
    foreach ($parseError in @($parseErrors)) {
        $failures.Add("$($file.Name):$($parseError.Extent.StartLineNumber): $($parseError.Message)")
    }
    $names = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    if ($ast.ParamBlock) {
        foreach ($parameter in $ast.ParamBlock.Parameters) {
            [void]$names.Add($parameter.Name.VariablePath.UserPath)
            foreach ($attribute in $parameter.Attributes | Where-Object { $_.TypeName.Name -eq 'Alias' }) {
                foreach ($argument in $attribute.PositionalArguments) {
                    [void]$names.Add([string]$argument.SafeGetValue())
                }
            }
        }
    }
    $parametersByScript[$file.Name] = $names
    if ($names.Contains('Target') -and (Get-Content -LiteralPath $file.FullName -Raw) -match '(?i)foreach\s*\(\s*\$target\s+in\b') {
        $failures.Add("$($file.Name): loop variable `$target collides with the -Target parameter in case-insensitive PowerShell")
    }
}

$commonParameters = @('Verbose', 'Debug', 'ErrorAction', 'WarningAction', 'InformationAction', 'ProgressAction', 'ErrorVariable', 'WarningVariable', 'InformationVariable', 'OutVariable', 'OutBuffer', 'PipelineVariable', 'WhatIf', 'Confirm', 'Syntax')
foreach ($document in Get-ChildItem -Path @((Join-Path $PSScriptRoot 'README.md'), (Join-Path $PSScriptRoot 'docs')) -Recurse -File -Filter '*.md') {
    $lineNumber = 0
    foreach ($line in Get-Content -LiteralPath $document.FullName) {
        $lineNumber++
        foreach ($invocation in [regex]::Matches($line, '\.\\(?<script>[A-Za-z0-9-]+\.ps1)(?<tail>.*)$')) {
            $scriptName = $invocation.Groups['script'].Value
            if (-not $parametersByScript.ContainsKey($scriptName)) {
                $failures.Add("$($document.FullName):${lineNumber}: documents missing script $scriptName")
                continue
            }
            foreach ($flagMatch in [regex]::Matches($invocation.Groups['tail'].Value, '(?<!\S)-(?<flag>[A-Za-z][A-Za-z0-9]+)')) {
                $flag = $flagMatch.Groups['flag'].Value
                if ($flag -notin $commonParameters -and -not $parametersByScript[$scriptName].Contains($flag)) {
                    $failures.Add("$($document.FullName):${lineNumber}: $scriptName has no -$flag flag")
                }
            }
        }
    }
}

foreach ($wrapper in Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.bat' -File) {
    $content = Get-Content -LiteralPath $wrapper.FullName -Raw
    if ($content -match '(?i)powershell(?:\.exe)?' -and $content -match '(?i)\.ps1' -and $content -notmatch '%\*') {
        $failures.Add("$($wrapper.Name): does not forward command-line flags with %*")
    }
    if ($content -notmatch '(?im)^exit /b ') {
        $failures.Add("$($wrapper.Name): does not return the wrapped script exit code")
    }
}

foreach ($contract in @(
    @{ Script = 'Build-BapEdge.ps1'; Current = 'Target'; Alias = 'Targets' },
    @{ Script = 'Build-BapEdge-Native.ps1'; Current = 'Target'; Alias = 'Targets' },
    @{ Script = 'Build-BapService.ps1'; Current = 'Target'; Alias = 'NativeTarget' }
)) {
    $names = $parametersByScript[$contract.Script]
    if (-not $names.Contains($contract.Current) -or -not $names.Contains($contract.Alias)) {
        $failures.Add("$($contract.Script): expected -$($contract.Current) with compatibility alias -$($contract.Alias)")
    }
}

foreach ($scriptName in @('Start-BapNativeLocal.ps1', 'Start-LocalClaude.ps1', 'Capture-ClaudeFixtures.ps1')) {
    $names = $parametersByScript[$scriptName]
    foreach ($requiredFlag in @('UseCompanyClaude', 'InteractiveClaude', 'CompanyCliArguments')) {
        if (-not $names.Contains($requiredFlag)) {
            $failures.Add("${scriptName}: missing consistent company launcher flag -$requiredFlag")
        }
    }
}

if ($failures.Count) {
    $message = 'Script/document contract failures:' + [Environment]::NewLine + ' - ' + ($failures -join ([Environment]::NewLine + ' - '))
    throw $message
}
Write-Host 'PASS: PowerShell syntax, documented flags, build aliases, and batch argument forwarding are consistent.'
