function Get-BapGoCommand {
    param([switch]$Required)

    $command = Get-Command go.exe -ErrorAction SilentlyContinue
    if (-not $command) { $command = Get-Command go -ErrorAction SilentlyContinue }
    if ($command) { return $command.Source }

    $candidates = @()
    if ($env:ProgramFiles) { $candidates += Join-Path $env:ProgramFiles 'Go\bin\go.exe' }
    if (${env:ProgramFiles(x86)}) { $candidates += Join-Path ${env:ProgramFiles(x86)} 'Go\bin\go.exe' }
    if ($env:LOCALAPPDATA) { $candidates += Join-Path $env:LOCALAPPDATA 'Programs\Go\bin\go.exe' }
    if ($env:USERPROFILE) { $candidates += Join-Path $env:USERPROFILE 'scoop\apps\go\current\bin\go.exe' }
    $candidates += 'C:\ProgramData\chocolatey\bin\go.exe'

    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    if ($Required) {
        throw 'Go 1.23.12 or newer was not found on PATH or in a standard Windows installation location.'
    }
    return $null
}
