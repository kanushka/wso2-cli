# CI pipelines against WSO2 products today, without the wso2 cli

Checked 2026-09-06 against official documentation and released source. Where
a page could not be rendered or a claim could not be traced to a primary
source it is marked "not confirmed". Nothing here was executed against a live
deployment; command shapes are quoted from the sources named.

## Summary

- Every product exposes a REST management surface that accepts a bearer
  token, and every product except legacy API Manager documents an OAuth
  client-credentials path for a machine caller. API Manager's own tooling
  still registers a password-grant client with the admin's username and
  password and stores those base64-encoded on disk.
- Four separate CLIs exist (`iamctl`, `apictl`, `ap`, `amctl`), each with its
  own config file, its own credential file, and its own idea of how CI
  supplies a secret: JSON file or env vars (`iamctl`), flags only
  (`apictl`), env vars (`ap`), flags only (`amctl`).
- WSO2 publishes CI samples for API Manager (Jenkins, a GitOps blog with
  GitHub Actions) and Identity Server (`iamctl` with GitHub Actions, described
  in prose). ThunderID ships composite GitHub Actions inside its own repo.
  Agent Manager and Platform Gateway document non-interactive auth but no
  pipeline sample.
- No Terraform provider for any WSO2 product was found. No product issues a
  personal access token with its own lifecycle except the Developer Platform
  (Choreo) console.

## 1. Identity Server 7.x, WSO2 Account Platform (Asgardeo), ThunderID

### 1.1 Identity Server 7.x

**Management REST APIs.** Three authentication methods are documented: HTTP
Basic with a user's credentials (the page warns against using the super
admin and recommends a least-privilege user), an OAuth2 bearer token obtained
by password or client-credentials grant carrying `internal_*` scopes such as
`internal_application_mgt_view`, and mutual TLS. Example from the docs:

```
curl https://localhost:9443/oauth2/token -k \
  -H "Authorization: Basic Base64(<clientid>:<client-secret>)" \
  -d "grant_type=password&username=<username>&password=<password>&scope=<scope>"
```

Source: [IS 7.1.0 APIs](https://is.docs.wso2.com/en/7.1.0/apis/).

**Machine client.** Register an **M2M Application**; the Console generates a
client ID and secret; the **API Authorization** tab grants management API
scopes to the app; tokens come from the client-credentials grant.
Source: [register M2M app](https://is.docs.wso2.com/en/7.1.0/guides/applications/register-machine-to-machine-app/).

**Configuration CLI: `iamctl`.** The Identity Server docs recommend IAM-CTL
for promoting configuration across environments: register an M2M app, run
`iamctl setupCLI`, edit `serverConfig.json`, then
`iamctl exportAll -c ./configs/env` and `iamctl importAll -c ./configs/env`.
Resource types listed: applications, identity providers, claims, user
stores, API resources, OIDC scopes, roles, email and SMS templates,
governance connectors, validation rules, organizations, branding, actions.
Source: [promote configurations, IS 7.1.0](https://is.docs.wso2.com/en/7.1.0/deploy/promote-configurations/).

`iamctl` details from its repository:

- Written in Go, "uses the management REST APIs"; supports IS 5.11, 7.0,
  7.1, 7.2 and 7.3. Asgardeo is not listed.
- `serverConfig.json` keys: `SERVER_URL`, `CLIENT_ID`, `CLIENT_SECRET`,
  `TENANT_DOMAIN`, `SERVER_VERSION`.
- Without `--config`, the tool reads env vars `SERVER_URL`, `CLIENT_ID`,
  `CLIENT_SECRET`, `TENANT_DOMAIN`, `SERVER_VERSION`, `TOOL_CONFIG_PATH`,
  `KEYWORD_CONFIG_PATH`; `serverConfig.json` values may also be
  `"${DEV_CLIENT_SECRET}"` placeholders resolved from the environment. The
  doc nevertheless "recommend[s] ... the serverConfig.json file ... as it is
  more secure".
- Environment-specific values use `{{KEYWORD}}` placeholders in exported
  YAML and a `keywordConfig.json` map, which may itself reference
  `"${DEV_CALLBACK_DOMAIN}"`.
- The CI/CD page describes a GitHub Actions flow with custom actions named
  `@action/setup`, `@action/export`, `@action/import`, and notes for the
  management-application step "(Automation will be implemented in the
  future)". No workflow YAML is published.

Sources: [README](https://github.com/wso2-extensions/account-tools-cli/blob/master/README.md),
[cli-mode.md](https://github.com/wso2-extensions/account-tools-cli/blob/master/docs/cli-mode.md),
[env-specific-variables.md](https://github.com/wso2-extensions/account-tools-cli/blob/master/docs/env-specific-variables.md),
[resource-propagation.md](https://github.com/wso2-extensions/account-tools-cli/blob/master/docs/resource-propagation.md).

**Server configuration as code.** `<IS_HOME>/repository/conf/deployment.toml`
accepts `$env{ENV_VAR}` and `$sys{system.property}` placeholders, for example
`[super_admin] password="$env{ENV_VAR}"`, and `$secret{alias}` values
encrypted by `./ciphertool.sh -Dconfigure -Dsymmetric` from a `[secrets]`
block. Sources: [env vars](https://is.docs.wso2.com/en/7.1.0/deploy/security/set-passwords-using-environment-variables-or-system-properties/),
[cipher tool](https://is.docs.wso2.com/en/7.1.0/deploy/security/encrypt-passwords-with-cipher-tool/).

**Terraform.** Only [`wso2-attic/terraform-is`](https://github.com/wso2-attic/terraform-is)
was found: archived Azure infrastructure scripts for IS 5.10, not a resource
provider. No provider for IS 7 configuration was found.

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| REST APIs (`/api/server/v1/...`) | Basic; OAuth2 bearer (password, client credentials, `internal_*` scopes); mTLS | caller's choice | none |
| `iamctl` | client credentials of an M2M app | `serverConfig.json` (secret in file) or env vars | prose only ([resource-propagation.md](https://github.com/wso2-extensions/account-tools-cli/blob/master/docs/resource-propagation.md)) |
| `deployment.toml` | n/a | `$env{}`, `$secret{}` | none |

### 1.2 WSO2 Account Platform (Asgardeo)

The docs home states Asgardeo is now branded WSO2 Account Platform
([home](https://wso2.com/asgardeo/docs/)). No CLI, Terraform provider, or
"config management" tool for the hosted service was found in the docs or
under the `wso2` and `asgardeo` GitHub organizations.

**Management API access.** Create an OIDC or **M2M application**, authorize
API resources under **API Authorization**, then:

```
curl https://api.asgardeo.io/t/{organization_name}/oauth2/token \
  -H "Authorization: Basic Base64(<clientid>:<client-secret>)" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=client_credentials" \
  --data-urlencode "scope=internal_application_mgt_view"
```

The sample response is an opaque-looking token (`decc891e-...`), `expires_in`
3600. Organization (sub-org) APIs "require an additional token exchange
step". Sources: [API authentication](https://wso2.com/account-platform/docs/apis/),
[grant types](https://wso2.com/asgardeo/docs/references/grant-types/),
[M2M app](https://wso2.com/asgardeo/docs/guides/applications/register-machine-to-machine-app/),
[organization API access](https://wso2.com/asgardeo/docs/apis/organization-apis/authentication/).

Whether `iamctl` works against a tenant of the hosted service is not
confirmed; the README lists only Identity Server versions.

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| Management REST APIs | client credentials (M2M app); authorization code for user tokens | caller's choice | none found |

### 1.3 ThunderID (v1.0.x)

**Server configuration.** `deployment.yaml` in the install directory
overrides defaults. Relevant keys: `server.security.trusted_issuer`
(`issuer`, `jwks_url`, `audience`, `required_claims`),
`server.security.direct_auth_secret`, `declarative_resources`, `crypto.keys[]`.
The shipped file reads
`direct_auth_secret: "file://config/secrets/direct_auth_secret"` with the
comment "Gates the Direct API endpoints ... Callers present it in the
Direct-Auth-Secret header." Sources: [configuration](https://thunderid.dev/docs/v1.0.x/deployment/configuration/),
[deployment.yaml v1.0.1](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/cmd/server/deployment.yaml).

**Bootstrap.** Default admin `admin`; the password is generated by
`setup.sh`, or supplied with `./start.sh --bootstrap-and-serve
--admin-password <password>` or the `ADMIN_PASSWORD` env var. On Kubernetes
it is read from secret `thunderid-admin-credentials`. The Helm chart uses
`passwordRef.key` for external secrets, `deployment.secretEnv` for extra
secret-sourced env vars, and warns that plaintext passwords in `values.yaml`
are "NOT recommended for production". Sources: [get ThunderID](https://thunderid.dev/docs/v1.0.x/getting-started/get-thunderid/),
[Helm README](https://github.com/thunder-id/thunderid/blob/v1.0.1/install/helm/README.md).

**Declarative resources.** Resources are YAML documents in REST camelCase.
Two paths: file-backed resources loaded from `config/resources` at startup,
and `POST /import` at runtime with `{content, variables, dryRun, options:
{upsert, continueOnError, target}}`; variables use `{{.VARIABLE_NAME}}`.
The import API requires "an access token with the **system** scope".
`POST /export` returns the same YAML in JSON. Sources: [import/export](https://thunderid.dev/docs/v1.0.x/guides/declarative-configurations/what-is-import-and-export/),
[import-resources.mdx](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/declarative-configurations/import-resources.mdx),
[resource-export.mdx](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/resource-export.mdx).

**Machine client.** Register via console or `POST /oauth2/dcr/register`
with `"grant_types": ["client_credentials"]`, then:

```
curl -X POST https://thunderid.example.com/oauth2/token \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d "grant_type=client_credentials" \
  -d "scope=reservations:create" \
  -d "resource=https://api.example.com/booking"
```

`resource` is mandatory unless `defaultResourceServer` is configured; scopes
are filtered by the client's roles and groups at that resource server. Agents
use the same endpoint with a client secret or private-key JWT, and the
token-exchange grant for on-behalf-of. Sources: [client credentials](https://thunderid.dev/docs/v1.0.x/guides/protocols/oauth-oidc/client-credentials/),
[agent-authentication.mdx](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/agents/agent-authentication.mdx),
[tokens](https://thunderid.dev/docs/v1.0.x/key-concepts/tokens/).

**CI in the product's own repository.** Two composite GitHub Actions ship
in the repo but are not product documentation:

- `.github/actions/obtain-admin-token`: scripts authorization code + PKCE
  against the `CONSOLE` client with `scope=system`, submitting the admin
  username and password to `/flow/execute`, then `/oauth2/auth/callback`
  and `/oauth2/token`.
- `.github/actions/import-declarative-config`: `POST $SERVER_URL/import`
  with `Authorization: Bearer $ADMIN_TOKEN`, `options.upsert=true`, and a
  `variables-json` input; fails on any `summary.failed`.

The workflow resolves the admin password "Secret > Variable > defaults.env".
Whether a client-credentials token can carry the `system` scope needed by
`/import` is not confirmed in the docs; the project's own CI uses a user
login. Sources: [obtain-admin-token](https://github.com/thunder-id/thunderid/blob/v1.0.1/.github/actions/obtain-admin-token/action.yml),
[import-declarative-config](https://github.com/thunder-id/thunderid/blob/v1.0.1/.github/actions/import-declarative-config/action.yml),
[pr-builder.yml](https://github.com/thunder-id/thunderid/blob/v1.0.1/.github/workflows/pr-builder.yml).

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| REST APIs incl. `/import`, `/export` | bearer with `system` scope; external JWT via `trusted_issuer` | caller's choice | repo-internal actions ([link](https://github.com/thunder-id/thunderid/tree/v1.0.1/.github/actions)) |
| `deployment.yaml` + `config/resources` | n/a | `file://` secrets, Helm `passwordRef` | none |
| OAuth clients / agents | client credentials, private-key JWT, token exchange | caller's choice | none |

## 2. API Manager 4.x and the API Platform

### 2.1 `apictl` (API Manager 4.7.0 docs)

**Login.** From the command source, examples are
`apictl login dev -u admin -p admin`, `apictl login dev -u admin`
(prompt), `cat ~/.mypassword | apictl login dev -u admin` with
`--password-stdin`, and `apictl login dev --token <token>` where the flag is
described as "Personal access token". `--password` prints "Warning: Using
--password in CLI is not secure. Use --password-stdin". No env-var
credential path exists in `login.go`.
Source: [cmd/login.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/cmd/login.go).

**Client registration and grant.** On username/password login apictl calls
`client-registration/v0.17/register` with HTTP Basic (the user's
credentials), registers a `password refresh_token` client, then runs the
password grant with a fixed `apim:*` scope list. Source:
[utils/constants.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/utils/constants.go)
(`defaultClientRegistrationEndpointSuffix`), and the earlier survey in
[wso2-authentication-landscape.md](wso2-authentication-landscape.md) §4.

**Storage.** Config dir `.wso2apictl` (`main_config.yaml`, environments);
credentials in `keys.json` under `.wso2apictl.local`, fields `username`,
`password`, `clientId`, `clientSecret`, `accessToken`, each base64-encoded;
the store prints `WARNING: credentials are stored as a plain text in %s`.
Sources: [credentials.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/credentials.go),
[jsonstore.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/jsonstore.go),
[constants.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/utils/constants.go).

**Environments.** `apictl add env dev --apim https://localhost:9443`, or
the four endpoints `--registration`, `--admin`, `--publisher`,
`--devportal`; optional `--token` endpoint; `--mi` for Micro Integrator.
`add-env` and `export-api` forms are deprecated since 4.0.0.
Source: [getting started, 4.7.0](https://apim.docs.wso2.com/en/4.7.0/apiops/cli/getting-started-with-wso2-api-controller/).
The docs' description of `--token` as a personal access token could not be
rendered by the fetcher on the 4.7.0 page; it is quoted in the earlier
survey from the `latest` page.

**Kubernetes mode.** `apictl k8s ...` replaces the deprecated `--mode`
flag; it drives the cluster, not the API Manager REST API. Source:
[getting started, 4.0.0](https://apim.docs.wso2.com/en/4.0.0/install-and-setup/setup/api-controller/getting-started-with-wso2-api-controller/).
A 4.7.0 page for `apictl k8s` was not located; not re-verified.

**Published CI/CD.**

- [CI/CD using the CLI, 4.7.0](https://apim.docs.wso2.com/en/4.7.0/apiops/cli/cicd-using-cli/):
  `apictl login dev -u admin -p admin`, `apictl export api -e dev -n
  SwaggerPetstore -v 1.0.0 --provider admin --latest`, `apictl init ...
  --oas`, `apictl gen deployment-dir -s ... -d ...`, `apictl vcs`.
- [Jenkins pipeline, 4.7.0](https://apim.docs.wso2.com/en/4.7.0/apiops/cli/building-jenkins-ci-cd-pipeline/):
  global env vars `APIM_DEV_HOST`, `APIM_PROD_HOST`, `ARTIFACTORY_*`;
  the linked Jenkinsfile gist runs
  `apictl add env dev --apim https://${APIM_DEV_HOST}:9443 -k` and
  `apictl login dev -u admin -p admin -k` with the password inline.
  Sample repos: [poc-cicd-source-repo](https://github.com/chamilaadhi/poc-cicd-source-repo),
  [poc-cicd-deployment-repo](https://github.com/chamilaadhi/poc-cicd-deployment-repo),
  [gist](https://gist.github.com/chamilaadhi/81241bf2e9c46b720ef61fb516e00249).
- [GitOps blog, 2025-09-02](https://wso2.com/library/blogs/how-to-build-a-ci-cd-pipeline-for-apis-using-wso2-api-manager-gitops/):
  GitHub Actions with secrets `WSO2_DEV_APIM`, `APIM_USERNAME`,
  `APIM_PASSWORD`, `apictl import-api` to dev, Argo CD for promotion. The
  workflow YAML itself is described, not printed.

**Publisher REST API directly.** The OpenAPI description tells callers to
register a DCR client with `"grantType":"client_credentials password
refresh_token"` using `Authorization: Basic Base64(admin_username:
admin_password)` at `/client-registration/v0.17/register`, then obtain a
token with `grant_type=password&username=admin&password=admin&scope=apim:
api_view apim:api_create`. Which user roles satisfy each `apim:*` scope is
tenant scope-role mapping; not re-verified here.
Source: [publisher-api.yaml](https://github.com/wso2/carbon-apimgt/blob/master/components/apimgt/org.wso2.carbon.apimgt.rest.api.publisher.v1/src/main/resources/publisher-api.yaml).

**External IdP for portals.** The 4.7.0 SSO guide registers an app at IS
with callback `https://localhost:9443/commonauth`, maps IdP groups to
`Internal/publisher` / `Internal/Subscriber` and claim `groups` to
`http://wso2.org/claims/role`. It covers portal login only; it says
nothing about apictl or REST tokens. Source:
[IS as external IdP, 4.7.0](https://apim.docs.wso2.com/en/4.7.0/install-and-setup/setup/sso/configuring-identity-server-as-external-idp-using-oidc/).

**Server config.** `deployment.toml` supports the same `$env{}`,
`$sys{}`, `$secret{}` mechanism as Identity Server. Source:
[set passwords using env vars](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/security/logins-and-passwords/set-passwords-using-vars-and-sys-props/).

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| `apictl` | username/password via DCR + password grant; `--token` bearer | `keys.json`, base64, plaintext warning | yes: Jenkins doc, GitOps blog (above) |
| Publisher/Admin REST | DCR (Basic admin creds) + password or client credentials, `apim:*` scopes | caller's choice | none beyond apictl docs |
| `deployment.toml` | n/a | `$env{}`, `$secret{}` | none |

### 2.2 API Platform / Platform Gateway 1.x

The docs site versions the gateway at 1.0.0, 1.1.0, 1.2.0 plus `next`
([overview](https://wso2.com/api-platform/docs/)). Page bodies on that site
did not render for the fetcher; quotes below come from the same content in
the source repository.

**`ap` CLI.** `ap gateway add --display-name <name> --server <server>
[--platform <platform>] [--auth <none|basic|bearer>]`; for `basic` export
`WSO2AP_GW_USERNAME` and `WSO2AP_GW_PASSWORD`, for `bearer` export
`WSO2AP_GW_TOKEN` ("env vars override config"). `ap devportal add ...
--auth <basic|oauth|api-key> [--username] [--password] [--token]
[--api-key] [--no-interactive]` with `WSO2AP_DEVPORTAL_USERNAME`,
`_PASSWORD`, `_TOKEN`, `_API_KEY`. The CLI executes no OAuth grant; `oauth`
means a pre-obtained bearer. Config lives in `~/.wso2ap/config.yaml`,
plaintext, per the earlier survey. Sources:
[docs/cli/reference.md](https://github.com/wso2/api-platform/blob/main/docs/cli/reference.md),
[wso2-authentication-landscape.md](wso2-authentication-landscape.md) §6.

**Gateway Controller management API.** Either local users or an IdP:

```yaml
auth:
  basic:
    users:
      - password: "$bcrypt$..."
        password_hashed: true
        roles: ["admin"]
  idp:
    jwks_url: "https://idp.example.com/oauth2/jwks"
    issuer: "https://idp.example.com/oauth2/token"
    roles_claim: "groups"
```

If `roles_claim` is set, `role_mapping` is mandatory; if unset,
"authorization is bypassed"; if both `basic.enabled` and `idp.enabled` are
false, "all requests ... are allowed without authentication". Audience is
not part of the documented config. Source:
[rest-apis/gateway/authentication.md](https://github.com/wso2/api-platform/blob/main/docs/rest-apis/gateway/authentication.md).

**Platform API (control plane).** Modes `internal_token`, `file`, `idp`.
File mode issues RS256 JWTs from a login endpoint and requires
`APIP_CP_ADMIN_USERNAME` and `APIP_CP_ADMIN_PASSWORD_HASH`; IdP mode needs
`[platform_api.auth.idp].jwks_url` and `.issuer` and lists Thunder,
Asgardeo, Keycloak, Azure AD, Okta. Source:
[platform-api/README.md](https://github.com/wso2/api-platform/blob/main/platform-api/README.md).

**CI/CD pages.** The site lists "AI Workspace > CI/CD > Overview /
Configure CI/CD workflow" and "Cloud > Administer > Manage CD pipelines";
their bodies could not be retrieved, so what they prescribe is not
confirmed. `https://wso2.com/bijira/docs/` served the API Platform
documentation set; whether that is a formal redirect is not confirmed.

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| `ap` | basic, bearer, api-key (bring your own) | `~/.wso2ap/config.yaml` or `WSO2AP_*` env vars | none found |
| Gateway Controller API | basic users; external JWT via JWKS + role mapping | server YAML | none found |
| Platform API | file users; internal token; external IdP JWT | server TOML / env vars | not confirmed |

## 3. Agent Manager (docs v1.0.0-rc3)

WSO2 Agent Manager has a public repository, a docs site, prebuilt `amctl`
binaries, and a cloud console. Sources: [repo](https://github.com/wso2/agent-manager),
[docs](https://wso2.github.io/agent-manager/),
[releases](https://github.com/wso2/agent-manager/releases).

**`amctl login`.** Opens a browser for authorization code by default;
"For CI or scripts, use the OAuth client-credentials grant by passing both
`--client-id` and `--client-secret`":

```
amctl login --url http://api.amp.localhost:8080 --client-id <client-id> --client-secret <client-secret>
```

Flags: `--url` (required), `--name` (default `default`), `--client-id`
(default `amctl`), `--client-secret`, `--auth-server` (skips metadata
discovery), `--org`, `--json`. Sessions are stored in
`~/.amctl/config.yaml`. Fresh installs use `admin`/`admin`. No env-var
input for the secret is documented. Sources:
[CLI installation](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/guides/cli-installation/),
[login reference](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/reference/cli/login/),
[CLI overview](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/reference/cli/overview/).

**Authorization model.** "Access is governed by OAuth 2.0 scopes issued by
the bundled Thunder identity provider"; scopes are `amp:<resource>:<action>`
(`amp:project:read`, `amp:agent:build`, `amp:agent:env-production`); roles
Agent Manager Admin, Developer, AI Lead, Platform Engineer. A scope
`amp:org:manage-service-account` exists, but no page describes creating a
service account or where a CI job's client ID and secret come from.
Per-environment ThunderID instances issue agent credentials, separate from
the Thunder used for console and API login. Sources:
[authorization](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/reference/authorization/),
[AgentID](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/concepts/agentid/).

**CI story.** The deployment-pipeline concept defines environment order and
the scopes promotion needs; it does not mention external CI. `--json`
output "with stable error codes" is the only automation affordance named.
No GitHub Actions or Jenkins sample was found. Source:
[deployment pipeline](https://wso2.github.io/agent-manager/docs/v1.0.0-rc3/concepts/deployment-pipeline/).

| Tool | Auth methods | Credential storage | CI sample |
| --- | --- | --- | --- |
| `amctl` | authorization code + PKCE; client credentials via flags | `~/.amctl/config.yaml` (tokens, secret) | none found |
| REST / MCP servers | bearer from bundled Thunder, `amp:*` scopes | caller's choice | none found |

## 4. Cross-product stories WSO2 publishes

- **Developer Platform (Choreo) CLI.** The console issues personal access
  tokens (changelog 2024-11-22) and the CLI consumes them non-interactively:
  `echo "$CHOREO_TOKEN" | wdp login --with-token`. The docs recommend a
  secrets manager and name CI/CD as the use case. The older `choreo-cli`
  README documents only `choreo login`. Sources:
  [PAT guide](https://wso2.com/choreo/docs/choreo-cli/manage-authentication-with-personal-access-tokens/),
  [changelog](https://wso2.com/choreo/changelog),
  [choreo-cli README](https://github.com/wso2/choreo-cli/blob/main/README.md).
- **Shared IdP for management tokens.** Each product can trust an external
  issuer for its own management surface: Thunder `trusted_issuer`, Gateway
  Controller `idp.jwks_url`, Platform API `idp` mode, API Manager portal
  federation. None of the pages describes one token serving two products;
  audience handling differs (Thunder enforces `aud`, the Gateway Controller
  doc has no audience key). The earlier
  [single-login setup research](2026-09-06-single-login-product-setup.md)
  covers the per-product trust setup.
- **Asgardeo + API Manager.** Documented only as portal SSO (§2.1); no
  guidance on machine tokens for `apictl` or the REST APIs from the shared
  IdP was found.
- No WSO2 page was found that describes one credential or one tool driving
  Identity Server, API Manager and Agent Manager together from a pipeline.

## 5. Assessment for the wso2 cli

The shell model, per [architecture §4.6](../architecture.md) and the
2026-09-06 design "one login, one session per product, acquired through
shared sign-on":
one account per login provider; one product record per product on that
account; in CI, client credentials minted per product (with a `resource`
indicator on Thunder) or a jwt-bearer derivation to a product with its own
issuer; the client secret read from a named environment variable and never
written to configuration.

### Today versus the shell, per product

| Product | CI user today | With the shell | Remaining gap |
| --- | --- | --- | --- |
| Identity Server 7.x | M2M app; `iamctl` with `CLIENT_SECRET` in `serverConfig.json` or env; or raw REST with Basic admin creds | one M2M client per IS record; secret from a named env var; `internal_*` scopes on the record | `iamctl`'s export/import format and keyword replacement are not in the shell; both would run side by side unless an IS module wraps `exportAll`/`importAll` (proposal) |
| Account Platform (Asgardeo) | M2M app, hand-written `curl` to `/t/{org}/oauth2/token`; no CLI | same as IS, tenant URL on the record | organization APIs need the extra exchange step; the shell has no organization-switch design yet |
| ThunderID | admin user login scripted through `/flow/execute` (as the project's own CI does) or a DCR client with `resource`; `POST /import` needs `system` scope | client credentials with `resource` set to Thunder's own API and `system` scope requested | whether a client-credentials token can carry `system` for `/import` is not confirmed; if not, CI would still need the user-login script |
| API Manager 4.x | `apictl login -u -p` (password grant, base64 file) or `--token` from a hand-run DCR + token call; Jenkins/Actions samples inline the admin password | jwt-bearer derivation from the login provider, or client credentials at APIM's token endpoint | a machine client must still map to `Internal/publisher`-class roles for `apim:*` scopes; the role-to-scope path for a client-credentials subject is not documented; apictl's export/import project format stays in apictl |
| Platform Gateway 1.x | `ap` with `WSO2AP_GW_TOKEN` or basic env vars; token minted elsewhere | client credentials at the configured IdP; bearer presented to the controller | controller config has no audience key, so a token for one product could be replayed to another unless the IdP scopes differ; `ap`'s project workflow is not in the shell |
| Agent Manager | `amctl login --client-id --client-secret` on the command line; `~/.amctl/config.yaml` holds the secret | client credentials at the bundled Thunder with `amp:*` scopes | how to create the service-account client is undocumented; the bundled Thunder's `resource` identifier for the Agent Manager API is not published |
| Developer Platform | console PAT piped to `wdp login --with-token` | PAT as compatibility adapter only | no derivation or narrowing from a PAT |

### Observations

- Every current tool takes the secret on the command line or in a file;
  only `iamctl` and `ap` read env vars. The shell's env-var-only rule is
  stricter than any product tool today.
- No product tool other than `amctl` and the shell design discovers
  endpoints; `iamctl`, `apictl` and `ap` take URLs by hand.
- The published API Manager samples are the only WSO2 pipeline samples that
  show a login; both inline `admin`/`admin`.

### Proposals

- Proposal: an IS module command that shells out to `iamctl exportAll` /
  `importAll` with the shell's token would need `iamctl` to accept a bearer
  token; it accepts only client ID and secret today.
- Proposal: ask ThunderID whether `system` may be granted to a
  client-credentials client bound to Thunder's own resource server; the
  answer decides whether Thunder CI needs the flow-API script at all.
- Proposal: ask Agent Manager for a documented service-account procedure
  and the resource identifier its API expects, before designing its module.
