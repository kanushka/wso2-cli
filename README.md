# WSO2 CLI

The WSO2 CLI provides a common command-line entry point for WSO2 products. This
repository is the project home for its source code, SDK, and documentation.

> [!IMPORTANT]
> The project is in early development. Documentation may describe intended
> behavior before the corresponding implementation is available, and
> interfaces may change until they are identified as stable.

## Installation

macOS, Linux, and WSL:

```sh
curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash
```

Windows:

```powershell
iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex
```

Both scripts download a published release, verify it against the SHA-256 checksum
file published beside it, and install the binary under your WSO2 state root.
Neither needs administrator rights, and both are plain text at the URLs above if
you would rather read one before running it.

Released binaries are checksum-verified but not code signed or notarized.
Supported platforms are Linux on `amd64`, `arm64`, `arm`, and `386`, and macOS and
Windows on `amd64` and `arm64`.

The [installation guide](docs/guides/install.md) covers installing from the
release page without running a remote script, pinning a version, release
candidates, tab completion, where files go, and how to uninstall.

### Tab completion

The installers set up tab completion for your shell. To set it up yourself, for
the shell `$SHELL` names or the one you give:

```sh
ws completion install [bash|zsh|fish|powershell]
```

Or add the line for your shell to its profile by hand:

| Shell | Line | Where |
| --- | --- | --- |
| zsh | `source <(ws completion zsh)` | `~/.zshrc`, after `autoload -Uz compinit && compinit` |
| bash | `eval "$(ws completion bash)"` | `~/.bashrc` (needs the `bash-completion` package) |
| fish | `ws completion fish \| source` | `~/.config/fish/completions/ws.fish` |
| PowerShell | `ws completion powershell \| Out-String \| Invoke-Expression` | `$PROFILE` |

## Documentation

The [documentation index](docs/README.md) provides the complete reading order.
The principal documents are:

- [Architecture](docs/architecture.md)
- [CLI commands](docs/reference/commands.md)
- [Install](docs/guides/install.md)
- [Use the CLI](docs/guides/using-the-cli.md)
- [Build a module](docs/guides/build-module-quickstart.md)
- [Set up the example module](docs/guides/setup-example-module.md)
- [Context file reference](docs/reference/context-file.md)
- Setup guides: [Asgardeo](docs/guides/setup-asgardeo.md),
  [Identity Server 7.x](docs/guides/setup-identity-server-7.x.md),
  [ThunderID](docs/guides/setup-thunder.md)

Product requirements and architecture decisions are authoritative within their
respective scopes. Reference describes what is built; anything not built yet
says so.

## Project status

The shell, the module SDK, and the product modules are implemented and
released from this repository. Interfaces may still change: open decisions are
tracked as GitHub issues and recorded in
[decision records](docs/adr/) once settled.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for documentation standards and the
review process. Report security-sensitive concerns according to
[SECURITY.md](SECURITY.md).

## License

This repository is licensed under the [Apache License 2.0](LICENSE).
