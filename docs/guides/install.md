# Install and use the WSO2 CLI

The installer adds the `ws` command to your shell. It prints the name it
installed; use that name if yours differs. These steps need no administrator
rights.

## Install

macOS, Linux, and WSL:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

Windows (PowerShell):

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Open a new terminal, then check the installation:

```sh
ws version
```

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

## Uninstall

Run `ws logout` first if you signed in. Then run the uninstaller for your
platform:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/uninstall.sh | bash
```

```powershell
iwr https://wso2.github.io/wso2-cli/uninstall.ps1 -useb | iex
```

The uninstaller leaves your contexts and installed products in `~/.wso2`.

For manual installation, pinned versions, completion, verification details,
and removal options, see [Installer details](../reference/installer.md).
