# `apim` module (API Manager) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `modules/apim` product module that registers the CLI on API
Manager's resident key manager, publishes an API from an OpenAPI file,
subscribes an application, wires ThunderID in as a key manager, and calls
the API through the gateway.

**Architecture:** Mirror of the `iam` module: `internal/apim` client with
typed calls per plane (publisher v4, devportal v3, admin v4), handlers
that acquire exactly the scopes they need, one fake server per test file.

**Tech Stack:** as for `iam`; multipart upload for `import-openapi`.

**Spec:** `docs/superpowers/specs/2026-09-04-iam-apim-modules-design.md` §5, §6.

## Global Constraints

- As for the `iam` plan (header, no stdout, no shell internals, `next` on
  every result).
- `module.json`: namespace `apim`, `authAudiences` `["apim-publisher"]`,
  `authScopes` `["apim:api_view","apim:api_create","apim:api_publish","apim:subscribe","apim:app_manage","apim:admin"]`.
- The product audience in the context document is the DCR client id; the
  README and bootstrap's `next` line say so.
- Codes: `apim.unavailable`, `apim.refused`, `apim.unreadable`,
  `apim.not_found`, `apim.missing_secret`, `apim.no_endpoint`.
- Every management call goes to `{endpoint}/api/am/<plane>/…`; bootstrap
  and gateway invoke are the only commands not under `/api/am`.

---

## File structure

```
modules/apim/
  go.mod, module.json, README.md
  cmd/wso2-module-apim/
    main.go        status; commands()
    bootstrap.go   apim bootstrap
    apis.go        apim apis list|import|deploy|publish
    apps.go        apim apps list|create|subscribe|keys generate|map-keys
    keymanagers.go apim key-managers list|add
    gateway.go     apim gateway invoke
    *_test.go
  internal/apim/
    client.go      Client{Base; HTTP; Token}; Get/Post/PostMultipart; Refusal; Problem()
    httpclient.go  WSO2_CA_FILE
    types.go
```

### Task 1: Skeleton, manifest, workspace, contract tests
As `iam` Task 1 with namespace `apim`; `status`'s `next` =
"Run wso2 apim bootstrap --url <base> once." Commit —
`feat(apim): scaffold the API Manager module`.

### Task 2: `internal/apim` client
As `iam` Task 2 plus `PostMultipart(ctx, path string, file io.Reader, filename string, fields map[string]string, into any)`
and `PostForm` (for the token endpoint). Commit —
`feat(apim): add the API Manager REST client`.

### Task 3: `apim bootstrap`
Flags: `--url` (base, required), `--admin-user` (`admin`),
`--password-variable` (`WSO2_APIM_ADMIN_PASSWORD`), `--client-name`
(`wso2-cli`), `--callback` (`http://127.0.0.1:10425/callback`).
POST `{base}/client-registration/v0.17/register` with basic auth and body
`{"callbackUrl","clientName","owner":<admin-user>,"grantType":"password refresh_token client_credentials authorization_code","saasApp":true,"tokenType":"JWT"}`.
The endpoint is idempotent by name on APIM (it returns the existing app
with its secret); report `created` from whether `clientId` was already
known via a prior GET `{base}/client-registration/v0.17/register/{clientName}`
is not available, so: always POST, and say "registered or found".
Result: `clientId`, `clientSecret`, `issuer` (`{base}/oauth2/token`),
`next` = `"export WSO2_APIM_CLIENT_SECRET=<secret>; wso2 identity create apim-admin --issuer <issuer> --client-id <id> --client-secret-variable WSO2_APIM_CLIENT_SECRET --product apim --endpoint <base> --audience <id> --scope apim:api_view --scope apim:api_create --scope apim:api_publish --scope apim:subscribe --scope apim:app_manage --scope apim:admin"`.
Test: fake DCR asserts basic auth header and body; unset password →
`apim.missing_secret`. Commit — `feat(apim): add apim bootstrap`.

### Task 4: `apim apis list|import|deploy|publish`
- `list` (scope `apim:api_view`): GET `/api/am/publisher/v4/apis` →
  `count`, `apis` ("name/version context (state)"), `next` = import.
- `import --file --name --version --api-context --backend [--policy Unlimited]`
  (`apim:api_create`): multipart `file` + `additionalProperties` JSON
  `{"name","version","context","policies":[policy],"endpointConfig":{"endpoint_type":"http","production_endpoints":{"url":backend},"sandbox_endpoints":{"url":backend}}}`
  to `/apis/import-openapi`; if an API with that name/version exists
  (`GET /apis?query=name:<name>`), report found. Result `id`, `name`,
  `version`, `context`, `created`, `next` = deploy.
- `deploy <name/version> [--gateway Default] [--vhost localhost]`
  (`apim:api_publish`): resolve id (`apim.not_found`); POST
  `/apis/{id}/revisions` `{"description":"wso2 apim deploy"}` → POST
  `/apis/{id}/deploy-revision?revisionId=` `[{"name":gateway,"vhost":vhost,"displayOnDevportal":true}]`;
  poll `GET /apis/{id}/deployments` until `successDeployedTime > 0` for
  that gateway or 60 s. Result `id`, `revision`, `gateway`, `status`,
  `next` = publish.
- `publish <name/version>` (`apim:api_publish`): POST
  `/apis/change-lifecycle?apiId=&action=Publish`; already published →
  found. Result `id`, `state`, `next` = `apps create`.
Tests: one fake covering the four; assert multipart field names, the
revision then deploy order, and the poll stopping on success. Commit —
`feat(apim): add apis list, import, deploy and publish`.

### Task 5: `apim apps list|create|subscribe|keys generate|map-keys`
Scopes `apim:subscribe apim:app_manage`, devportal v3.
- `create <name> [--policy Unlimited]`: lookup `GET /applications?query=<name>`
  by exact name; POST `{"name","throttlingPolicy","tokenType":"JWT"}`.
  `next` = subscribe.
- `subscribe <app> <name/version> [--policy Unlimited]`: resolve app and
  API (devportal `GET /apis?query=name:<name>`), POST `/subscriptions`;
  "already exists" refusal → found. `next` = `keys generate` or `map-keys`.
- `keys generate <app> [--key-manager "Resident Key Manager"] [--grant client_credentials]...`:
  POST `/applications/{id}/generate-keys` `{"keyType":"PRODUCTION","grantTypesToBeSupported":[…],"validityTime":3600,"keyManager":km,"scopes":["default"]}`;
  then verify by `PostForm {base}/oauth2/token grant_type=client_credentials`
  with the returned key pair; a refusal there → `apim.refused` "the key
  manager did not honour the key it issued; delete the mapping and retry".
  Result `consumerKey`, `consumerSecret`, `verified`, `next` = gateway invoke.
- `map-keys <app> --key-manager <name> --client-id <id> [--key-type PRODUCTION]`:
  POST `/applications/{id}/map-keys` `{"consumerKey","consumerSecret":"not-held-here","keyManager","keyType"}`;
  "Key Manager not Registered" → retry up to 5× with 2 s pause (measured
  propagation delay). `next` = gateway invoke under the Thunder identity.
Commit — `feat(apim): add apps create, subscribe, keys and map-keys`.

### Task 6: `apim key-managers list|add`
Scope `apim:admin`, admin v4. `add <name> --well-known <issuer> [--jwks]
[--token-endpoint] [--revoke-endpoint] [--consumer-key-claim client_id]
[--scopes-claim scope]`: GET `{issuer}/.well-known/openid-configuration`
for `jwks_uri`, `token_endpoint`, `revocation_endpoint` (fallback
`{issuer}/oauth2/revoke`), overridable; POST the `CustomKeyManager` body
from `apim-thunder-km.sh` (`enableTokenGeneration false`,
`enableMapOAuthConsumerApps true`, `enableOAuthAppCreation false`,
`enableSelfValidationJWT true`, `tokenValidation [{enable true, type JWT, value {body {}}}]`,
`tokenType DIRECT`, `permissions PUBLIC`). Existing name → found. `next` =
map-keys. Commit — `feat(apim): add key-managers list and add`.

### Task 7: `apim gateway invoke <path> [--method GET] [--body -]`
Acquire with the identity's own product audience and scopes: the handler
reads them from `request.Context`? No: `module.Request` carries no scopes;
so the command takes `--audience` and `--scope` flags defaulting to the
module's declared ones, and the README states that under a Thunder
identity the product entry's audience/scopes are what the broker honours
(`declared_audiences` is a hint; the broker binds to the product's
audience). Call `{endpoint}{path}` with the token; result `status`,
`body` (first 4 KiB), `next` = "(done) or the next path". Commit —
`feat(apim): add gateway invoke`.

### Task 8: Install and prove against `cli-apim`
- `WSO2_HOME=<fresh> make install-module NAMESPACE=apim`; run the APIM
  half of spec §1 with new names (`MockAPI2`, `CliApp2`, key manager
  `Thunder2`) and record outputs in the findings file. Commit docs —
  `docs(apim): describe the module's commands`.

### Task 9: The fresh-environment journey (spec §8)
- New `WSO2_HOME`, fresh `cli-thunder2`/`cli-apim2` containers on offset
  ports if memory allows, else the existing ones with a wiped `WSO2_HOME`;
  run spec §1 top to bottom with no exercise script. Record in
  `iam-apim-journey-findings.md` and update `HANDOFF.md`. Findings feed
  the shell backlog.
