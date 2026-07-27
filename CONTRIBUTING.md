# Contributing

Thank you for contributing to the WSO2 CLI.

## Scope

Contributions may improve the CLI implementation, public SDK, product
requirements, architecture decisions, examples, command conventions, tests, or
supporting public-source research.

## Building and testing

The repository contains three independently buildable Go modules: the shell at
the repository root, the public SDK in `sdk/`, and the reference module in
`examples/reference-module/`. `go.work` composes the unpublished modules for
local development. Committed `replace` directives are prohibited in every
`go.mod`; a test enforces this. `go.work` carries one replacement for the
unpublished SDK version the reference module requires. Replacing a module
version with contents found elsewhere is what a `go.work` replacement is for
([Go modules reference](https://go.dev/ref/mod#go-work-file-replace)); this
checkout needs one because the reference module requires an SDK version that
has never been published. It disappears once the SDK is published, and a test
pins it to that single line.

```shell
go build ./...                      # shell
go test ./...                       # shell, including acceptance tests
(cd sdk && GOWORK=off go test ./...)  # SDK without workspace composition
```

The shell, protocol, SDK, and module versions move independently and are
injected at build time:

```shell
go build -ldflags "\
  -X github.com/wso2/wso2-cli/internal/version.shellVersion=0.1.0 \
  -X github.com/wso2/wso2-cli/internal/version.protocolVersion=1" ./cmd/wso2

cd examples/reference-module && go build -ldflags "\
  -X main.moduleVersion=0.1.0 \
  -X github.com/wso2/wso2-cli/sdk/module.SDKVersion=0.1.0" ./cmd/wso2-module-reference
```

Every Go file begins with the Apache-2.0 license header, followed by a blank
line so the header does not become package documentation. A test in
`internal/boundaries` enforces both.

### The module contract schema

The shell and every module exchange Protobuf messages defined in
`sdk/proto/wso2/cli/module/v1/contract.proto`. The generated Go types in
`sdk/protocol/contractv1` are committed, so building and testing the repository
needs neither a Protobuf toolchain nor network access.

After editing any `.proto` file, regenerate and commit the result:

```shell
./scripts/generate-protobuf.sh
```

The script fetches a pinned `buf` and a remote code-generation plugin, so it
needs network access while it runs. It also applies the license header, which
code generators do not emit.

Tests never read or write real WSO2 user state. The shell resolves all local
state below one root, overridden with `WSO2_HOME`, and the test-only fixture
installer refuses to write into `~/.wso2`.

## Documentation standards

Contributions must:

- use formal, concise, and inclusive language;
- distinguish accepted decisions from proposals and open questions;
- preserve the authority hierarchy described in
  [docs/README.md](docs/README.md);
- cite public primary sources for externally verifiable technical claims;
- avoid secrets, customer information, personal data, private repository
  references, internal infrastructure details, and non-public business
  information;
- use reserved example domains such as `example.com` and `example.invalid`;
- keep commands and configuration samples non-destructive and free of
  credential values; and
- update affected links and indexes when documents are added, moved, or
  renamed.

## Proposing changes

1. Open an issue or discussion for a material requirement or architecture
   change.
2. Update the authoritative document and any affected reference or example
   files.
3. Add or revise an architecture decision record when the change establishes a
   durable project-wide constraint.
4. Verify Markdown formatting, relative links, examples, and terminology.
5. Submit a focused pull request that explains the decision and its impact.

Research documents may compare alternatives, but final requirements belong in
`docs/product-requirements.md` and final architecture decisions belong in
`docs/architecture.md` or an accepted decision record.

## Security-sensitive contributions

Do not open a public issue or pull request containing a suspected credential,
private infrastructure detail, or unpublished vulnerability. Follow
[SECURITY.md](SECURITY.md) instead.
