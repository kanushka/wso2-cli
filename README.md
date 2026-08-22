# WSO2 CLI

The WSO2 CLI provides a common command-line entry point for WSO2 products. This
repository is the project home for its source code, SDK, documentation,
examples, and supporting research.

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

The [installation guide](docs/guides/installing.md) covers installing from the
release page without running a remote script, pinning a version, release
candidates, where files go, and how to uninstall.

## Documentation

The [documentation index](docs/README.md) provides the complete reading order.
The principal documents are:

- [Product requirements](docs/product-requirements.md)
- [Architecture](docs/architecture.md)
- [wso2 cli commands](docs/reference/commands.md)
- [Building a product module](docs/guides/building-product-modules.md)
- [Authentication context examples](docs/examples/authentication-contexts.md)

Product requirements and architecture decisions are authoritative within their
respective scopes. Reference material and examples illustrate proposed
interfaces. Research records evidence and alternatives but does not override
the requirements or architecture.

## Project status

The initial implementation is being prepared. Public namespaces, protocol
details, release infrastructure, supported platforms, and migration scope
remain subject to review where identified as open decisions.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for documentation standards and the
review process. Report security-sensitive concerns according to
[SECURITY.md](SECURITY.md).

## License

This repository is licensed under the [Apache License 2.0](LICENSE).
