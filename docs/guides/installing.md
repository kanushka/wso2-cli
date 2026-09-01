# Installing the WSO2 CLI

**Status:** Working draft
**Related:** [Release artifacts](../reference/release-artifacts.md),
[distribution research](../research/root-cli-installation-distribution.md)
**Last reviewed:** 2026-09-01

Install the `wso2` shell, then install your first module.

## Install

**macOS, Linux, and WSL**

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

**Windows**

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Open a new terminal, or re-source the profile the script names, then check the
install:

```sh
wso2 version
```

Nothing in the install needs administrator rights.

### Supported platforms

| Operating system | Architectures                  |
| ---------------- | ------------------------------ |
| Linux            | `amd64`, `arm64`, `arm`, `386` |
| macOS            | `amd64`, `arm64`               |
| Windows          | `amd64`, `arm64`               |

On anything else the installer stops and prints what it detected.

> **On integrity.** The installer checks every download against the SHA-256
> checksum published beside the release, and installs nothing that fails. The
> binaries are not code signed or notarized, so macOS Gatekeeper and Windows
> SmartScreen may warn about them. Signed Homebrew, WinGet, APT, and RPM
> channels are planned; see the
> [distribution research](../research/root-cli-installation-distribution.md).

## Install your first module

The shell installs and runs modules. This repo publishes one, `reference`, for
testing the shell end to end. It stays on the `prerelease` channel, so the
stable channel never offers it.

```console
$ wso2 module available
MODULE      CHANNEL      VERSION
reference   prerelease   v0.1.0-rc.4

Run wso2 module install <module> to install one.
```

Install it by naming the channel:

```console
$ wso2 module install reference --channel prerelease
Installed reference v0.1.0-rc.4 for darwin/arm64.
The artifact was checked against the digest the catalog publishes. Artifacts are integrity-checked, not signed.
```

```console
$ wso2 module list
MODULE      INSTALLED     CHANNEL      UPDATE
reference   v0.1.0-rc.4   prerelease   current

Every installed module is current.
```

Module subcommands are separate from the shell's, and most need an
authenticated session:

```console
$ wso2 reference status
error: the "reference" module needs access, and no WSO2 CLI context is selected (auth.context_not_selected)
  Run wso2 context use <name> to select a configured context, or wso2 login --url <issuer> --client-id <id> to create an identity and a context. wso2 context list shows what is configured.
```

See [Logging in](login.md) to set that up. To remove a module:

```console
$ wso2 module remove reference --yes
Removed the reference module.
```

## Other ways to install

### Pin a version

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash -s v0.1.0
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/install.ps1 -useb))) v0.1.0
```

### Install a release candidate

Resolving the newest release skips prereleases, so ask for one explicitly. In a
pipeline the variable belongs on the `bash` that runs the script, not on the
`curl` that fetches it.

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | WSO2_CLI_PRERELEASE=true bash
```

```powershell
$env:WSO2_CLI_PRERELEASE = 'true'
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

### Read the script first

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | less
```

The served scripts are the ones in this repository:
[`scripts/install.sh`](../../scripts/install.sh) and
[`scripts/install.ps1`](../../scripts/install.ps1).

### Install without running a remote script

1. Open the [releases page](https://github.com/wso2/wso2-cli/releases) and pick
   a tag.
2. Download the archive for your platform and the `checksums.txt` beside it.
   [Release artifacts](../reference/release-artifacts.md) has the naming
   convention.
3. Verify the archive, and stop if it fails:

   ```sh
   sha256sum --check --ignore-missing checksums.txt
   ```

   On macOS use `shasum -a 256 --ignore-missing -c checksums.txt`. Keep
   `--ignore-missing`: `checksums.txt` lists every platform's archive, and
   without the flag the check fails over the ones you did not download. On
   Windows, `Get-FileHash -Algorithm SHA256 <archive>` prints the digest to
   compare against `checksums.txt`.
4. Extract it and put the `wso2` binary on your `PATH`.

## Upgrade

Run the installer again. It replaces the binary in place and does not add a
second entry to your profile or `PATH`. There is no self-update command.

## Configuration

| What               | Where            |
| ------------------ | ---------------- |
| The binary         | `$WSO2_HOME/bin` |
| State root default | `~/.wso2`        |
| Contexts and state | The state root   |

Set `WSO2_HOME` before installing to put everything somewhere else. The
installer records the state root it used, so the shell and its state cannot
disagree about where they live.

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | WSO2_HOME=/opt/wso2 bash
```

### Skip the shell profile change

By default the Unix installer appends one delimited block to the shell profile
it detects, and the Windows installer sets your per-user `PATH` and `WSO2_HOME`.
To skip that and be told what to set yourself:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | WSO2_CLI_NO_PROFILE=1 bash
```

The block is delimited, so you can always find what was added:

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

This removes the binary, the directory the installer created, and the profile
block or environment entries it added. It keeps your configuration, contexts,
and credentials, and tells you where they are. Add `--purge` to remove those
too:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash -s -- --purge
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb))) -Purge
```

Uninstalling when nothing is installed reports that there was nothing to do, so
it also cleans up after an install that failed halfway.

## Troubleshooting

**`wso2: command not found` right after installing.** The profile change applies
to new shells. Open a new terminal, or run the `source` command the installer
printed.

**A checksum mismatch.** The install stops and writes nothing. Retry once in
case the download was truncated. If it happens again, do not work around it:
open an issue with the tag and platform.

**Windows cannot replace the binary.** Something is running it. Close any `wso2`
process and run the installer again.
