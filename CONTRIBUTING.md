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
local development. Committed `replace` directives are prohibited; a test
enforces this.

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
