# ADR 0001: Public Documentation Structure

**Status:** Accepted  
**Date:** 2026-07-27

## Context

This repository is the project home for the WSO2 CLI. Its initial product
requirements, architecture decisions, command examples, implementation
planning, and supporting research were developed as Markdown files at the
repository root and in `examples/` and `research/`.

Before public publication, the repository requires:

- a clear distinction between authoritative documents, examples, current
  research, and historical material;
- removal of references derived from private repositories;
- formal and consistent language;
- an explicit statement that the project is in early development and that
  documentation may precede implementation;
- standard repository entry, licensing, contribution, and security files; and
- a clean Git history containing only publication-ready material.

## Decision

Repository entry and governance files remain at the root:

```text
README.md
LICENSE
NOTICE
CONTRIBUTING.md
SECURITY.md
```

Project documentation is maintained under `docs/`:

```text
docs/
├── README.md
├── product-requirements.md
├── architecture.md
├── adr/
│   └── 0001-public-documentation-structure.md
├── examples/
│   └── authentication-contexts.md
├── reference/
│   └── commands.md
└── research/
    ├── README.md
    ├── cloud-cli-comparison.md
    ├── kubectl-krew.md
    ├── module-architecture-options.md
    ├── public-wso2-cli-inventory.md
    ├── root-cli-installation-distribution.md
    └── archive/
        └── original-proposal.md
```

The authority hierarchy is:

1. `docs/product-requirements.md` defines product intent and requirements.
2. `docs/architecture.md` defines selected architectural decisions.
3. Reference and example documents illustrate proposed interfaces.
4. Current research records evidence and alternatives.
5. Archived material preserves historical context and is explicitly
   non-authoritative.

The WSO2 CLI inventory retains only publicly verifiable repository and product
information. Private repository names, links, layouts, implementation details,
and cross-repository relationships are excluded.

## Editorial Requirements

Every document must:

- identify its status and role;
- use formal, concise language;
- distinguish decisions from proposals and unresolved questions;
- link to the authoritative document when presenting derived information;
- use consistent terminology for the shell, protocol, SDK, modules, and product
  namespaces; and
- avoid confidential, personal, customer, credential, and internal
  infrastructure information.

The root README must identify the repository as the long-lived project home and
state that documentation may describe intended behavior before the
corresponding implementation is available.

## Publication Validation

Before the repository is published:

- Markdown links and local references must resolve;
- the documentation must contain no secret material or private-repository
  references;
- current and historical documents must be clearly separated;
- conflicting command and update terminology must be reconciled;
- offline installation guidance must describe a pre-execution trust mechanism;
- Markdown must contain no unintended control characters; and
- Git must contain only the intended publication-ready files.

## Git History

After the documentation rewrite is validated, the existing local Git history
will be replaced with a clean publication history. The clean history will
contain logically scoped commits for the public documentation baseline and
subsequent validation corrections, if any.

Remote configuration and publication are separate actions. Replacing local
history does not authorize a force-push or any other remote mutation.

## Excluded Concurrent Work

The root `roadmap.md` and `research/roadmap.md` files are owned by a separate,
ongoing task. This documentation migration does not edit, move, format, stage,
or commit either file.

The final clean-history replacement must wait until the roadmap task is
complete. Rewriting shared Git history while that work is active could disrupt
the other task or make its changes difficult to reconcile.

## Consequences

This structure creates a stable location for future specifications and
architecture decisions while keeping GitHub-recognized repository files
discoverable at the root. Existing Markdown paths will change, so all relative
links must be updated as part of the migration.
