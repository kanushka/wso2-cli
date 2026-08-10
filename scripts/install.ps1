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
Installs the wso2 shell on Windows.

.DESCRIPTION
Downloads the published archive for this machine, verifies it against the
checksum file published beside it, and installs the binary under the WSO2 state
root with its directory added to the per-user PATH.

Nothing here needs administrator rights: no symbolic link is created, no machine
level environment variable is written, and no installer is registered. A run that
cannot verify what it downloaded installs nothing.

The artifact names, the checksum file, and the tag resolution this depends on are
documented in docs/reference/release-artifacts.md.

.PARAMETER Version
The release tag to install, such as v0.1.0. Defaults to the newest stable
release.

.EXAMPLE
iwr <install url> -useb | iex

.EXAMPLE
&([scriptblock]::Create((iwr <install url> -useb))) v0.1.0

.NOTES
Environment variables it reads:

  WSO2_HOME                  State root to install into. Default ~\.wso2.
  WSO2_CLI_PRERELEASE=true   Resolve the newest prerelease, not the newest
                             stable release.
  WSO2_CLI_NO_PROFILE=1      Install without changing any environment variable.
  WSO2_CLI_RELEASE_BASE_URL  Where releases are downloaded from. Overridden by
  WSO2_CLI_RELEASE_API_URL   the tests; users have no reason to set either.
#>
param(
    [string] $Version
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
# Invoke-WebRequest on Windows PowerShell 5.1 renders a progress bar that
# throttles the download itself, badly enough to dominate the transfer time.
$ProgressPreference = 'SilentlyContinue'

$BlockName = 'wso2 cli'

function Get-ReleaseBaseUrl {
    if ($env:WSO2_CLI_RELEASE_BASE_URL) { return $env:WSO2_CLI_RELEASE_BASE_URL }
    return 'https://github.com/wso2/wso2-cli/releases'
}

function Get-ReleaseApiUrl {
    if ($env:WSO2_CLI_RELEASE_API_URL) { return $env:WSO2_CLI_RELEASE_API_URL }
    return 'https://api.github.com/repos/wso2/wso2-cli/releases'
}

function Stop-WithError {
    param([string] $Message)
    # Written to the error stream and exited non-zero, so a script that pipes this
    # installer can tell a refusal from a success.
    [Console]::Error.WriteLine("error: $Message")
    exit 1
}

# Resolve-Architecture maps what Windows calls this machine onto the architecture
# names the release artifacts use.
#
# PROCESSOR_ARCHITEW6432 is read first: a 32-bit PowerShell on 64-bit Windows
# reports x86 in PROCESSOR_ARCHITECTURE and the real architecture only there, so
# reading the latter alone would install a 32-bit binary on a 64-bit machine.
function Resolve-Architecture {
    $machine = $env:PROCESSOR_ARCHITEW6432
    if (-not $machine) { $machine = $env:PROCESSOR_ARCHITECTURE }
    if (-not $machine) { Stop-WithError 'could not determine this machine''s architecture.' }

    switch ($machine.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        'X86' { return '386' }
        default {
            Stop-WithError "unsupported architecture: $machine. Supported: AMD64, ARM64, x86."
        }
    }
}

# Resolve-Version reports the release tag to install. An explicit argument wins.
# Otherwise the newest stable tag comes from the redirect on the release page's
# /latest, which needs no API token; the prerelease opt-in has to read the release
# listing instead, because /latest deliberately skips prereleases.
function Resolve-Version {
    param([string] $Requested)

    if ($Requested) { return $Requested }

    if ($env:WSO2_CLI_PRERELEASE -eq 'true') {
        $url = Get-ReleaseApiUrl
        try {
            $releases = Invoke-RestMethod -Uri $url -UseBasicParsing
        } catch {
            Stop-WithError "could not read the release listing at ${url}: $($_.Exception.Message)"
        }
        # The listing is newest first, so the first prerelease in it is the newest.
        foreach ($release in $releases) {
            if ($release.prerelease) { return $release.tag_name }
        }
        Stop-WithError "no prerelease was found at $url."
    }

    $url = "$(Get-ReleaseBaseUrl)/latest"
    try {
        $response = Invoke-WebRequest -Uri $url -UseBasicParsing
    } catch {
        Stop-WithError "could not reach $url to find the newest release: $($_.Exception.Message)"
    }

    # The property that carries the URL after redirects differs between Windows
    # PowerShell and PowerShell 7, and this script supports both.
    $final = $null
    if ($response.BaseResponse.PSObject.Properties['ResponseUri']) {
        $final = $response.BaseResponse.ResponseUri
    }
    if (-not $final -and $response.BaseResponse.PSObject.Properties['RequestMessage']) {
        $final = $response.BaseResponse.RequestMessage.RequestUri
    }
    if (-not $final) { Stop-WithError "could not determine the latest release from $url." }

    $tag = ($final.AbsoluteUri.TrimEnd('/') -split '/')[-1]
    if (-not $tag -or $tag -eq 'latest') {
        Stop-WithError "could not determine the latest release from $url."
    }
    return $tag
}

# Assert-Checksum refuses anything whose SHA-256 does not match the checksum
# published beside it, before the archive is extracted.
#
# The published name is compared exactly rather than searched for: a name that
# merely ends with this archive's would otherwise supply the wrong digest and
# refuse a release that is perfectly good.
function Assert-Checksum {
    param([string] $ArchivePath, [string] $ChecksumPath, [string] $ArchiveName)

    $expected = $null
    foreach ($line in Get-Content -LiteralPath $ChecksumPath) {
        $fields = -split $line
        if ($fields.Count -lt 2) { continue }
        $name = $fields[1].TrimStart('*')
        if ($name -eq $ArchiveName) {
            $expected = $fields[0]
            break
        }
    }
    if (-not $expected) {
        Stop-WithError "checksums.txt does not list $ArchiveName, so the download cannot be verified."
    }

    $actual = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash
    # Hex case is not part of the value: Get-FileHash reports upper case and the
    # published file is lower case.
    if ($expected -ine $actual) {
        [Console]::Error.WriteLine("error: checksum mismatch for $ArchiveName")
        [Console]::Error.WriteLine("  expected $expected")
        [Console]::Error.WriteLine("  actual   $actual")
        Stop-WithError 'refusing to install an archive that failed verification.'
    }
    Write-Output 'Checksum verified.'
}

# Add-ToUserPath puts the binary directory on the per-user PATH, which needs no
# elevation, and on the current session's PATH so the command works without
# reopening the terminal.
#
# An entry that is already there is left alone rather than appended again.
function Add-ToUserPath {
    param([string] $Directory)

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { $userPath = '' }

    $present = $false
    foreach ($entry in $userPath -split ';') {
        if ($entry.Trim().TrimEnd('\') -ieq $Directory.TrimEnd('\')) {
            $present = $true
            break
        }
    }

    if ($present) {
        Write-Output "PATH already contains $Directory."
    } else {
        $updated = if ($userPath.TrimEnd(';')) { "$($userPath.TrimEnd(';'));$Directory" } else { $Directory }
        [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
        Write-Output "Added $Directory to your user PATH."
    }

    # The current session's PATH, so the command works without reopening the
    # terminal. The array subexpression is required rather than decorative: a
    # pipeline that matches one entry or none returns a scalar or $null, and
    # reading .Count off either is an error under Set-StrictMode.
    $sessionPath = if ($env:Path) { $env:Path } else { '' }
    $alreadyThere = @($sessionPath -split ';' |
        Where-Object { $_.Trim().TrimEnd('\') -ieq $Directory.TrimEnd('\') })
    if ($alreadyThere.Count -eq 0) {
        $env:Path = if ($sessionPath.TrimEnd(';')) { "$($sessionPath.TrimEnd(';'));$Directory" } else { $Directory }
    }
}

function Write-ManualPathInstructions {
    param([string] $StateRoot, [string] $BinDir, [string] $Reason)
    Write-Output ''
    Write-Output $Reason
    Write-Output 'Set these for yourself to run wso2 by name:'
    Write-Output ''
    Write-Output "    `$env:WSO2_HOME = '$StateRoot'"
    Write-Output "    `$env:Path += ';$BinDir'"
}

function Invoke-Install {
    param([string] $Requested)

    $arch = Resolve-Architecture
    $tag = Resolve-Version -Requested $Requested

    $stateRoot = if ($env:WSO2_HOME) { $env:WSO2_HOME } else { Join-Path $HOME '.wso2' }
    $binDir = Join-Path $stateRoot 'bin'
    $archiveName = "wso2-cli-$tag-windows-$arch.zip"
    $url = "$(Get-ReleaseBaseUrl)/download/$tag/$archiveName"

    Write-Output "Installing the WSO2 CLI $tag for windows/$arch."

    # Everything downloaded lands in a directory removed however this script
    # exits, so a failed verification leaves nothing behind to run.
    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("wso2-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

    try {
        $archivePath = Join-Path $tempDir $archiveName
        $checksumPath = Join-Path $tempDir 'checksums.txt'

        Write-Output "Downloading $url"
        try {
            Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing
        } catch {
            Stop-WithError "could not download $url. Check that $tag is a published release."
        }
        try {
            Invoke-WebRequest -Uri "$(Get-ReleaseBaseUrl)/download/$tag/checksums.txt" `
                -OutFile $checksumPath -UseBasicParsing
        } catch {
            Stop-WithError "could not download the checksum file for $tag, so the archive cannot be verified."
        }

        Assert-Checksum -ArchivePath $archivePath -ChecksumPath $checksumPath -ArchiveName $archiveName

        $unpacked = Join-Path $tempDir 'unpacked'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $unpacked -Force
        $extracted = Join-Path $unpacked 'wso2.exe'
        if (-not (Test-Path -LiteralPath $extracted)) {
            Stop-WithError 'the archive did not contain the expected wso2.exe binary.'
        }

        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
        # Replacing the binary through a staged copy beside its final path keeps a
        # failed move from leaving a half-written executable where the finished one
        # belongs. The staging name carries this process id so two runs at once
        # cannot stage onto each other.
        $staged = Join-Path $binDir (".wso2.install.$PID.exe")
        Move-Item -LiteralPath $extracted -Destination $staged -Force
        $installed = Join-Path $binDir 'wso2.exe'
        try {
            Move-Item -LiteralPath $staged -Destination $installed -Force
        } catch {
            Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
            Stop-WithError "could not replace ${installed}: $($_.Exception.Message). Close any running wso2 and try again."
        }
        Write-Output "Installed $installed"

        if ($env:WSO2_CLI_NO_PROFILE) {
            Write-ManualPathInstructions -StateRoot $stateRoot -BinDir $binDir `
                -Reason 'Left your environment untouched, as asked.'
        } else {
            # The state root is recorded, not just used: an installation under a
            # non-default WSO2_HOME would otherwise leave the installed shell reading
            # its state from the default root.
            [Environment]::SetEnvironmentVariable('WSO2_HOME', $stateRoot, 'User')
            $env:WSO2_HOME = $stateRoot
            Add-ToUserPath -Directory $binDir
        }

        Write-Output ''
        Write-Output "The WSO2 CLI $tag is installed. Run: wso2 --help"
    } finally {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Invoke-Install -Requested $Version
