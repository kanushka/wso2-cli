# Documentation

**Status:** Project documentation
**Last reviewed:** 2026-07-27

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

Plans are bounded execution documents. They do not override product
requirements or architecture.

## Reference and examples

- [Proposed shell commands](reference/commands.md)
- [Authentication context examples](examples/authentication-contexts.md)

These documents illustrate proposed interfaces. They are not evidence that the
described commands or schemas are currently available.

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
