# Connect, the Command Surface and the Login-Product Pin: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `wso2 iam connect <url>`, `wso2 apim connect <url>`, `wso2 login`,
then any product command, with no identity flags, no scopes and no
audiences typed; a pipeline does the same with one machine secret; the
remaining section 7 items land; the login product is pinned; the live
matrix is run and recorded.

**Architecture:** A module declares a *product descriptor* inside its
manifest's `capabilities`, which the catalog, the release record and the
installed receipt already carry by value, so the descriptor reaches the
shell with no new plumbing. `connect` is intercepted in the shell's
namespace dispatch after the receipt is verified, and writes the product
record (and the identity, when the product is a login provider) through
`contexts.Update`. Everything else is a small change to code that exists.

**Tech Stack:** Go 1.24, cobra, the in-repo `internal/auth/fakeissuer`.

**Spec:** `docs/superpowers/specs/2026-09-06-one-login-many-sessions-design.md`
sections 6, 7 and 10. Previous plan:
`docs/superpowers/plans/2026-09-06-one-login-many-sessions.md`.

## Global Constraints

- Commit messages: one line, `type(scope): description`, no body, no
  attribution trailers. Hard-wrapped `.md`. Never name agent tooling in a
  committed file.
- No credential value reaches a document, a log line, a problem message
  or a module. Every broker refusal is a `problem.Problem` in the
  authentication class with a recovery.
- Problem codes added by this plan: `shell.connect_unsupported`,
  `shell.login_provider_required`. Everything else reuses existing codes.
- Tests: `go test ./internal/... ./cmd/...` stays green except the
  pre-existing environmental failure in `internal/boundaries`.
- Deviations from the handoff, written into the spec already: the
  descriptor lives *inside* `capabilities` rather than beside it (one
  carrier type through catalog, release and receipt); `connect` with no
  identity and a non-provider product refuses rather than prompting for
  an issuer it could build no identity from; bootstrap prints a `connect`
  line rather than returning a record.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/modules/receipt.go` (modify) | `ProductDescriptor` on `Capabilities`; validation. |
| `internal/modules/receipt_schema_test.go`, `internal/modules/fixture/fixture.go` (modify) | Descriptor tests and the fixture carrying one. |
| `modules/iam/module.json`, `modules/apim/module.json` (modify) | The descriptors. |
| `internal/contexts/identity.go`, `access.go` (modify) | `Identity.LoginProduct`; `LoginAccess` reads it. |
| `internal/contexts/access_test.go` (modify) | The drift case. |
| `internal/app/connect.go` (create) | The `connect` command: planning and writing. |
| `internal/app/connect_test.go` (create) | Every branch of section 6. |
| `internal/app/app.go` (modify) | Intercept `connect` in `dispatchNamespace`. |
| `internal/app/productargs.go`, `invoke.go` (modify) | Shell-owned `--no-input`; hook and module env. |
| `internal/rpc/launch.go` or `internal/modules/environment.go` (modify) | `WSO2_NO_INPUT` handed to the module. |
| `internal/auth/auth.go`, `source.go` (modify) | Empty request scopes mean the recorded scopes. |
| `internal/auth/source_product_test.go` (modify) | Test for the above. |
| `modules/apim/cmd/wso2-module-apim/gateway.go` (modify) | Drop `--scope`. |
| `internal/app/org.go`, `org_test.go` (modify) | Refuse `org use` on ThunderID and Identity Server. |
| `modules/iam/.../bootstrap.go`, `modules/apim/.../bootstrap.go` (modify) | Print the `connect` line. |
| `docs/reference/commands.md`, `docs/examples/authentication-contexts.md` (modify) | Documentation. |
| `docs/research/2026-09-07-one-login-live-matrix.md` (create) | The live runs. |

---

### Task 1: The product descriptor (`internal/modules`)

- [ ] `ProductDescriptor` struct on `Capabilities.Product` (`json:"product,omitempty"`)
  with `Provider`, `IssuerPath`, `ClientID`, `Audience` (`resource`|`client`),
  `DefaultAudience`, `Scopes`, `Grant`, `Machine`.
- [ ] `Receipt.Validate` refuses a descriptor whose audience kind is not one of
  the two, whose provider is not one the shell knows (`contexts.Providers()`
  cannot be imported without a cycle: keep a local list, tested equal), whose
  grant is not `federated`/`jwt-bearer`, or whose machine entries are not
  `inline`/`credential`. A receipt with no descriptor is unchanged.
- [ ] `fixture.Module.Product *modules.ProductDescriptor` carried into the receipt.
- [ ] `modules/iam/module.json`: provider `thunder`, client `wso2-cli`, audience
  `resource`, default `https://localhost:8090/mcp`, scopes `system`, machine
  `inline`. `modules/apim/module.json`: issuerPath `/oauth2/token`, audience
  `client`, scopes the six management scopes, grant `federated`, machine
  `credential`.
- [ ] Tests: a descriptor round-trips through the receipt; each refusal.
- [ ] Commit: `feat(modules): carry a product descriptor in the receipt capabilities`.

### Task 2: The login-product pin (`internal/contexts`)

- [ ] `Identity.LoginProduct string json:"loginProduct,omitempty"`; validation
  refuses a pin naming a product the identity does not record or one with
  a grant.
- [ ] `LoginAccess` uses the pinned product when set, else the sorted order.
- [ ] Test: recording an earlier-sorting product moves the login product
  without the pin and does not with it.
- [ ] Commit: `feat(contexts): pin the login product on the identity`.

### Task 3: `wso2 <namespace> connect <url>` (`internal/app`)

- [ ] `dispatchNamespace`: after `store.Resolve`, when `args[0] == "connect"`,
  run `s.connect(namespace, resolved.Receipt, args[1:])`.
- [ ] `connect.go`: a cobra command per invocation (`--identity`,
  `--login-provider`, `--client-id`, `--client-secret-variable`,
  `--client-id-variable`, `--audience`, `--scopes`, `--replace`, plus the
  shell's `--context` and `--output`), a planner that turns descriptor +
  flags + document into either a new identity or a product on an existing
  one, and a writer through `contexts.Update`. Refusals per spec section 6.
  Sets `LoginProduct` for the first direct product. Reports the record and
  a next line (`wso2 login` or the product's `status`).
- [ ] Tests (fake module store via `fixture.Install`, no network): provider
  product creates identity and context; second provider connect on the
  same issuer records on it; non-provider product attaches with the grant;
  no identity refuses `shell.login_provider_required`; `--login-provider`
  picks among identities; no descriptor refuses `shell.connect_unsupported`;
  machine identity via `--client-secret-variable`; apim on a machine
  identity refuses without the two variables and records the credential
  with them; `--replace`; `--scopes`/`--audience` overrides; the pin.
- [ ] Commit: `feat(app): add wso2 <namespace> connect, written from the module's product descriptor`.

### Task 4: Bootstraps print the connect line

- [ ] `iam bootstrap` prints `wso2 iam connect <issuer>`; `apim bootstrap`
  prints the two exports and `wso2 apim connect <base> --client-id-variable
  ... --client-secret-variable ...` for the machine path, and names the
  public federated client as the interactive path.
- [ ] Update the module tests. Commit: `feat(modules): bootstraps print the connect line to run next`.

### Task 5: Empty scope request means the recorded scopes (`internal/auth`)

- [ ] In `Broker.Acquire`, before `checkDeclared`: a request with no scopes
  takes `Products[ns].Scopes`. The record stays the ceiling.
- [ ] Test in `source_product_test.go`. Drop `--scope` from `apim gateway invoke`.
- [ ] Commit: `feat(auth): a module request with no scopes means the product's recorded scopes`.

### Task 6: `--no-input` reaches product commands

- [ ] `shellFlags` gains `noInput`; `productLine` carries it; `invokeModule`
  passes it to `sessionEstablisher`, which calls `nonInteractiveControl(noInput)`.
- [ ] The module is handed `WSO2_NO_INPUT=1` when either control asked:
  `rpc.Launcher` gains `NoInput bool`, appended to the sanitized environment.
- [ ] Tests: productargs reads the flag anywhere; establisher refuses with
  `--no-input` named. Commit: `feat(app): --no-input reaches product commands and their module`.

### Task 7: `org use` on a provider without organization switch

- [ ] Refuse with `auth.organization_switch_unsupported` when the selected
  identity's provider is `thunder` or `identity-server`, naming it, before
  the document is written. Test. Commit: `fix(org): refuse org use on a provider without organization switch`.

### Task 8: Documentation

- [ ] `docs/reference/commands.md`: the `connect` row, `--no-input` on
  product commands, `org use` refusal, empty scopes. `authentication-contexts.md`:
  `loginProduct`, the descriptor. Commit: `docs: describe connect, the product descriptor and the login-product pin`.

### Task 9: The live matrix

- [ ] From an empty home against `cli-thunder3` and `cli-apim`: rows 1, 2,
  the denied user, and the CI row. Row 3 (Identity Server) as far as the
  deployment allows. Record in `docs/research/2026-09-07-one-login-live-matrix.md`
  shaped like the spikes document. Commit: `docs(research): record the one-login live matrix`.

### Task 10: Review

- [ ] Run the code review over the branch since `6c4f7d3` and fix what it finds.
