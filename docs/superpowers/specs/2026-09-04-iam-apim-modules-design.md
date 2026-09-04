# Design: the `iam` and `apim` modules and a setup journey typed only as `wso2`

**Status:** Approved design, 2026-09-04
**Builds on:** the measured journeys in the exercise directory
(`wso2-cli-whole-picture-gap-analysis.md`, `apim-journey-findings.md`), SDK
v0.2.0, shell `claude/apim-journey-step1` (adds `WSO2_CA_FILE`).

## 1. Goal

A user with a fresh ThunderID and API Manager, and an API of their own, can
register everything and call the API through the gateway with `wso2`
commands, typing a product administrator password exactly twice and editing
no JSON. The target journey:

```sh
export WSO2_CA_FILE=$PWD/apim-9443.pem

# ThunderID: register this CLI as a public client, once.
WSO2_IAM_ADMIN_PASSWORD=… wso2 iam bootstrap --url http://localhost:8490
wso2 identity create thunder-admin --issuer http://localhost:8490 \
  --client-id wso2-cli --provider thunder \
  --product iam --endpoint http://localhost:8490 \
  --audience https://localhost:8090/mcp --scope system
wso2 login --context thunder-admin

# ThunderID: the user's API and who may call it.
wso2 iam resource-servers create "Mock API" --identifier http://localhost:18080/mockapi \
  --permission reference:status:read --permission reference:status:write --permission orders:read
wso2 iam users create cliuser --email cliuser@example.com --password-from WSO2_IAM_USER_PASSWORD
wso2 iam apps create wso2-cli-ci --type m2m
wso2 iam roles create "Mock API Caller" --resource-server "Mock API" \
  --permission reference:status:read --permission orders:read \
  --assign-user cliuser --assign-app wso2-cli-ci

# API Manager: register this CLI, once.
WSO2_APIM_ADMIN_PASSWORD=admin wso2 apim bootstrap --url https://localhost:9443
export WSO2_APIM_CLIENT_SECRET=…            # printed once by bootstrap
wso2 identity create apim-admin --issuer https://localhost:9443/oauth2/token \
  --client-id <id> --client-secret-variable WSO2_APIM_CLIENT_SECRET \
  --product apim --endpoint https://localhost:9443 --audience <id> \
  --scope apim:api_view … (the line bootstrap prints)

# API Manager: publish the API, subscribe, wire ThunderID in.
wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.0.0 \
  --context /mockapi --backend http://host.docker.internal:18080 --context apim-admin
wso2 apim apis deploy MockAPI/1.0.0 --context apim-admin
wso2 apim apis publish MockAPI/1.0.0 --context apim-admin
wso2 apim key-managers add Thunder --well-known http://localhost:8490 \
  --jwks http://host.docker.internal:8490/oauth2/jwks --context apim-admin
wso2 apim apps create CliApp --context apim-admin
wso2 apim apps subscribe CliApp MockAPI/1.0.0 --context apim-admin
wso2 apim apps map-keys CliApp --key-manager Thunder --client-id wso2-cli-ci --context apim-admin

# Call the API through the gateway with a ThunderID token.
wso2 identity create thunder-caller --issuer http://localhost:8490 --client-id wso2-cli \
  --provider thunder --product apim --endpoint https://localhost:8243 \
  --audience http://localhost:18080/mockapi --scope reference:status:read --scope orders:read
wso2 login --context thunder-caller
wso2 apim gateway invoke /mockapi/1.0.0/status --context thunder-caller
```

Out of scope, recorded for later: a WSO2-seeded `wso2cli` public client in
ThunderID (would remove `iam bootstrap`; needs a design discussion with the
Thunder team); a public client on API Manager's resident key manager (not
supported by the product); tenants and organization units; `apictl` parity
beyond the commands above; named endpoints per product.

## 2. Three pieces, built in this order

1. **Shell**: `wso2 identity create` and a namespace-scoped module
   environment.
2. **`iam` module** (ThunderID) under `modules/iam`.
3. **`apim` module** (API Manager) under `modules/apim`.

Then the journey above is run on a second, fresh environment with nothing
else, which is the acceptance test for the whole design.

## 3. Shell

### 3.1 `wso2 identity create`

```text
wso2 identity create <name> --issuer <url> --client-id <id>
    [--client-secret-variable <VAR>] [--provider asgardeo|identity-server|thunder]
    [--product <ns> --endpoint <url> [--audience <uri>] [--scope <s>]...]
```

Writes one identity and a same-named context to the context document, and
selects the context when none is selected. The kind is `oauth-browser`
unless `--client-secret-variable` is given, which makes it
`client-credentials`. `--product` records one product exactly as
`identity add-product` would; the four product flags are only legal
together. `--audience` is required when the derivation is `token-resource`
(Thunder), because the login is bound to it, and a Thunder identity takes
at most one product (existing rule). Nothing is stored in the secure store;
the document names a variable or a credential reference, never a value
(ADR 0012). Refusals reuse the existing codes: `shell.missing_required_flag`,
`shell.invalid_argument`, `shell.conflicting_arguments`, and the context
package's own validation errors for a duplicate name or an unknown provider.

Table output states what was written; `--output json` returns the same
fields. The reason this exists: it is the hand-off point from a module's
bootstrap to the shell (option 1 of the design discussion), and it closes
finding #3 of the gap analysis on its own.

### 3.2 Namespace-scoped module environment

`modules.SanitizedEnvironment(namespace)` keeps starting from nothing and
adds, besides `WSO2_CA_FILE`, every variable named `WSO2_<NAMESPACE>_*`
with the namespace upper-cased. Both launch sites pass the namespace. A
module `iam` therefore sees `WSO2_IAM_ADMIN_PASSWORD` and nothing a CI
runner exported for anything else; a downloaded module `foo` can only read
what a user deliberately prefixed with `WSO2_FOO_`. The test pins both
directions. This is the only way a bootstrap can receive an administrator
password, because a module cannot prompt: its standard streams carry
protocol frames.

Documented in `docs/reference/commands.md` beside `WSO2_CA_FILE`, and in
`docs/guides/building-product-modules.md` as the rule for module secrets.

## 4. `iam` module

Namespace `iam`, executable `wso2-module-iam`, SDK v0.2.0, declaring tree
(`cobratree.Tree.Serve`). Audience `thunder-system`, scope `system`: every
command except `bootstrap` and `status` acquires that through the broker
and calls `{endpoint}/…` with it. All Thunder request and response shapes
come from the exercise's `thunder-api/*.yaml` and the registration script
that was proven against 1.0.0-beta.

| Command | Does | Thunder calls |
| --- | --- | --- |
| `iam status` | Version, endpoint, whether the context reaches Thunder | none |
| `iam bootstrap --url <issuer> [--admin-user admin] [--client-id wso2-cli]` | Obtains a `system` token as the administrator by driving the seeded console client's authorization flow (`/oauth2/authorize` → `/flow/execute` → `/oauth2/auth/callback` → `/oauth2/token`, the sequence in `thunder-token.sh`), creates the public application `wso2-cli` (custom type, auth code + refresh, PKCE required, loopback redirects 10425–10428) if absent, then prints the two shell commands to run next. Password from `WSO2_IAM_ADMIN_PASSWORD`; refused with a usage problem naming the variable when unset. Idempotent. | `GET/POST /applications` |
| `iam resource-servers list \| create <name> --identifier <uri> [--permission a:b:c]...` | A resource server and its permission tree; `a:b:c` is created as the handle path `a` › `b` › `c`, parents reused | `/resource-servers`, `/resource-servers/{id}/resources` |
| `iam users list \| create <username> --email <e> --password-from <VAR>` | People in the default organization unit | `/users` |
| `iam apps list \| create <client-id> --type m2m\|public [--name]` | An m2m app gets a generated secret printed once; a public app gets the loopback redirects | `/applications` |
| `iam roles list \| create <name> --resource-server <name> --permission …  [--assign-user u]... [--assign-app c]...` | A role with permissions and assignees | `/roles`, `/roles/{id}/assignments/add` |

Every `create` is idempotent by name lookup and reports whether it created
or found. Product-service failures use `problem.CategoryProductService`
with codes `iam.unavailable`, `iam.refused`, `iam.unreadable`; the refusal
message carries Thunder's own error text. The one-product-per-identity rule
is explained when `auth.product_not_configured` comes back under a Thunder
identity that carries another product.

Tests: `testkit.Run` for the contract (status, unknown command), and
`httptest` fakes of the Thunder endpoints for every command, including the
bootstrap flow, with the request bodies asserted against the shapes above.

## 5. `apim` module

Namespace `apim`, executable `wso2-module-apim`. Audience `apim-publisher`
(the product audience is the resident key manager's client id, a measured
quirk the guide states plainly), scopes per command as listed; the broker
issues exactly the scopes a command asks for, which is what the narrowing
check requires. Every call goes to `{endpoint}/api/am/{publisher/v4,
devportal/v3, admin/v4}/…`.

| Command | Scopes | Calls |
| --- | --- | --- |
| `apim status` | none | none |
| `apim bootstrap --url <base> [--admin-user admin] [--client-name wso2-cli]` | none (basic auth with `WSO2_APIM_ADMIN_PASSWORD`) | DCR `POST /client-registration/v0.17/register` with `tokenType: JWT`, all four grants, the loopback callback. Prints the secret once with an `export` line, and the `wso2 identity create` line whose audience is the client id. Idempotent: an existing client of that name is reported, not re-registered, and the secret is not shown again. |
| `apim apis list \| import --file <openapi> --name --version --context --backend <url> \| deploy <name/version> [--gateway Default] \| publish <name/version>` | `apim:api_view`, `apim:api_create`, `apim:api_publish` | `import-openapi`, `revisions` + `deploy-revision`, `change-lifecycle` |
| `apim apps list \| create <name> \| subscribe <app> <name/version> \| keys generate <app> \| map-keys <app> --key-manager <km> --client-id <id>` | `apim:subscribe`, `apim:app_manage` | devportal `applications`, `subscriptions`, `generate-keys`, `map-keys` |
| `apim key-managers list \| add <name> --well-known <issuer> [--jwks <url>] [--token-endpoint] [--revoke-endpoint]` | `apim:admin` | admin `key-managers`, type `CustomKeyManager`; endpoints derived from the issuer's discovery document, overridable because the gateway sees the host from inside a container |
| `apim gateway invoke <path> [--method GET]` | the identity's own scopes | `{endpoint}{path}` with a token the broker minted for the identity's product audience: run under a ThunderID identity whose `apim` product points at the gateway, this is the user's API called with a ThunderID token |

`deploy` waits for the deployment to report success, because the gateway
answers 404 until then. `keys generate` verifies the key it received by
requesting one token, because a mapping the key manager did not honour was
observed once. Codes `apim.unavailable`, `apim.refused`, `apim.unreadable`,
`apim.not_found` (an API or app named on the command line that does not
exist).

Tests as for `iam`: contract tests plus `httptest` fakes of the four APIM
planes with bodies asserted.

## 6. Shared module code

Both modules need the same three things: an HTTP client that honours
`WSO2_CA_FILE`, a JSON call helper that turns non-2xx answers into a
product-service problem carrying the body, and a `--password-from`/secret
variable reader. They are small enough to live in each module's own
`internal` package; a shared SDK helper (`module.HTTPClient`) is proposed
separately and not blocked on.

## 7. What this does not change

The module contract and protocol, the SDK version, the catalog, and the
release workflow. Both modules are released the way `reference` is
(`iam/v0.1.0`, `apim/v0.1.0` tags) after the journey passes; until then
they are installed through the dev origin.

## 8. Acceptance

The journey in §1, on a fresh `WSO2_HOME` and fresh containers, with no
script from the exercise directory, ends with
`wso2 apim gateway invoke /mockapi/1.0.0/status` answering the mock API's
`status: ok` through the gateway. Every command's table and JSON output and
exit code is recorded in a findings file beside the earlier two.
