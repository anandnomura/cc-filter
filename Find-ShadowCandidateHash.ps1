param(
    [string]$Command = '',
    [string]$OutsideWorkspacePath = '',
    [string]$TargetHash = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ($PSBoundParameters.ContainsKey('Command') -eq $PSBoundParameters.ContainsKey('OutsideWorkspacePath')) {
    throw 'Specify exactly one of -Command <string> or -OutsideWorkspacePath <string>.'
}

function Get-Sha256Hex {
    param([Parameter(Mandatory)][string]$Text)
    $bytes = [Text.Encoding]::UTF8.GetBytes($Text)
    $hasher = [Security.Cryptography.SHA256]::Create()
    try {
        $hashBytes = $hasher.ComputeHash($bytes)
        return -join ($hashBytes | ForEach-Object { $_.ToString('x2') })
    } finally {
        $hasher.Dispose()
    }
}

$calculated = ''
$type = ''
$inputVal = ''

if ($Command) {
    $hex = Get-Sha256Hex -Text $Command
    $calculated = "command-sha256:$hex"
    $type = 'Command'
    $inputVal = $Command
} else {
    $normalized = $OutsideWorkspacePath.Replace('\', '/')
    $hex = Get-Sha256Hex -Text $normalized
    $calculated = "outside-workspace-sha256:$hex"
    $type = 'OutsideWorkspacePath'
    $inputVal = $OutsideWorkspacePath
}

$matched = $false
if ($TargetHash) {
    $cleanTarget = $TargetHash.Trim().ToLowerInvariant()
    $calculatedLower = $calculated.ToLowerInvariant()
    $hexOnly = $calculated.Split(':', 2)[-1].ToLowerInvariant()
    $matched = ($calculatedLower -eq $cleanTarget -or $hexOnly -eq $cleanTarget)
}

$result = [ordered]@{
    InputType     = $type
    Input         = $inputVal
    TargetSummary = $calculated
}

if ($TargetHash) {
    $result['TargetHash'] = $TargetHash
    $result['Matches'] = $matched
}

[PSCustomObject]$result
if ($TargetHash -and -not $matched) {
    throw 'NO MATCH: the supplied candidate does not produce the requested target hash.'
}
