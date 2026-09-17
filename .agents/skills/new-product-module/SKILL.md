---
name: new-product-module
description: Plan and build a new WSO2 CLI product module (a `wso2 <namespace>` command tree on the Go SDK and Cobra). Use when adding a product namespace, scaffolding under `modules/`, writing a spec or tickets for one, or adding commands to an existing product module.
---

# New product module

A **product module** is a separately released executable that owns one **product namespace** (`CONTEXT.md`). The shell owns login, tokens, rendering and exit codes; the module owns a Cobra command tree whose handlers return results. `modules/identity` and `modules/api` are the prior art; copy their shape.

Before any step, read what `docs/agents/domain.md` lists, plus ADRs 0002, 0003, 0004, 0013 and 0015. Use glossary words in every issue, identifier and help line: _product module_, _product namespace_, _context_, _module contract_, never _plugin_ or _account_.

## Pick the branch

- **Plan**: the module has no approved spec yet → steps 1–3.
- **Build**: an issue labelled `ready-for-agent` describes it → steps 4–6.
- **Extend**: the module exists and needs a command → steps 5–6 only.

## 1. Settle the design

The workflow skills here (`/grill-with-docs`, `/to-spec`, `/to-tickets`, `/triage`, `/implement`) are user-invoked: you cannot fire them. Tell the user which to run and when. When the user is unavailable or tells you to get on with it, do the same work inline, following the templates in the skills' own `SKILL.md` files under `.agents/skills/` plus the shapes in [PLANNING.md](PLANNING.md), and say which skill's work you stood in for.

Fill every row of the **module sheet** in [PLANNING.md](PLANNING.md). A row you cannot fill from the conversation or the code is an open question, never a quiet guess: the authentication rows in particular build and test clean and fail the first user. Done when every row has a value, and every guess is named as one.

## 2. Spec

Ask the user to run `/to-spec`. The spec is one issue titled `spec: <namespace> product module for <product>`, and its Implementation Decisions carry the whole module sheet. Done when the issue exists with `ready-for-agent`.

## 3. Tickets

Ask the user to run `/to-tickets` on the spec issue. Slice the module as [PLANNING.md](PLANNING.md) describes, so the first ticket is a tracer bullet that reaches the real shell. Done when every ticket is published as a sub-issue of the spec, with its blockers linked.

## 4. Scaffold

Follow `docs/guides/build-module-quickstart.md` step 1. Always use `make new-module`; never copy another module's directory, because the generator stamps SDK and protocol versions from the checkout. Then fill in `module.json` and `moduleOptions()` from the module sheet. Done when `make test-module NAMESPACE=<ns>` passes on the untouched scaffold.

## 5. Commands, test-first

For each command in the ticket, write a failing `testkit.Run` test against an `httptest` stub of the product API, then the handler. Follow [CONVENTIONS.md](CONVENTIONS.md) for layout, results, problems and the API client. The SDK surface itself is in `docs/reference/module-sdk.md`; read it rather than guessing a signature.

Done when every command in the ticket has a test that proves:

- the result schema and fields, including `next`,
- the brokered token reached the stub (`Bearer <token>`),
- each refusal path maps to the category in the sheet.

## 6. Prove it under the real shell

`testkit` never checks declared audiences and scopes, so green tests do not prove the module runs. Follow the guide's steps 3–5: `make install-module`, run every new command through `./bin/ws <ns> ...`, then run `./scripts/acceptance.sh`.

Always `export WSO2_HOME=$(mktemp -d)` first. The guide calls this optional because a person installing their own module wants it to stick; you are not that person. Without it you install into the developer's real module store and a second agent working at the same time corrupts it.

A command that needs a live product is proven as far as it goes without one: its help under `./bin/ws`, and the refusals it gives with no context and no argument. Say in your report which commands reached the product and which stopped at a refusal, rather than implying a full round trip.

Done when every item in the guide's "Before review" list holds and each new command has run under `./bin/ws`. Then commit (one-line Conventional Commit, scope = namespace, e.g. `feat(abc): add projects list`) and reference the ticket in the PR.

Releasing (`make gate-module`, tag `<ns>/vX.Y.Z-rc.N`) is its own ticket and needs a human to push the tag.
