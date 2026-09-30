---
name: new-product-module
description: Use when planning or building a WSO2 CLI product module, adding a product namespace, scaffolding a module, writing its spec or tickets, or extending its commands.
---

# New product module

Before starting, read the documents required by [domain guidance](../../../docs/agents/domain.md), including ADRs 0002, 0003, 0004, 0013 and 0015. Use the glossary's vocabulary.

## Pick the branch

- Plan: no approved spec yet. Follow steps 1–3.
- Build: a `ready-for-agent` issue describes the module. Follow steps 4–6.
- Extend: add commands to an existing module. Follow steps 5–6.

The planning and implementation workflow skills are user-invoked. Recommend the relevant skill; when the user authorizes proceeding inline, follow its template under `.agents/skills/` and name the workflow you performed.

## 1. Settle the design

Recommend `/grill-with-docs`. Read [command design](../../../docs/guides/command-design.md) before defining the tree. Fill the [module sheet](PLANNING.md#module-sheet); record unresolved decisions as questions. Done when every row has a value and assumptions are explicit.

## 2. Spec

Recommend `/to-spec` using the [spec shape](PLANNING.md#spec-shape). Done when the spec issue carries the whole module sheet and is labelled `ready-for-agent`.

## 3. Tickets

Recommend `/to-tickets` using [ticket slicing](PLANNING.md#ticket-slicing). Done when every ticket is a sub-issue of the spec with blockers linked.

## 4. Scaffold

Follow [Generate it](../../../docs/guides/build-module-quickstart.md#1-generate-it), including its untouched-scaffold test checkpoint. Then apply the approved module sheet to the manifest and SDK options.

## 5. Commands, test-first

Before implementing, read [command design](../../../docs/guides/command-design.md), [Add a command](../../../docs/guides/build-module-quickstart.md#2-add-a-command), [Call your product](../../../docs/guides/build-module-quickstart.md#3-call-your-product), [code conventions](CONVENTIONS.md), and the [SDK reference](../../../docs/reference/module-sdk.md). For extensions, inspect existing decisions and update both manifest and SDK access declarations when requirements change.

Write a failing `testkit.Run` test against an `httptest` product stub, then the handler. Done when every command proves its schema and fields including `next`, each refusal category, and, for protected API calls, the brokered Bearer token received by the stub.

## 6. Prove it under the real shell

Always `export WSO2_HOME=$(mktemp -d)` before installation. Follow [Run it in a real shell](../../../docs/guides/build-module-quickstart.md#4-run-it-in-a-real-shell) and [Test it](../../../docs/guides/build-module-quickstart.md#5-test-it); `testkit` cannot prove shell capability checks. Reinstall after code or declaration changes.

If installation, launch or access fails, consult [troubleshooting](../../../docs/guides/troubleshoot-module.md) before changing declarations.

Done when the [review checklist](../../../docs/guides/build-module-quickstart.md#before-you-open-the-pull-request) holds and every new command has run through `./bin/ws`. Without a live product, check help and applicable missing-context and missing-argument refusals. Report which commands reached the product and which stopped at a refusal.

[Release](../../../docs/guides/build-module-quickstart.md#6-release-it) is a separate ticket; a human pushes the tag.
