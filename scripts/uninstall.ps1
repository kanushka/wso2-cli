# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

<#
.SYNOPSIS
Removes what scripts/install.ps1 added.

.DESCRIPTION
Removes the binary, the directory the installer created for it, the per-user PATH
entry, the per-user WSO2_HOME variable, and the tab completion block in the
PowerShell profile. It does not remove configuration or contexts unless -Purge
is given: removing a binary is not the same decision as abandoning a setup.

Running it when nothing is installed is not a failure. It reports what it found
and exits successfully, which is also what makes it usable to clean up after an
install that failed halfway.

Nothing here needs administrator rights.

.PARAMETER Purge
Also remove configuration and contexts. Sessions in the OS secure store are not touched: run logout first.
#>
param(
    [switch] $Purge
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$stateRoot = if ($env:WSO2_HOME) { $env:WSO2_HOME } else { Join-Path $HOME '.wso2' }
$binDir = Join-Path $stateRoot 'bin'
$removed = $false

# The binary, under the name the installer recorded; an install from before
# the record was named wso2.
$nameRecord = Join-Path $binDir '.cli-name'
$cliName = 'wso2'
if (Test-Path -LiteralPath $nameRecord) {
    $recorded = (Get-Content -LiteralPath $nameRecord -TotalCount 1)
    if ($recorded -and $recorded -notmatch '[\\/]' -and $recorded -notin @('.', '..')) {
        $cliName = $recorded.Trim()
    }
}
$installed = Join-Path $binDir "$cliName.exe"
if (Test-Path -LiteralPath $installed) {
    try {
        Remove-Item -LiteralPath $installed -Force
    } catch {
        [Console]::Error.WriteLine("error: could not remove ${installed}: $($_.Exception.Message). Close any running $cliName and try again.")
        exit 1
    }
    Write-Output "Removed $installed"
    $removed = $true
}

# The name record, and any staging file an interrupted install left beside the binary.
Remove-Item -LiteralPath $nameRecord -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $binDir -Filter '.wso2.install.*' -Force -ErrorAction SilentlyContinue |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }

# Only if it is empty. A directory holding something this installer did not put
# there is not this script's to delete.
if ((Test-Path -LiteralPath $binDir) -and
    -not (Get-ChildItem -LiteralPath $binDir -Force -ErrorAction SilentlyContinue)) {
    Remove-Item -LiteralPath $binDir -Force
    Write-Output "Removed $binDir"
    # Counted, so the summary cannot end with "nothing to remove" after saying
    # what it removed.
    $removed = $true
}

# The PATH entry, matched the way the installer wrote it: case-insensitively and
# ignoring a trailing separator, so the entry is found however it was recorded.
# Every other entry is written back exactly as it was.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath) {
    $target = $binDir.TrimEnd('\')
    $kept = @()
    $dropped = 0
    foreach ($entry in $userPath -split ';') {
        if ($entry.Trim() -and $entry.Trim().TrimEnd('\') -ieq $target) {
            $dropped++
        } else {
            $kept += $entry
        }
    }
    if ($dropped -gt 0) {
        [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), 'User')
        Write-Output "Removed $binDir from your user PATH."
        $removed = $true
    }
}

# Only when it is this state root. A WSO2_HOME pointing somewhere else was set by
# someone for a reason, and clearing it would be removing a decision that is not
# this script's to reverse.
$userStateRoot = [Environment]::GetEnvironmentVariable('WSO2_HOME', 'User')
if ($userStateRoot -and $userStateRoot.TrimEnd('\') -ieq $stateRoot.TrimEnd('\')) {
    [Environment]::SetEnvironmentVariable('WSO2_HOME', $null, 'User')
    Write-Output 'Removed the user WSO2_HOME variable.'
    $removed = $true
}

# The tab completion block `completion install` wrote into a PowerShell profile.
# Both editions' profiles are checked, because the install may have run under
# the other one. Only the lines between the markers go. A profile whose markers
# do not pair up, start before end, is left alone rather than guessed at.
#
# The profile is rewritten byte for byte apart from the block: it is read as
# Latin-1, which maps every byte to one character, so UTF-8 with or without a
# BOM and the ANSI code page all survive; only UTF-16, which PowerShell also
# writes, is read as what it is. The rewrite goes through a temporary file
# beside the profile, so an interrupted run cannot truncate it.
$BlockBegin = '# >>> wso2 cli >>>'
$BlockEnd = '# <<< wso2 cli <<<'
$profiles = @()
if ($env:WSO2_CLI_POWERSHELL_PROFILE) {
    $profiles += $env:WSO2_CLI_POWERSHELL_PROFILE
} else {
    $documents = [Environment]::GetFolderPath('MyDocuments')
    $profiles += $PROFILE
    foreach ($edition in @('PowerShell', 'WindowsPowerShell')) {
        $profiles += Join-Path (Join-Path $documents $edition) 'Microsoft.PowerShell_profile.ps1'
    }
}
foreach ($profilePath in ($profiles | Select-Object -Unique)) {
    if (-not $profilePath -or -not (Test-Path -LiteralPath $profilePath)) { continue }
    $bytes = [System.IO.File]::ReadAllBytes($profilePath)
    $encoding = [System.Text.Encoding]::GetEncoding(28591)
    if ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFF -and $bytes[1] -eq 0xFE) {
        $encoding = [System.Text.Encoding]::Unicode
    } elseif ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFE -and $bytes[1] -eq 0xFF) {
        $encoding = [System.Text.Encoding]::BigEndianUnicode
    }
    $lines = $encoding.GetString($bytes) -split "`n"

    $kept = New-Object System.Collections.Generic.List[string]
    $inside = $false
    $found = $false
    $malformed = $false
    foreach ($line in $lines) {
        $bare = $line.TrimEnd("`r")
        if ($bare -ceq $BlockBegin) {
            if ($inside) { $malformed = $true; break }
            $inside = $true
            $found = $true
            continue
        }
        if ($bare -ceq $BlockEnd) {
            if (-not $inside) { $malformed = $true; break }
            $inside = $false
            continue
        }
        if (-not $inside) { $kept.Add($line) }
    }
    if ($inside) { $malformed = $true }
    if (-not $found -and -not $malformed) { continue }
    if ($malformed) {
        [Console]::Error.WriteLine("warning: the wso2 block markers in $profilePath do not pair up.")
        [Console]::Error.WriteLine("Left it alone rather than guessing where the block ends. Remove these lines by hand:")
        [Console]::Error.WriteLine("  $BlockBegin ... $BlockEnd")
        continue
    }

    $staged = "$profilePath.wso2-uninstall.$PID"
    [System.IO.File]::WriteAllBytes($staged, $encoding.GetBytes($kept -join "`n"))
    Move-Item -LiteralPath $staged -Destination $profilePath -Force
    Write-Output "Removed the wso2 block from $profilePath"
    $removed = $true
}

if ($Purge) {
    if (Test-Path -LiteralPath $stateRoot) {
        Remove-Item -LiteralPath $stateRoot -Recurse -Force
        Write-Output "Removed $stateRoot, including configuration and contexts."
        Write-Output "Sessions in the OS secure store are not touched: run logout first."
        $removed = $true
    }
} elseif (Test-Path -LiteralPath $stateRoot) {
    # Named explicitly rather than left implicit: someone who wanted everything
    # gone needs to know that something is still there and how to remove it.
    Write-Output ''
    Write-Output "Left $stateRoot in place, with your contexts and preferences."
    Write-Output 'Remove it too with: .\uninstall.ps1 -Purge'
}

if (-not $removed) {
    Write-Output "Nothing to remove: no WSO2 CLI installation was found under $stateRoot."
} else {
    Write-Output ''
    Write-Output 'Open a new terminal so the PATH change takes effect.'
}
