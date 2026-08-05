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
- [Authentication context examples](examples/authentication-contexts.md)

These documents illustrate proposed interfaces. They are not evidence that the
described commands or schemas are currently available.

## Guides

- [Logging in](guides/login.md) takes a first-time user from registering the
  OAuth application in Asgardeo or Identity Server 7.x, through authoring the
  context document and the first `wso2 login`, to a CI job that authenticates
  without one. It is written to be read on its own.

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
