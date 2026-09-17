# Planning a product module

What a module spec and its tickets must carry beyond the generic `/to-spec` and `/to-tickets` templates. Issue mechanics (`gh`, labels, sub-issues) live in `docs/agents/issue-tracker.md` and `docs/agents/triage-labels.md`.

## Module sheet

Every row gets a value before the spec is written. Each one is costly to change after release.

The third column is for whoever builds the module. Keep it out of the issues: a decision belongs in a spec as prose ("every command requests the `<ns>-management` audience, declared to both the manifest and the SDK options"), because a path or a snippet in an issue goes stale the first time the tree moves.

| Row | What to decide | Where it lands |
| --- | --- | --- |
| Product | The WSO2 product and which deployments it serves (ThunderID, Identity Server, Asgardeo, APIM…) | Spec problem statement |
| Namespace | Lowercase letters and digits, named after the **product**, not a deployment (ADR 0015). Renaming later is a migration. Not a shell command, not `reference`, not already taken. ADR 0015 and `docs/product-requirements.md` enumerate the sanctioned namespaces: a namespace absent from that list is an ADR conflict to surface as its own prerequisite, never something to assume into the spec | `Namespace`, `module.json`, program path `wso2-module-<ns>`, tag prefix |
| Title | Human name shown in `wso2 product` listings | `module.json` `title` |
| Command tree | Every `wso2 <ns> <group> <verb>` with args and flags. Nouns plural (`users`, `resource-servers`), verbs `list`/`create`/`delete`/…, and `status` always present | Cobra tree in `commands()` |
| Product API | Base URL source (always `request.Context.Endpoint`), paths each command calls, auth header, pagination | `internal/<product>/client.go` |
| Audiences | Logical names such as `<ns>-management`, never a deployment URL; the user maps them in the context | `module.json` `capabilities.authAudiences` and `module.Options.AuthAudiences` |
| Scopes | Per audience, the minimum each command needs | `authScopes` and `AuthScopes` |
| Product descriptor | Whether `capabilities.product` is declared (provider, clientId, audience binding, scopes, machine strategies), or deliberately absent. See `docs/reference/module-manifest.md` | `module.json` |
| Result schemas | One `<ns>.<noun>/v1` per result shape, fields in display order | Schema constants |
| Failure map | For each refusal: code `<ns>.<snake_case>`, category (usage / product_service / auth_policy) and recovery line | `access.go` helpers |
| Test seam | `testkit.Run` against an `httptest` stub of the product API: the one seam, reused by every ticket | Spec Testing Decisions |
| Out of scope | Commands deferred, deployments not served | Spec Out of Scope |

## Spec shape

- Title: `spec: <namespace> product module for <product>`.
- Problem Statement names the journey users do today without the module (curl, console, product UI, hand-written scripts), then shows the target journey as one annotated `wso2 <ns> ...` shell block a reader can follow top to bottom. Issue #166 is the worked example if you can reach the tracker.
- User stories cover every command in the tree, the first-run `status` path, and each refusal a user can hit (no product recorded on the context, access denied, deployment unreachable, bad input).
- Implementation Decisions carry the module sheet as prose. No file paths.

## Ticket slicing

Tickets are titled `feat(<ns>): <what a user can now do>`.

1. **Tracer bullet** (unblocked): scaffold, `status`, and one read-only `list` command calling the product API with brokered access, installed and run under `./bin/ws`. This proves namespace, manifest, audience and scope declaration, client and test seam together.
2. **One ticket per command group** (blocked by 1): each adds its `list`/`create`/… verbs with their tests. Groups that don't share code run in parallel.
3. **Cross-cutting behaviour** such as pagination or provider checks: its own ticket if more than one group needs it; it blocks those groups.
4. **Release** (blocked by all): `make gate-module`, then a prerelease tag. Label `ready-for-human`, because pushing a tag publishes.
