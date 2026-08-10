# Installing the WSO2 CLI

**Status:** Working draft
**Related:** [Release artifacts](../reference/release-artifacts.md),
[distribution research](../research/root-cli-installation-distribution.md)
**Last reviewed:** 2026-08-11

This guide installs the `wso2` shell. It covers the one-command install, the
manual alternative for anyone who will not pipe a script to a shell, and how to
pin, upgrade, and remove it.

## What this channel gives you, and what it does not

The installer downloads a published release and verifies it against the SHA-256
checksum file published beside it. A download that fails verification is not
installed.

The binaries are **not code signed or notarized**. macOS Gatekeeper and Windows
SmartScreen may warn about them, and integrity rests on that checksum file and
on HTTPS. Signed, per-platform channels — Homebrew, WinGet, APT, RPM — are
the intended destination and are described in the
[distribution research](../research/root-cli-installation-distribution.md); this
channel exists so the CLI is installable before they are ready.

Nothing in the install needs administrator rights, on any platform.

## Install

### macOS, Linux, and WSL

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

### Windows

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Open a new terminal afterwards, or re-source the profile the script names, and
check what you have:

```sh
wso2 version
```

### Supported platforms

| Operating system | Architectures                  |
| ---------------- | ------------------------------ |
| Linux            | `amd64`, `arm64`, `arm`, `386` |
| macOS            | `amd64`, `arm64`               |
| Windows          | `amd64`, `arm64`               |

An unsupported operating system or architecture is refused, naming what was
detected, rather than installed and left to fail later.

## Read the script first

Both scripts are plain text at the URLs above, and reading one before running it
is a reasonable thing to want:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | less
```

They are versioned in this repository at
[`scripts/install.sh`](../../scripts/install.sh) and
[`scripts/install.ps1`](../../scripts/install.ps1), and what is served is what
is in the repository.

## Install without running a remote script

Every release carries the same archives the script downloads, so nothing is lost
by doing it by hand.

1. Open the [releases page](https://github.com/wso2/wso2-cli/releases) and note
   the tag you want.
2. Download the archive for your platform and the `checksums.txt` beside it. The
   naming convention is in
   [release artifacts](../reference/release-artifacts.md).
3. Verify the archive, and do not continue unless it passes:

   ```sh
   sha256sum --check --ignore-missing checksums.txt
   ```

   On macOS, `shasum -a 256 -c checksums.txt` does the same. On Windows,
   `Get-FileHash -Algorithm SHA256 <archive>` prints the digest to compare
   against the line in `checksums.txt`.
4. Extract it and put the `wso2` binary somewhere on your `PATH`. The installer
   uses `~/.wso2/bin`, and there is nothing special about that location.

## Pin a version

Installing whatever is newest is the wrong default for a build. Pass the tag:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash -s v0.1.0
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/install.ps1 -useb))) v0.1.0
```

## Install a release candidate

Prereleases are skipped when resolving the newest release, so asking for one is
explicit:

```sh
WSO2_CLI_PRERELEASE=true curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

```powershell
$env:WSO2_CLI_PRERELEASE = 'true'
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

## Upgrade

Run the installer again. It replaces the binary in place and does not add a
second entry to your profile or `PATH`. There is no self-update command.

## Where things go, and how to change it

| What                | Where                            |
| ------------------- | -------------------------------- |
| The binary          | `$WSO2_HOME/bin`                 |
| State root default  | `~/.wso2`                        |
| Contexts and state  | Under the state root             |

Set `WSO2_HOME` before installing to put everything somewhere else:

```sh
WSO2_HOME=/opt/wso2 curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

The installer records the state root it used, so the shell it installs and the
state it reads cannot disagree about where they live.

### Keep your shell profile to yourself

By default the Unix installer appends one delimited block to the shell profile
it detects, and the Windows installer sets your per-user `PATH` and
`WSO2_HOME`. To install without either, and be told what to set yourself:

```sh
WSO2_CLI_NO_PROFILE=1 curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

The block is delimited and greppable, so you can always find what was added:

```text
# >>> wso2 cli >>>
export WSO2_HOME="/home/you/.wso2"
export PATH="/home/you/.wso2/bin:$PATH"
# <<< wso2 cli <<<
```

## Uninstall

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash
```

```powershell
iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb | iex
```

This removes the binary, the directory the installer created for it, and the
profile block or environment entries it added. It **keeps your configuration,
contexts, and credentials**, and tells you where they are. To remove those too:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash -s -- --purge
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb))) -Purge
```

Uninstalling when nothing is installed is not an error: it reports that there
was nothing to do, which also makes it the way to clean up after an install that
failed halfway.

## If something goes wrong

**`wso2: command not found` right after installing.** The profile change applies
to new shells. Open a new terminal, or run the `source` command the installer
printed.

**A checksum mismatch.** The install stops and nothing is written. Retry once,
in case the download was truncated. If it happens again, do not work around
it — open an issue with the tag and platform, since a released archive not
matching its published checksum is a problem worth knowing about.

**Windows says it cannot replace the binary.** Something is running it. Close
any `wso2` process and run the installer again.
