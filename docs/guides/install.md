# Install the WSO2 CLI

The installer downloads a release from GitHub, checks it against the
published `checksums.txt`, and installs it under `~/.wso2`. It needs no
administrator rights. The binaries are not code signed, so macOS Gatekeeper or
Windows SmartScreen may warn.

Releases install the command as `ws` by default. The installer prints the name
it installed. These guides write `wso2`, so use the name the installer printed.

## Install

macOS, Linux, and WSL:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

Windows (PowerShell):

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Open a new terminal and run `<name> version`, with the name the installer
printed (`ws` for a stock release). Supported platforms: Linux (`amd64`, `arm64`, `arm`, `386`), macOS (`amd64`,
`arm64`), Windows (`amd64`, `arm64`).

To read the scripts first, see `scripts/install.sh` and `scripts/install.ps1`
in this repository.

## Install by hand

1. On the [releases page](https://github.com/wso2/wso2-cli/releases), download
   the archive for your platform and `checksums.txt`. The file names are listed
   in [release artifacts](../reference/release-artifacts.md).
2. Verify the archive:

   ```sh
   sha256sum --check --ignore-missing checksums.txt       # Linux
   shasum -a 256 --ignore-missing -c checksums.txt        # macOS
   ```

   On Windows, compare `Get-FileHash -Algorithm SHA256 <archive>` with the line
   in `checksums.txt`.
3. Extract the archive and put the binary on your `PATH`.

## Pin a version

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash -s v0.1.0
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/install.ps1 -useb))) v0.1.0
```

To install the newest prerelease, set `WSO2_CLI_PRERELEASE=true` on `bash`,
not on `curl`:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | WSO2_CLI_PRERELEASE=true bash
```

To upgrade, run the installer again.

## Install location

| Variable | Effect |
| --- | --- |
| `WSO2_HOME` | State root. Default `~/.wso2`. The binary goes in `$WSO2_HOME/bin`. |
| `WSO2_CLI_NO_PROFILE=1` | Don't edit your shell profile (Unix) or user environment (Windows). The installer prints what to set. |

On Unix the installer adds this block to your shell profile:

```text
# >>> wso2 cli >>>
export WSO2_HOME="/home/you/.wso2"
export PATH="/home/you/.wso2/bin:$PATH"
# <<< wso2 cli <<<
```

## Install a product

```sh
wso2 product list
wso2 product install reference
wso2 product remove reference --yes
```

`wso2 product install <product>@<version>` pins an exact version.
`--channel prerelease` installs from the prerelease channel.

## Uninstall

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash
```

```powershell
iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb | iex
```

This removes the binary and the profile block, and leaves everything under
`$WSO2_HOME` (contexts, preferences, installed products) in place.

Log out before you remove anything, because your sessions live in the OS
secure store and no uninstaller touches it:

```sh
wso2 logout
wso2 context delete <name>
```

`--purge` then deletes `$WSO2_HOME` itself. It cannot be undone, and a session
left in the keychain survives it:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash -s -- --purge
```

```powershell
&([scriptblock]::Create((iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb))) -Purge
```

## If the install fails

| Problem | Fix |
| --- | --- |
| `command not found` right after install | Open a new terminal, or run the `source` command the installer printed. |
| Checksum mismatch | Nothing was installed. Retry once. If it fails again, open an issue with the tag and platform. |
| Windows can't replace the binary | Close every running CLI process and run the installer again. |
