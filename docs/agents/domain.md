# Domain Docs

How the engineering skills should consume this repository's product, architecture, and domain documentation before exploring the codebase.

## Before exploring, read these

- **`docs/product-requirements.md`** — authoritative product intent, scope, goals, and requirements.
- **`docs/architecture.md`** — authoritative system design, component boundaries, and architectural constraints.
- **`CONTEXT.md`** — domain glossary, conceptual model, and preferred vocabulary, when present.
- **`docs/adr/`** — accepted decisions and their rationale. Read ADRs relevant to the area being changed.

If `CONTEXT.md` or an applicable ADR does not exist, proceed silently. Do not suggest creating one upfront. The domain-modeling skill creates and updates domain documentation when terminology or decisions are resolved.

## File structure

This is a single-context repository:

```text
/
├── CONTEXT.md
└── docs/
    ├── product-requirements.md
    ├── architecture.md
    └── adr/
        └── 0001-public-documentation-structure.md
```

## Document authority

1. `docs/product-requirements.md` defines product intent and requirements.
2. `docs/architecture.md` defines the selected system design.
3. `docs/adr/` records durable decisions and their rationale.
4. `CONTEXT.md` defines shared domain terminology and conceptual boundaries.

When documents conflict, surface the conflict rather than silently choosing one.

## Use the glossary's vocabulary

When output names a domain concept—in an issue title, refactor proposal, hypothesis, or test name—use the term defined in `CONTEXT.md`. Do not drift to synonyms the glossary explicitly avoids.

If a needed concept is absent, reconsider whether the project uses that language or note the genuine gap for domain modeling.

## Flag ADR conflicts

If output contradicts an existing ADR, surface it explicitly rather than silently overriding it:

> _Contradicts ADR-0001 (public documentation structure)—but worth reopening because…_
