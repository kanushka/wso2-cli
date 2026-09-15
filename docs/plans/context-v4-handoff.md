# Handoff: context file v4 and file-first setup

Status: direction agreed 2026-09-14, details revised the same day after review.
Implemented 2026-09-15 on `feat/context-v4` (all four parts on one branch, not
the four PRs below); see *Implementation notes* at the end. Discovery is a
separate handoff (`docs/plans/deployment-discovery-handoff.md`) and is out of
scope here.

## Problem

On-premises setup today (from `cli-exercise-6/secure-api-demo.ipynb`):

```
wso2 identity connect http://localhost:8501 --account demo
wso2 api connect http://localhost:9251 --account demo
wso2 api connect http://localhost:9091 --gateway --account demo
wso2 login
```

- `connect` sits under each product namespace, so `wso2 --help` never lists
  it.
- Three command families write `contexts.json`: `account`, `context`, and
  `<ns> connect`.
- Users find "account" confusing. The file holds `accounts[]` and
  `contexts[]`, and each context is just a pointer into the account list
  (`{"name":"demo","account":"demo"}`).
- Ten developers type the same four commands ten times, and their setups
  drift apart.

## Goal

Cloud users don't need to edit the context file. `context show` and
`context export` stay available to them for troubleshooting. For on-premises
users, the file is the setup: a platform team writes it once, and each
developer runs two commands.

## Decisions

1. **One list (schema v4).** `accounts[]` goes away. A context owns its
   authentication, its products, its sessions, and optionally an organization
   and project. The word "account" disappears from the file and the commands.
   The authentication model stays as explicit as it is today (see
   *Authentication model*).
2. **Two files with two roles.** The *input file* is short and shareable, and
   `apply` reads it. The *local document* (`contexts.json`) is complete and
   resolved. It is the only thing the shell reads at command time. Nothing is
   derived when a context is resolved (see *Frozen defaults*).
3. **Each context owns its sessions.** No two contexts share a
   `credentialRef` (see *Session ownership*).
4. **Old commands are removed but still recognized.** `wso2 account …` and
   `wso2 <ns> connect` stop working, but typing them prints the exact
   replacement command instead of "unknown command" (see *Rollout*).
5. **Keep `wso2 product install|remove|update|list`.** ADR 0015 decided that
   users install a product and contributors build a module. You install the
   `api` product, then tell a context where `api` runs. Both use the word
   "product" because it is the same thing.
6. **Installation is a visible setup step.** `apply` and `product add` report
   which products they need, install missing ones only after validating the
   whole input, and never upgrade a product that is already installed unless
   asked (see *Apply*).

## Frozen defaults

A later product update must not change authentication behavior when the
context file hasn't changed. So:

- An input file may omit issuer, audience, scopes, grant, and gateway
  audience/scopes.
- `apply` and `context product add` fill in the omitted values from the
  **installed** product's descriptor, then write **complete** records to
  `contexts.json`.
- At command time the shell reads only `contexts.json`. It never consults a
  descriptor to fill a gap. An incomplete record is refused as malformed.
- `wso2 product update` never edits contexts. After an update, `wso2 doctor`
  and `wso2 context show` flag any record whose values differ from what the
  new descriptor would produce, and name the command that adopts the new
  values.
- To adopt new defaults, re-run `apply` (or `context product add --replace`).
  `--dry-run` shows the field-by-field change first.
- Migration freezes the defaults too. An account with no `loginProduct` gets
  its effective one (today: the first direct product by namespace) written
  out explicitly.

The input file is for convenience. `contexts.json` is the reproducible local
configuration.

## Authentication model

`account` goes away, but the authentication settings stay. Each context has
one `login` block:

```json
{
  "schemaVersion": 4,
  "defaultContext": "local",
  "contexts": [
    {
      "name": "local",
      "type": "onprem",
      "credentialRef": "local",
      "login": {
        "kind": "oauth-browser",
        "issuer": "http://localhost:8501",
        "clientId": "wso2-cli",
        "provider": "thunder",
        "product": "identity"
      },
      "products": {
        "identity": { "url": "http://localhost:8501", "audience": "https://localhost:8090/mcp", "scopes": ["system"] },
        "api": {
          "url": "http://localhost:9251", "audience": "http://localhost:9251",
          "grant": { "kind": "exchange" },
          "gateway": { "url": "http://localhost:9091", "audience": "http://localhost:9091" }
        }
      }
    }
  ]
}
```

This is the complete local form. The input form for the same context can
leave out everything the descriptor resolves:

```json
{
  "contexts": [
    {
      "name": "local",
      "login": { "product": "identity", "clientId": "wso2-cli" },
      "products": {
        "identity": { "url": "http://localhost:8501" },
        "api": { "url": "http://localhost:9251", "gateway": { "url": "http://localhost:9091" } }
      }
    }
  ]
}
```

A product's address is `url` in the file and `--url` on the command line,
the same word in both, because people type both by hand. v3's `endpoint` is
renamed during migration. Go field names (`Product.Endpoint`,
`Gateway.Endpoint`) can stay as they are. Only the JSON tag and the flags
change.

### Where each v3 field goes

| v3 | v4 |
|---|---|
| `defaultContext` | `defaultContext` (unchanged, to avoid needless churn) |
| `contexts[].name / organization / project` | same fields on the context |
| `contexts[].account` | gone; the account is inlined |
| `accounts[].type` | `contexts[].type` |
| `accounts[].auth.kind / issuer / clientId / tenant / provider / narrowing / clientSecretVariable` | `contexts[].login.*` |
| `accounts[].auth.credentialRef` | `contexts[].credentialRef`, at the top level because it owns every session the context holds, not only the login one |
| `accounts[].loginProduct` | `contexts[].login.product`, always written (frozen) |
| `accounts[].products` (endpoint, audience, scopes, grant, `clientIdVariable`, `clientSecretVariable`, gateway) | `contexts[].products`, same shape except `endpoint` is renamed `url` |
| `products.<ns>.gateway.endpoint` | `products.<ns>.gateway.url` |
| v1 `development-credential` synthetic identities | stays read-only, as today: readable, never written back |

### How the authentication mode is chosen

`login.kind` is one of `oauth-browser`, `oauth-device`, `client-credentials`,
or `pat`, the same legal set as today. `context create` sets it from its flags:
browser by default, `--device`, or `--client-secret-variable` for
client-credentials. `wso2 login` behaves as it does now. This work does not
reconcile the stored kind with `CONTEXT.md`'s **Login mode**, which describes
the mode as a property of the machine rather than of the configuration. That
stays a separate question.

### A login provider without its CLI

`login.product` is optional. Without it, the context logs in through
`login.issuer` and `login.clientId` directly as a generic OIDC provider, and
nothing needs to be installed for the login provider. This is the existing
bare-session behavior of `LoginAccess`, and `wso2 login --url … --client-id …`
keeps creating a context this way.

### `context create` never guesses

A URL alone doesn't identify which descriptor applies, so `create` takes
exactly one of two forms and refuses a `--url` given without `--login-product`:

```
wso2 context create local --login-product identity --url http://localhost:8501 [--client-id …] [--provider …]
wso2 context create local --issuer http://localhost:8501 --client-id wso2-cli [--provider thunder]
```

The first form resolves through the named product's descriptor, installing
the product if needed (see *Apply*). If the descriptor can't tell the provider
(the identity product serves both Thunder and IS), `create` refuses and asks
for `--provider`. The second form needs no product installed.

## Session ownership

Today a login session lives under `credentialRef`, a product session under
`credentialRef.<ns>`, and a gateway session under `credentialRef.<ns>/gateway`
(`contexts.ProductSessionRef`, `GatewayKey`). Once accounts are inlined, two
contexts that keep a shared ref could change their `api` URL or grant
independently, and each would then present the other's sessions. Comparing
only the login blocks doesn't prevent that.

**Rule: each `credentialRef` belongs to exactly one context.** Validation
refuses a document where two contexts declare the same ref. An input file may
not declare `credentialRef` at all, so `apply` keeps the existing ref for a
replaced context and assigns one to a new context. `export` removes it.

What follows from that rule:

- **Migrating a shared account.** The v3 account's ref, and every keychain
  entry under it, goes to one context: the default context if it is among
  those sharing the account, otherwise the first by name. The others get new
  refs and have to log in again. Sessions are **never copied** to a second
  ref. If an issuer rotates refresh tokens with reuse detection, two copies
  of one refresh token revoke each other. Migration prints which contexts
  need a new login. Migration does not promise that nobody will have to log
  in again.
- **When a stored session may be reused.** Only when its recorded binding
  still matches the context's record. Today a stored session records its
  issuer, client, scopes, and strategy, and `sessionSource.renew` checks
  those. It **does not record audience or resource**. PR 1 adds both. An
  entry written before that change is reused only for a record that asks for
  no resource indicator, and otherwise needs a new login.
- **When a context's own record changes** (`product remove`, `apply`
  replacing a context, `product add --replace`): every session whose binding
  no longer matches is ended, with best-effort revocation per ADR 0010, then
  deleted. This extends `SessionsUnreachedBy` from "no longer reached" to
  "reached, but bound differently". `--dry-run` lists these sessions before
  anything is ended.
- **Logout** ends only the selected context's sessions, because nothing else
  holds them. The "shared by contexts" report in `logout.go` goes away. One
  side effect is outside the shell's control. By default logout also ends the
  identity provider's browser sign-on (`--keep-browser-session` leaves it
  alone), and some providers revoke other refresh tokens for that user when
  it ends. Logout says so. It doesn't claim to know.
- **Rename** changes only `name`. `credentialRef` is a stable id that
  defaults to the name when the context is created and never follows a
  rename, so no keychain entry moves.
- **Delete** ends and removes every session under the context's ref (login,
  products, gateways) **before** it writes the document. If the write then
  fails, the context is still there without a session: harmless, and fixed by
  logging in. The reverse order would leave secrets in the keychain that no
  document names.

## Apply

```
wso2 context apply -f team-context.json [--use <name>] [--dry-run] [--no-install] [--update-products]
```

**Matching:** each supplied context is matched by name and replaces the
existing context of that name **completely**. Contexts not in the input are
kept. There is no whole-document replace mode.

**Selection:** the input file format has no `defaultContext`, and `apply`
refuses a file that includes one. The selection changes only with
`--use <name>`, which must name a context in the input. If nothing is
selected and `--use` isn't given, `apply` selects nothing and prints the
`wso2 context use` command as the next step. A team file never changes an
existing user's active environment.

Two-command onboarding:

```
wso2 context apply -f team-context.json --use local
wso2 login
```

**Order of operations.** Nothing is written until everything before step 5
has succeeded.

1. **Validate the whole input**: schema, names, URLs, references between
   entries, and no `credentialRef` or `defaultContext` keys. No side effects.
2. **Plan**: which products are needed and missing, which are installed at a
   different version than the input pins, and for each context whether it
   will be created, replaced (with a field diff), or left unchanged, plus
   which sessions will end.
3. **`--dry-run` stops here.** It prints the plan, downloads nothing, and
   writes nothing. Defaults for products that aren't installed yet are shown
   as "resolved after install".
4. **Install the missing products.** A product entry may pin a version
   (`"version": "1.4.2"`), which is passed through as
   `wso2 product install <ns>@<version>`. A product that is already installed
   is left alone even if the pin differs: the mismatch is reported, and
   `--update-products` adopts the pin. Installation is machine-wide, so one
   context's pin can't silently move another context's product.
   `--no-install` never downloads. It then requires each product to be
   installed already, or its record to be complete in the input, since
   defaults can't be resolved without a descriptor.
5. **Resolve the defaults** from the now-installed descriptors and build the
   complete records.
6. **End the sessions** whose bindings change, as listed in the plan.
7. **Write the document** atomically, under the existing lock
   (`contexts.Update`).

**Partial failure:** if one installation fails after another succeeded, the
successful ones stay installed. An installed product changes no
configuration, and installing is idempotent. `contexts.json` stays untouched.
The report lists what was installed and what failed, and running the command
again continues from there.

`context product add` goes through the same steps for a single product.

## Command surface

```
wso2 login | logout | whoami                     (logout: selected context only)

wso2 context create <name> (--login-product <ns> --url <url> | --issuer <url> --client-id <id>) [...]
wso2 context product add <ns> --url <url> [--gateway <url>] [--scopes …] [--audience …] [--replace] [--dry-run]
wso2 context product remove <ns> [--dry-run]
wso2 context list | use | current | show | delete | rename
wso2 context apply -f <file> [--use <name>] [--dry-run] [--no-install] [--update-products]
wso2 context edit                   open in $EDITOR on the complete document, validate, refuse invalid edits
wso2 context export [<name>]        complete records, with credentialRef and defaultContext removed

wso2 org use <org>                  unchanged
wso2 product …                      unchanged (ADR 0015)
```

## Rollout

- **Redirects, not "unknown command".** This reuses ADR 0015's pattern: the
  old verbs are recognized only after normal dispatch has failed, so no
  namespace is shadowed. The replacement is built from what the user typed:
  - `wso2 api connect http://x:9091 --gateway --account demo` prints
    `wso2 context product add api --url <api-url> --gateway http://x:9091 --context demo`
  - `wso2 account list` prints `wso2 context list`, and so on for each
    shipped verb.
  - Keep the redirects until an issue removes them. File that issue when the
    command change merges.
- **Revised PR split.** Docs go with the commands they describe, not at the
  end.
  1. **Session binding.** Store audience and resource with each session and
     check the full binding on reuse. v3 only, no format change, and useful
     on its own.
  2. **v4 model, migration, and an internal compatibility layer.** The new
     types; read v1/v2/v3 and write v4; enforce one ref per context. Existing
     callers keep compiling through an adapter that builds today's
     `Selection.Identity` view from a context, so `Account` stays internal
     for one PR. The full migration test matrix (below) and a test that an
     older shell fails closed on v4 (`documentFrozen`).
  3. **Command change.** `context create/product add/remove/delete/rename`,
     `logout` limited to the selected context, `connect` and `account`
     removed, the redirects, and removing the adapter. In the **same PR**:
     `docs/reference/commands.md`, the login guides, `authentication-contexts.md`,
     `CONTEXT.md`, ADR 0016, and the demo notebook.
  4. **File-first and installation.** `apply`, `edit`, `export`, the install
     step, `--dry-run`/`--no-install`/`--update-products`, the
     changed-defaults notice in `doctor`, and their docs.

## Migration test matrix

Each case asserts the v4 output, the sessions kept or invalidated, and the
printed report:

- v1 document: still read-only, never written back
- v2 (`identities` key) and v3 (`accounts` key)
- one account with one context
- one account shared by several contexts, with the default context among
  them and with it not among them
- **an account no context references**: becomes a context named after the
  account so no configuration is lost. On a name collision it gets a
  suffix, which is reported
- each kind: `oauth-browser`, `oauth-device`, `client-credentials` (including
  a product's own `clientIdVariable` and `clientSecretVariable`), `pat`
- a pinned `loginProduct`, and an unpinned one (frozen to the effective one)
- a gateway with and without audience/scope overrides
- `endpoint` renamed to `url` on every product and gateway
- grants `exchange`, `jwt-bearer`, and `federated`, with and without
  `resource`
- `provider` and `narrowing` both set, where narrowing overrides provider
- organization and project set
- a context that references a missing account (should be refused as today,
  never silently dropped)

## ADRs and domain docs

- **New ADR 0016.** It replaces ADR 0015's account concept, and it answers
  ADR 0015's rejection of "fold account under context" ("several contexts may
  name one account") by giving each context its own sessions (see *Session
  ownership*). Cloud multiple organizations are handled by `wso2 org use`
  within one context.
- **ADR 0012** (writing a context grants nothing) still holds. Update its
  wording.
- **ADR 0014** (one login, one session per product): sessions are now keyed
  by the context's ref.
- **ADR 0010** (best-effort revocation): now used by delete and by rebinding.
- **`CONTEXT.md`**: retire **Account**. Redefine **Login product**,
  **Product session**, and **Acquisition strategy** in terms of a context.
  Add the input-file versus local-document distinction.

## Where the work is

- `internal/contexts/`: `contexts.go`, `identity.go`, `access.go`,
  `legacy.go`, `save.go`, `remove.go`, fixtures
- `internal/auth/session/session.go` (binding fields),
  `internal/auth/source_session.go` (reuse check), `source.go`,
  `narrowing.go`
- `internal/app/`: delete `connect*.go` and the `identity*.go` account
  family; rework `context.go`, `login_create.go`, `login_target.go`,
  `logout.go`, `accountname.go`, `doctor*.go`, `whoami.go`; add redirects
  next to ADR 0015's
- `internal/install/` for the apply install step
- Acceptance tests: `login_test.go`, `logout_test.go`, `whoami_test.go`,
  `status_test.go`
- Docs listed in PR 3 and PR 4, plus
  `~/dev/wso2/cli-exercise-6/secure-api-demo.ipynb`
- Leave the uncommitted change to `internal/app/identity.go` on
  `preview/product-modules` alone. It predates this handoff.

## Resolved during implementation

1. Redirects fire the same way for people and scripts: `shell.command_moved`,
   exit class usage (64), the replacement in the recovery.
2. `context edit` takes complete records only and points to `apply` for the
   short form.
3. `context create` selects only with `--use`, the same rule as `apply`.
   `wso2 login --url` still selects the first context it creates.
4. `context product add/remove` target the selected context, with `--context`
   (and `WSO2_CONTEXT`) to pick another.
5. `context create --login-product` installs a missing product, reported on
   standard error; `--no-install` refuses instead.
6. The gateway redirect prints `--url <api-url>` as a placeholder and adds
   `--replace`, because the product was always recorded first.

## Implementation notes

- `internal/contexts`: `Document{SchemaVersion, DefaultContext, Contexts}`;
  `Context` holds `credentialRef`, `login`, `products`. `migrate.go` reads v2/v3
  (shared accounts, orphans, frozen login product, `endpoint`→`url`);
  `contexts.Upgrade` rewrites once at dispatch and prints the migration notes.
  `defaultContext` may be empty (nothing selected).
- The internal adapter stays: `Context.Account()` builds the `contexts.Account`
  view the broker and access plan read (`Selection.Identity`). It has no
  presence in the file or the commands. Renaming it is a follow-up.
- Session binding: `session.Session` records `resource`, `audience` and
  `bound`; an unbound entry is refused for a resource-bound record with its own
  message. `SessionsUnreachedBy` compares bindings, not just references.
- `internal/app`: `context_create.go`, `context_product.go`, `context_apply.go`
  (apply, export, drift notes), `context_edit.go`, `context_lifecycle.go`
  (rename, delete), `context_write.go` (plan, end sessions, replay under lock),
  `context_resolve.go` (descriptor defaults, install), `redirects.go`.
  `connect*.go` and `identity*.go` are deleted. `doctor` gained a `defaults`
  check (`differs`, never a failure).
- Guards: `TestNoUserVisibleStringNamesAnAccount` in `internal/boundaries`.
- Docs: guides, `commands.md`, `authentication-contexts.md`, module reference,
  `architecture.md` §4.6–4.7, `CONTEXT.md`, ADR 0016 and amendments to 0010,
  0012, 0014, 0015.
