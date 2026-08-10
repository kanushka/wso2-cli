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
entry, and the per-user WSO2_HOME variable. It does not remove configuration,
contexts, or credentials unless -Purge is given: removing a binary is not the
same decision as abandoning a setup.

Running it when nothing is installed is not a failure. It reports what it found
and exits successfully, which is also what makes it usable to clean up after an
install that failed halfway.

Nothing here needs administrator rights.

.PARAMETER Purge
Also remove configuration, contexts, and credentials.
#>
param(
    [switch] $Purge
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$stateRoot = if ($env:WSO2_HOME) { $env:WSO2_HOME } else { Join-Path $HOME '.wso2' }
$binDir = Join-Path $stateRoot 'bin'
$removed = $false

$installed = Join-Path $binDir 'wso2.exe'
if (Test-Path -LiteralPath $installed) {
    try {
        Remove-Item -LiteralPath $installed -Force
    } catch {
        [Console]::Error.WriteLine("error: could not remove ${installed}: $($_.Exception.Message). Close any running wso2 and try again.")
        exit 1
    }
    Write-Output "Removed $installed"
    $removed = $true
}

# Any staging file an interrupted install left beside the binary.
Get-ChildItem -LiteralPath $binDir -Filter '.wso2.install.*' -Force -ErrorAction SilentlyContinue |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }

# Only if it is empty. A directory holding something this installer did not put
# there is not this script's to delete.
if ((Test-Path -LiteralPath $binDir) -and
    -not (Get-ChildItem -LiteralPath $binDir -Force -ErrorAction SilentlyContinue)) {
    Remove-Item -LiteralPath $binDir -Force
    Write-Output "Removed $binDir"
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

if ($Purge) {
    if (Test-Path -LiteralPath $stateRoot) {
        Remove-Item -LiteralPath $stateRoot -Recurse -Force
        Write-Output "Removed $stateRoot, including configuration and credentials."
        $removed = $true
    }
} elseif (Test-Path -LiteralPath $stateRoot) {
    # Named explicitly rather than left implicit: someone who wanted everything
    # gone needs to know that something is still there and how to remove it.
    Write-Output ''
    Write-Output "Left $stateRoot in place, with your contexts and credentials."
    Write-Output 'Remove it too with: .\uninstall.ps1 -Purge'
}

if (-not $removed) {
    Write-Output "Nothing to remove: no wso2 installation was found under $stateRoot."
} else {
    Write-Output ''
    Write-Output 'Open a new terminal so the PATH change takes effect.'
}
