# `iam` module (ThunderID) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `modules/iam` product module that bootstraps the CLI's own
client in ThunderID and then registers resource servers, users, apps and
roles through shell-brokered `system` access.

**Architecture:** One Go module `github.com/wso2/wso2-cli/modules/iam`
composed by `go.work`, executable `cmd/wso2-module-iam`, declaring command
tree via `cobratree`. A small `internal/thunder` package wraps the REST
calls behind typed functions taking an `http.Client` and a bearer token,
so handlers stay short and tests fake one HTTP server.

**Tech Stack:** Go 1.25, cobra, SDK v0.2.0 (`module`, `cobratree`,
`result`, `problem`, `testkit`), `httptest`.

**Spec:** `docs/superpowers/specs/2026-09-04-iam-apim-modules-design.md` §4, §6.

## Global Constraints

- Apache-2.0 header on every Go file; no stdout writes outside the
  protocol; no import of the shell's `internal` tree (boundaries tests).
- Module manifest `module.json`: namespace `iam`, `authAudiences`
  `["thunder-system"]`, `authScopes` `["system"]`, shell `>=0.1.0 <2.0.0`,
  protocol `[2]`.
- Every result ends with a `next` field (spec §3.3).
- Problem codes: `iam.unavailable`, `iam.refused`, `iam.unreadable`,
  `iam.no_endpoint`, `iam.missing_secret` (category usage), `iam.not_found`.
- Secrets enter only through `WSO2_IAM_*` variables named on the command
  line; never through flag values; never printed except a generated client
  secret once at creation.
- Thunder seeded ids on 1.0.0-beta: default OU
  `01900000-0000-7000-8000-000000000001`, auth flow
  `01900000-0000-7000-8000-000000000061`, console client `CONSOLE`,
  console redirect `{issuer}/console`, system resource
  `https://localhost:8090/mcp`, scope `system`. They are flags with these
  defaults, never hard-coded in a handler.

---

## File structure

```
modules/iam/
  go.mod                      module github.com/wso2/wso2-cli/modules/iam; require sdk v0.2.0, cobra
  module.json
  README.md                   (adapt from the scaffold's text)
  cmd/wso2-module-iam/
    main.go                   Namespace, moduleOptions, commands(), status
    bootstrap.go              iam bootstrap
    resourceservers.go        iam resource-servers list|create
    users.go                  iam users list|create
    apps.go                   iam apps list|create
    roles.go                  iam roles list|create
    main_test.go              contract tests
    *_test.go                 one per command file, against a fake Thunder
  internal/thunder/
    client.go                 Client{Base string; HTTP *http.Client; Token string}; do(); errors → problem
    httpclient.go             HTTPClient() honouring WSO2_CA_FILE
    login.go                  SystemToken(ctx, client, issuer, user, password, flags) — the console flow
    types.go                  request/response structs
    thunder_test.go
```

Each `create` handler: acquire access → look up by name → create if absent
→ result with `created: true|false` and `next`.

### Task 1: Module skeleton, manifest, workspace, contract tests

**Files:** `modules/iam/{go.mod,module.json,README.md}`,
`cmd/wso2-module-iam/{main.go,main_test.go}`, `go.work` (add `./modules/iam`).

- [ ] **Step 1:** Copy `~/dev/wso2/cli-exercise/apim-probe-module` layout as
  a starting point but write fresh files: `main.go` declares `status` only
  (fields `namespace`, `version`, `endpoint`, `next` = "Run wso2 iam
  bootstrap --url <issuer> once, then wso2 identity create as it prints.").
- [ ] **Step 2:** `main_test.go` = the two contract tests (status answers,
  unknown command refused) exactly as the probe's, plus
  `TestEveryResultEndsWithNext` that walks every handler registered in
  `commands()` through `testkit.Run` with a fake Thunder answering empty
  lists and asserts the last field is `next` (add handlers to this test as
  later tasks land).
- [ ] **Step 3:** `go test ./modules/iam/...` PASS; `go test ./internal/boundaries/`
  passes for `modules/iam` (license header, workspace, builds).
- [ ] **Step 4:** Commit — `feat(iam): scaffold the ThunderID module`

### Task 2: `internal/thunder` client and error mapping

**Files:** `modules/iam/internal/thunder/{client.go,httpclient.go,types.go,thunder_test.go}`

**Interfaces (produced):**
```go
type Client struct { Base string; HTTP *http.Client; Token string }
func New(base, token string) *Client            // HTTP = HTTPClient()
func (c *Client) Get(ctx, path string, into any) error
func (c *Client) Post(ctx, path string, body, into any) error
// errors: *Refusal{Status int; Body string} for non-2xx; wrapped net errors otherwise
func Problem(err error, doing string) problem.Problem
// maps Refusal → iam.refused "ThunderID answered 409 to <doing>: <body>",
// network → iam.unavailable, decode → iam.unreadable
func HTTPClient() *http.Client                  // WSO2_CA_FILE as in the probe
```
Types: `ResourceServer{ID,Name,Identifier,OUID}`, `Resource{ID,Name,Handle,Parent,Permission}`,
`Application{ID,Name,Type,ClientID}` with `InboundAuthConfig []struct{Type; Config OAuthConfig}`,
`OAuthConfig{ClientID,ClientSecret,RedirectURIs,GrantTypes,ResponseTypes,TokenEndpointAuthMethod,PKCERequired,PublicClient}`,
`User{ID; Attributes map[string]any}`, `Role{ID,Name}`, list wrappers
`{TotalResults int; ResourceServers/Resources/Applications/Users/Roles []T}`.

- [ ] **Step 1:** Test: fake server returns 409 `{"message":"exists"}` →
  `Problem(err,"the role creation")` has code `iam.refused` and message
  containing `409` and `exists`; unreachable base → `iam.unavailable`;
  bad JSON → `iam.unreadable`; `Post` sends `Authorization: Bearer` and
  `Content-Type: application/json`.
- [ ] **Step 2:** Run — FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit — `feat(iam): add the ThunderID REST client`

### Task 3: `iam bootstrap`

**Files:** `cmd/wso2-module-iam/bootstrap.go`, `bootstrap_test.go`,
`internal/thunder/login.go`.

Flags: `--url` (required), `--admin-user` (default `admin`),
`--password-variable` (default `WSO2_IAM_ADMIN_PASSWORD`), `--client-id`
(default `wso2-cli`), `--console-client` (`CONSOLE`),
`--system-resource` (`https://localhost:8090/mcp`), `--ou`
(default OU id), `--auth-flow` (default flow id).

`thunder.SystemToken` performs the sequence from `thunder-token.sh`:
1. GET `{issuer}/oauth2/authorize?response_type=code&client_id=CONSOLE&redirect_uri={issuer}/console&scope=openid system&resource=<system>&state=…&code_challenge=…&code_challenge_method=S256` with a cookie jar, no redirect following; read `authId` (or `auth_id`) and `executionId` from the `Location`.
2. POST `{issuer}/flow/execute` `{"executionId":…,"inputs":{"username":…,"password":…}}` → `challengeToken`.
3. POST again with `challengeToken`, `"action":"action_001"` and the same inputs → `assertion`.
4. POST `{issuer}/oauth2/auth/callback` `{"authId":…,"assertion":…}` → `redirect_uri` carrying `code`.
5. POST `{issuer}/oauth2/token` `grant_type=authorization_code&client_id=CONSOLE&code=…&redirect_uri=…&code_verifier=…` → `access_token`.
A failure at any step returns `iam.refused` naming the step; a wrong
password surfaces Thunder's own message.

Handler: password from `os.Getenv(variable)`; empty → usage problem
`iam.missing_secret` "set WSO2_IAM_ADMIN_PASSWORD". Then list
applications; if one has `inboundAuthConfig[0].config.clientId == clientID`
report found; else POST the public app body from `thunder-register.py`
(`type: custom`, `authFlowId`, `url: http://127.0.0.1:10425`,
`allowedUserTypes: ["Person"]`, redirect URIs for 10425–10428, grants
`authorization_code refresh_token`, `tokenEndpointAuthMethod: none`,
`pkceRequired: true`, `publicClient: true`). Result fields:
`issuer`, `clientId`, `created`, `next` =
`"Run wso2 identity create thunder-admin --issuer <url> --client-id <id> --provider thunder --product iam --endpoint <url> --audience <system-resource> --scope system, then wso2 login --context thunder-admin."`

- [ ] **Step 1:** Test with a fake Thunder implementing the five login
  endpoints and `/applications`: asserts the authorize query, the two
  flow bodies, that the token request carries the verifier for the
  challenge sent, that the app POST body matches the shape above, and that
  a second run with the app present POSTs nothing and reports
  `created false`. Unset variable → problem `iam.missing_secret`, no HTTP.
- [ ] **Step 2–4:** FAIL → implement → PASS.
- [ ] **Step 5:** Commit — `feat(iam): add iam bootstrap`

### Task 4: `iam resource-servers list|create`

Flags for create: positional `<name>`, `--identifier` (required),
`--permission a:b:c` repeatable, `--ou`.

Create: acquire `thunder-system`/`system`; GET `/resource-servers`, match
name; POST `{"name","description":"Registered by wso2 iam","identifier","ouId"}`
if absent. Then for each permission `a:b:c`: GET
`/resource-servers/{id}/resources?limit=200`, walk handles from the root
creating missing `{"name": Title(handle), "handle", "parent": <id or omitted>}`.
Result: `id`, `name`, `identifier`, `created`, `permissions` (joined),
`next` = `"Run wso2 iam roles create <role> --resource-server \"<name>\" --permission <first> to grant it."`
List result: `total`, `resourceServers` ("name (identifier)" joined), `next`
naming create.

- [ ] Test: fake with an empty server then a server holding `reference`;
  asserts creation order and that an existing parent is reused; 409 →
  `iam.refused`.
- [ ] Commit — `feat(iam): add resource-servers list and create`

### Task 5: `iam users list|create`

Create: `<username>`, `--email`, `--given-name`, `--family-name`,
`--password-variable` (required; value read from `os.Getenv`; empty →
`iam.missing_secret`), `--ou`. Body:
`{"ouId","type":"Person","attributes":{"username","email","given_name","family_name","password"}}`.
Lookup by `attributes.username`. Result: `id`, `username`, `created`,
`next` = roles create with `--assign-user <username>`. List: `total`,
`users` (usernames joined), `next`.

- [ ] Test asserts the password never appears in the result fields and
  the body carries it; commit — `feat(iam): add users list and create`

### Task 6: `iam apps list|create`

Create: `<client-id>`, `--type m2m|public` (required), `--name` (default
client id), `--ou`, `--auth-flow`. `public` → the bootstrap body shape;
`m2m` → `type: m2m`, `grantTypes: ["client_credentials"]`,
`tokenEndpointAuthMethod: client_secret_basic`, `clientSecret` generated
(`crypto/rand`, 32 bytes, base64url). Result: `id`, `clientId`, `type`,
`created`, `clientSecret` (only when generated this run, else `(not shown again)`),
`next` = for m2m: `"export WSO2_THUNDER_CI_SECRET=<secret>; then wso2 identity create <name> … --client-secret-variable WSO2_THUNDER_CI_SECRET …"`.
Lookup by clientId in `inboundAuthConfig`.

- [ ] Test asserts both bodies and that a second run shows no secret;
  commit — `feat(iam): add apps list and create`

### Task 7: `iam roles list|create`

Create: `<name>`, `--resource-server <name>` (required), `--permission`
repeatable, `--assign-user <username>` repeatable, `--assign-app
<client-id>` repeatable, `--ou`. Resolve the resource server, users and
apps by name to ids (`iam.not_found` when absent, naming which). POST
`/roles` `{"name","description","ouId","permissions":[{"resourceServerId","permissions":[…]}],"assignments":[{"type":"user","id"}...]}`;
then POST `/roles/{id}/assignments/add` `{"assignments":[{"type":"app","id"}]}`
for apps (this is how the proven script did it). Result: `id`, `name`,
`created`, `assigned` (joined), `next` = `"Run wso2 login --context <caller> and call the API, or wso2 apim bootstrap to register API Manager."`

- [ ] Test asserts ids resolved and both POST bodies; commit —
  `feat(iam): add roles list and create`

### Task 8: Install and prove against `cli-thunder`

- [ ] `WSO2_HOME=<fresh dir> make install-module NAMESPACE=iam`; run the
  Thunder half of the spec §1 journey verbatim (bootstrap through roles
  create) against the live container with a **new** set of names
  (`Mock API 2`, `cliuser2`, `wso2-cli-ci2`, role `Mock API Caller 2`) so
  the existing state stays. Record every command's output and exit code in
  `~/dev/wso2/cli-exercise/iam-apim-journey-findings.md`.
- [ ] `go test ./modules/iam/... ./internal/boundaries/` PASS; commit
  the README/docs touch-ups — `docs(iam): describe the module's commands`
