# Documentation

## Start here

- [Product requirements](product-requirements.md): goals, non-goals,
  requirements, and success criteria.
- [Architecture](architecture.md): how the shell, modules, SDK, catalog, and
  authentication fit together.
- [Decision records](adr/): why things are the way they are.
- [Domain glossary](../CONTEXT.md): the words the project uses.

## Guides

Short task documents.

- [Install](guides/install.md) the CLI.
- Set up a login provider:
  [Asgardeo](guides/setup-asgardeo.md),
  [Identity Server 7.x](guides/setup-identity-server-7.x.md), or
  [ThunderID](guides/setup-thunder.md).
- [Build a module](guides/build-module-quickstart.md): create, install, run,
  and release a product module.
- [Troubleshoot a module](guides/troubleshoot-module.md).

## Reference

What is built.

- [Commands](reference/commands.md): shell commands, exit classes, and
  non-interactive use.
- [Context file](reference/context-file.md): the context document, input
  files, and examples.
- [Module manifest](reference/module-manifest.md): `module.json`.
- [Module SDK](reference/module-sdk.md): the Go API a module is written
  against.
- [Module catalog](reference/module-catalog.md): tags, catalog files,
  install, and update.
- [Release artifacts](reference/release-artifacts.md): archives, checksums,
  and the release gate.

## For contributors and agents

- [Agent instructions](agents/): issue tracker, triage labels, and domain
  docs layout.
- Open work and research live in
  [GitHub issues](https://github.com/wso2/wso2-cli/issues), not in this
  directory. Decisions go in an ADR
  ([ADR 0001](adr/0001-public-documentation-structure.md)).
