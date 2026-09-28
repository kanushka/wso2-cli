# Use the WSO2 CLI

This guide covers installation, contexts, login, product commands, and updates.
The installer names the command `ws` by default. Use the name it prints if
yours differs.

## 1. Install the CLI

On macOS, Linux, or WSL:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

On Windows, in PowerShell:

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Open a new terminal, then check the installation:

```sh
ws version
ws --help
```

For other installation options, see [Install the WSO2 CLI](install.md).

## 2. Install a product

Products add commands to the CLI. List what the catalog offers, then install
the product you need:

```sh
ws product list
ws product install <product>
ws product list
```

Replace `<product>` with a name from the list. The second list shows its
installed version. To choose a prerelease, add `--channel prerelease` to the
install command.

## 3. Create a context and log in

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

If your team provides a context file, you can use
`ws context apply -f <file> --use <name>` instead. See the
[context file reference](../reference/context-file.md).

## 4. Run commands and switch contexts

An installed product has its own help and commands:

```sh
ws <product> --help
ws context list
ws context use <name>
```

Use `--context <name>` on a command to target another context without changing
the selected one. Run `ws context show` to check which context is selected and
which product endpoints it records.

## 5. Update

Check for updates and update one product:

```sh
ws product list
ws product update <product>
```

Run `ws product update --all` to update all installed products that follow a
channel. To update the CLI itself, run the installer from step 1 again.

When you finish using a context, run `ws logout` to end its sessions.
For more commands and flags, see the [command reference](../reference/commands.md).
