# Documentation

**Status:** Project documentation
**Last reviewed:** 2026-08-05

This directory contains the product, architecture, reference, example, and
research documentation for the WSO2 CLI.

## Authoritative documents

Read these documents first:

1. [Product requirements](product-requirements.md) define product intent,
   priorities, non-goals, and success criteria.
2. [Architecture](architecture.md) defines the selected system structure,
   trust boundaries, module model, and runtime behavior.
3. [Architecture decisions](adr/) record durable repository-wide
   decisions and their consequences.
4. [Domain glossary](../CONTEXT.md) defines the preferred language and
   conceptual boundaries.

## Implementation plans

- [First CLI vertical slice](plans/first-cli-vertical-slice.md) defines the
  approved architecture proof, its exclusions, implementation order, and
  acceptance gate.
- [Architecture proof review](plans/architecture-proof-review.md) closes that
  plan: how to reproduce the gate, the seams a production implementation
  replaces, and what the proof does not establish.
- [`wso2 login` first slice](plans/login-first-slice.md) defines the
  authentication slice: browser PKCE login, schema version 2 identities,
  keychain sessions, and the broker's token-source seam.

Plans are bounded execution documents. They do not override product
requirements or architecture.

## Reference and examples

- [Proposed shell commands](reference/commands.md)
- [Release artifacts](reference/release-artifacts.md) is the naming, checksum,
  and version contract between a published release and the programs that
  download from it. It describes what a release actually publishes, not a
  proposed interface.
- [Module catalog](reference/module-catalog.md) is the contract between the
  tags a product module is released under and the two generated files a shell
  reads to discover, select, and verify a module version. It too describes what
  is generated rather than a proposed interface.
- [Authentication context examples](examples/authentication-contexts.md)

These documents illustrate proposed interfaces. They are not evidence that the
described commands or schemas are currently available.

## Guides

- [Installing](guides/installing.md) takes a first-time user from a bare machine
  to a working `wso2`, by one command or by hand from the release page, and
  covers pinning a version, release candidates, where files go, and uninstalling.
- [Logging in](guides/login.md) takes a first-time user from a registered
  OAuth application, through the first `wso2 login`, which creates the identity
  and context it authenticates, to a CI job that authenticates without one. It
  also describes the context document itself, for reading what a login wrote or
  writing one by hand. Everything in it is the same whichever product backs the
  deployment.
- Registering the application is product-specific, and each product has its own
  walkthrough: [Asgardeo](guides/login-asgardeo.md),
  [Identity Server 7.x](guides/login-identity-server.md), and
  [ThunderID](guides/login-thunder.md). They are alternatives; a reader needs
  exactly one, and each hands off to the login guide at its section 2.
- [Building a product module](guides/building-product-modules.md) shows a
  product team how to add an independently released module, use the SDK and
  authentication broker, test it, and release it through the generated catalog.
  Its reader is a product team, not a first-time user.

## Research

The [research index](research/README.md) identifies the public sources and
technical comparisons that informed the authoritative documents. Research may
preserve alternatives or earlier recommendations. When research differs from
the product requirements or architecture, the authoritative document controls.

## Document status

Each document identifies its status near the title:

- **Working draft:** under active review and not a frozen public contract.
- **Proposed reference:** illustrative interface subject to change.
- **Research:** evidence or analysis that informs, but does not establish,
  requirements.
- **Archived:** retained for historical context and explicitly
  non-authoritative.
- **Accepted:** an approved decision that remains in force until superseded.
