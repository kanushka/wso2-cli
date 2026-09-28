# Install and use the WSO2 CLI

The installer downloads a release from GitHub, checks it against the
published `checksums.txt`, and installs it under `~/.wso2`. It needs no
administrator rights. The binaries are not code signed, so macOS Gatekeeper or
Windows SmartScreen may warn.

Releases install the command as `ws` by default. The installer prints the name
it installed. These guides write `ws`, so use the name the installer printed.

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

## Install a product

Products add commands to the CLI. List what the catalog offers, then install
the product you need:

```sh
ws product list
ws product install <product>
ws product list
```

Replace `<product>` with a name from the list. The second list shows its
installed version. Add `--channel prerelease` to install a prerelease, or use
`ws product install <product>@<version>` to pin an exact version.

## Create a context and log in

A context records where to connect and how to sign in. In a terminal, run:

```sh
ws context create
```

Follow the prompts to name the context, choose a login method, and record the
products it reaches. Then check the result and sign in:

```sh
ws context show
ws login
ws whoami
```

If your team provides a context file, use
`ws context apply -f <file> --use <name>` instead. See the
[context file reference](../reference/context-file.md).

## Run product commands

An installed product has its own help and commands:

```sh
ws <product> --help
ws context list
ws context use <name>
```

Use `--context <name>` on a command to target another context without changing
the selected one. Run `ws context show` to check which context is selected and
which product endpoints it records.

## Update

```sh
ws product list
ws product update <product>
```

Run `ws product update --all` to update all installed products that follow a
channel. To update the CLI itself, run the installer again. Run `ws logout`
when you finish using a context. For more commands and flags, see the
[command reference](../reference/commands.md).

## What the installer verifies

The archive and `checksums.txt` come from the same GitHub release. The
installer finds the checksum line whose file name matches the archive exactly,
computes SHA-256, and refuses to extract on a mismatch or a missing line, so a
failed check installs nothing.

A matching checksum proves the archive is the one published beside it. It does
not establish:

- **The checksum file.** It is downloaded from the same release as the archive,
  so whoever can replace one can replace both. Authenticity rests on HTTPS and
  on control of this repository and its release workflow.
- **The install script.** `curl ... | bash` runs the response from GitHub Pages
  without checking a digest or signature first. Read `scripts/install.sh` here,
  or install by hand, if that matters for your environment.
- **The binary.** Releases are not signed or notarized, carry no provenance
  attestation or SBOM, and neither the installer nor the release workflow runs
  a malware or vulnerability scan. See
  [release artifacts](../reference/release-artifacts.md).

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
| `WSO2_CLI_NO_PROFILE=1` | Don't edit your shell profile (Unix) or user environment (Windows), and don't set up tab completion. The installer prints what to set. |

On Unix the installer adds this block to your shell profile, here for bash:

```text
# >>> wso2 cli >>>
export WSO2_HOME="/home/you/.wso2"
export PATH="/home/you/.wso2/bin:$PATH"
[ -x '/home/you/.wso2/bin/ws' ] && eval "$('/home/you/.wso2/bin/ws' completion bash)"
# <<< wso2 cli <<<
```

## Tab completion

The installers finish by running `ws completion install`, which sets up tab
completion for your shell. Run it yourself after installing some other way, or
for another shell:

```sh
ws completion install [bash|zsh|fish|powershell]
```

It detects the shell from `$SHELL` (PowerShell on Windows) and changes nothing
when completion is already set up:

- zsh: adds `source <('<path>' completion zsh)` to the block in `~/.zshrc`,
  after a `compinit` that runs only when nothing else has run one.
- bash: adds `eval "$('<path>' completion bash)"` to the block in `~/.bashrc`
  (or `~/.bash_profile`). Completion needs the `bash-completion` package.
- fish: writes `~/.config/fish/completions/ws.fish`, which runs
  `'<path>' completion fish | source`.
- PowerShell: adds `& '<path>' completion powershell | Out-String | Invoke-Expression`
  to the block in `$PROFILE`. An execution policy of `Restricted` or `AllSigned`
  would stop the profile loading, so it is refused.

`<path>` is the full path of the binary that ran `completion install`, quoted
for the shell. The line evaluates what that command prints, so it never runs
the command by name, which would evaluate whatever same-named program comes
first on `PATH`. Each line runs only while the binary is at that path, so a
moved or removed binary leaves a line that does nothing; run
`ws completion install` again to point it at the new place.

`--profile <file>` edits another file. Every line loads the script when a
terminal opens, so it never goes stale, and a product you install completes at
once. `ws completion <shell>` prints the script itself when its output is piped;
typed at a terminal it says how to set completion up, and `--print` prints the
script anyway.

## Uninstall

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash
```

```powershell
iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb | iex
```

This removes the binary, the profile block with the tab completion line in it,
and the fish completion file, and leaves everything under
`$WSO2_HOME` (contexts, preferences, installed products) in place.

Log out before you remove anything, because your sessions live in the OS
secure store and no uninstaller touches it:

```sh
ws logout
ws context delete <name>
```

`--purge` then deletes `$WSO2_HOME` itself. It cannot be undone, and a session
left in the keychain survives it. It refuses, removing nothing, when
`$WSO2_HOME` resolves to a filesystem or drive root, your home directory, or a
directory above it:

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
