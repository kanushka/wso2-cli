# WSO2 authentication landscape

**Status:** Research
**Research date:** 2026-08-02
**Scope:** OAuth2/OIDC capabilities of Asgardeo, WSO2 Identity Server 7.x, and
Thunder (ThunderID), plus the authentication implementations of the existing
`apictl`, `amctl`, and `ap` product CLIs, evaluated against the login methods planned
in [architecture §4.6](../architecture.md) and
[product requirements §7.2](../product-requirements.md).
**Source policy:** Only public primary sources are used: official product
documentation, public GitHub source code, live first-party API metadata, and
IETF specifications. Secondary write-ups are not relied on. Claims that could
not be traced to a primary source are marked "unknown from public sources".

## Purpose

The planned `wso2` CLI login surface is: browser Authorization Code + PKCE
(`wso2 login`), Device Authorization Grant (`wso2 login --device-code`),
personal access token, and client credentials, with CI restricted to the
non-interactive methods. This document establishes, from public evidence only,
which of these each WSO2 identity backend and each existing product CLI
supports today, and where the planned design would not work without a
fallback.

A note on documentation sources: `wso2.com/asgardeo/docs` is now branded
"WSO2 Identity Platform" and much of its content is generated from the shared
[`wso2/docs-is`](https://github.com/wso2/docs-is) repository, which templates
pages per product (`WSO2 Identity Platform` vs `WSO2 Identity Server` with
version conditions). Where a claim comes from a shared page, this document
says so.

## 1. Asgardeo (WSO2 SaaS identity)

### Grant types

The platform grant-type reference documents Authorization Code, Authorization
Code + PKCE, Refresh Token, Client Credentials, Implicit (discouraged),
Password (discouraged), Device Authorization Grant (RFC 8628), plus custom and
advanced grants: Organization Switch, Token Exchange (RFC 8693), CIBA, and JWT
Bearer. [Grant types reference](https://wso2.com/asgardeo/docs/references/grant-types/)

The live OIDC discovery document of the documentation's example organization
(`bifrost`) — a first-party API response — confirms the advertised set
(retrieved 2026-08-02 from
`https://api.asgardeo.io/t/bifrost/oauth2/token/.well-known/openid-configuration`):

- `grant_types_supported` includes `authorization_code`, `refresh_token`,
  `client_credentials`, `urn:ietf:params:oauth:grant-type:device_code`,
  `urn:ietf:params:oauth:grant-type:token-exchange`,
  `urn:ietf:params:oauth:grant-type:jwt-bearer`, `organization_switch`,
  `password`, `urn:openid:params:grant-type:ciba`, and several
  Asgardeo-internal grants (`asg_api`, `system_app_grant`, `account_switch`).
- `device_authorization_endpoint` is present:
  `https://api.asgardeo.io/t/{org}/oauth2/device_authorize`.
- `code_challenge_methods_supported` is `["S256", "plain"]`.

So all four planned mechanisms that map to OAuth grants — auth code + PKCE,
device code, client credentials, refresh — are advertised by the server.
Token exchange (RFC 8693) is supported and has a configuration guide.
[Token exchange guide](https://wso2.com/asgardeo/docs/guides/authentication/configure-token-exchange/)

### Public clients and app types

Registerable application types are single-page, traditional web, mobile,
M2M, and standard-based (protocol-from-scratch) applications.
[Application guides](https://wso2.com/asgardeo/docs/guides/applications/)

Mobile applications are the documented public-client path: "Mobile
applications, by design, cannot maintain any secrets. These kinds of
applications are called public clients", and the docs recommend the
"Authorization Code Flow for public clients with the PKCE … extension".
[Mobile app login guide](https://wso2.com/asgardeo/docs/guides/authentication/add-login-to-mobile-app/)
A CLI can therefore register as a public client (mobile or standard-based
template with secretless auth), but there is no CLI-specific template.

The OIDC app-settings reference defines a public client as "an application
which cannot securely store client credentials", offers a PKCE
**Mandatory** option, and requires `redirect_uri` to match registered
authorized redirect URLs. Whether `http://127.0.0.1:{port}` loopback redirects
(RFC 8252 §7.3 style, with variable port) are accepted for the hosted service
is **unknown from public sources** — no page found either permits or forbids
loopback redirect registration.
[OIDC settings reference](https://wso2.com/asgardeo/docs/references/app-settings/oidc-settings-for-app/)

M2M applications get a client ID/secret and "obtain an M2M token using client
credential grant"; API scopes are granted via the application's API
Authorization tab.
[Register M2M app](https://wso2.com/asgardeo/docs/guides/applications/register-machine-to-machine-app/)

### Organization model and audience/scope restriction

Asgardeo has root organizations with B2B sub-organizations; applications are
shared into sub-organizations, whose users can then log in to them.
[Organization docs](https://wso2.com/asgardeo/docs/guides/organization-management/)
A token is issued against one organization: to act in a sub-organization, the
client first obtains a root-organization token, then exchanges it at the same
token endpoint with `grant_type=organization_switch`,
`switching_organization=<org-id>`, and the requested scopes.
[Organization API authentication](https://wso2.com/asgardeo/docs/apis/organization-apis/authentication/)

Audience and scope restriction per API is first-class: an API resource is
registered with an identifier that "will be used as the `aud` attribute in the
issued JWT token", permissions are modeled as scopes, and RBAC decides which
scopes a user's token may carry.
[API authorization guide](https://wso2.com/asgardeo/docs/guides/authorization/api-authorization/api-authorization/)

### Discovery, token lifetimes, refresh behavior

Discovery is standard OIDC, tenant-qualified:
`https://api.asgardeo.io/t/{org}/oauth2/token/.well-known/openid-configuration`,
with issuer `https://api.asgardeo.io/t/{org}/oauth2/token` and a DCR endpoint
at `/t/{org}/api/identity/oauth2/dcr/v1.0/register`.
[Discover OIDC configs](https://wso2.com/asgardeo/docs/guides/authentication/oidc/discover-oidc-configs/)

Token behavior (shared platform docs, applicable to both Asgardeo and IS):

- Access tokens are opaque or JWT; the default user access token expiry is
  3600 seconds.
  [Access token settings source](https://github.com/wso2/docs-is/blob/master/en/includes/guides/fragments/manage-app/oidc-settings/access-token.md)
- Refresh token default expiry is 86400 seconds. Rotation is **opt-in**: "By
  default, whenever the refresh token is exchanged for a new access token,
  [the product] issues the same refresh token back"; enabling **Renew refresh
  token** invalidates the old token on each exchange.
  [Refresh token settings source](https://github.com/wso2/docs-is/blob/master/en/includes/guides/fragments/manage-app/oidc-settings/refresh-token.md),
  [rendered reference](https://wso2.com/asgardeo/docs/references/app-settings/oidc-settings-for-app/)
- With rotation on, **Graceful refresh token rotation** keeps the previous
  refresh token usable for a short replay window (platform maxima: 60-second
  window, 5 reuses) so clients can recover a lost rotation response.
  [Refresh tokens reference source](https://github.com/wso2/docs-is/blob/master/en/includes/references/tokens/refresh-tokens.md)
- Expiry is lifetime-based; no idle-expiry mechanism was found in the public
  docs (**unknown from public sources** beyond the fixed validity period).

### PAT or equivalent; app-native authentication

No personal-access-token or long-lived API-key feature for user automation was
found in the Asgardeo documentation; the documented automation path is an M2M
application with client credentials (see M2M guide above). PAT support:
**not found in public sources**.

Asgardeo has an app-native authentication API: the client calls `/authorize`
with `response_mode=direct` and drives the login steps over REST, with Google-
and Apple-provided client attestation "where client authentication is not
possible".
[App-native authentication reference](https://wso2.com/asgardeo/docs/references/app-native-authentication/),
[App-native authentication API](https://wso2.com/asgardeo/docs/apis/app-native-authentication-api/)
This could in principle enable an in-terminal (no browser) username/password
login, but it is designed and documented for mobile apps with store
attestation, not CLIs.

## 2. WSO2 Identity Server (on-premises)

### Grants and public clients

IS documents the same grant catalogue as the platform docs: Authorization
Code (+PKCE), Client Credentials, Refresh Token, Implicit, Password, Device
Authorization (RFC 8628), Token Exchange (RFC 8693), JWT Bearer, SAML2 Bearer,
CIBA, Organization Switch.
[IS grant types](https://is.docs.wso2.com/en/latest/references/grant-types/)

- Device grant: "available by default"; tunables live in `deployment.toml`
  under `[oauth.grant_type.device_code]` (defaults: user-code key length 6,
  expiry 10m, polling interval 5s).
  [Device flow guide (7.0)](https://is.docs.wso2.com/en/7.0.0/guides/authentication/oidc/implement-device-flow/),
  [latest](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-device-flow/)
  Device flow guides exist back to IS 5.11/5.12 and 6.x, so this is not new in
  7.x. [6.1 guide](https://is.docs.wso2.com/en/6.1.0/guides/access-delegation/try-device-flow/),
  [5.12 guide](https://is.docs.wso2.com/en/5.12.0/learn/try-device-flow/)
- Public clients: applications can enable "Allow authentication without the
  client secret" plus a **PKCE Mandatory** option; the docs recommend code +
  PKCE for public clients.
  [OIDC settings (7.0)](https://is.docs.wso2.com/en/7.0.0/references/app-settings/oidc-settings-for-app/),
  [PKCE guide](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-auth-code-with-pkce/),
  [public clients guide (6.1)](https://is.docs.wso2.com/en/6.1.0/guides/login/oidc-auth-code-pkce-public-clients/)

### Discovery, lifetimes, refresh

IS exposes standard OIDC discovery per tenant; the docs provide a
discover-OIDC-configs guide analogous to Asgardeo's.
[Discover OIDC endpoints (7.0)](https://is.docs.wso2.com/en/7.0.0/guides/authentication/oidc/discover-oidc-configs/)
Token defaults and refresh rotation semantics are the shared platform
behavior cited in §1: access token 3600s, refresh token 86400s, rotation
opt-in via **Renew refresh token**; graceful rotation is gated to IS versions
newer than 7.2.0 in the docs source.
[Refresh token settings source](https://github.com/wso2/docs-is/blob/master/en/includes/guides/fragments/manage-app/oidc-settings/refresh-token.md)

### PAT or equivalent

No personal access token feature was found in the IS documentation. PAT
support: **not found in public sources**. Automation is client credentials
(M2M application template in 7.x) or password grant.

### 7.x vs 6.x differences that matter for a CLI

- **IS 7.0** replaced the Carbon console with the new Console and app
  templates (SPA, web, mobile, **M2M**), introduced **API resources with
  RBAC-validated scopes** (audience-restricted JWTs like Asgardeo), added
  **app-native authentication**, and added PAR, JARM, and FAPI compliance.
  [About IS 7.0](https://is.docs.wso2.com/en/7.0.0/get-started/about-this-release/)
- **IS 7.3** (current "latest" docs) adds, among others, "App-native
  authentication for device authorization grant", CIBA grant, token exchange
  for organization applications, and graceful refresh token rotation.
  [About this release (latest = 7.3.0)](https://is.docs.wso2.com/en/latest/get-started/about-this-release/)
- 6.x supports the CLI-relevant basics (auth code + PKCE, device grant,
  client credentials, refresh) but lacks the 7.x API-resource/RBAC audience
  model and app-native authentication; a CLI targeting 6.x cannot rely on
  per-API `aud` restriction the way it can on 7.x.

## 3. Thunder (ThunderID)

### What it is and maturity

The authoritative repository is
[`thunder-id/thunderid`](https://github.com/thunder-id/thunderid)
(GitHub redirects the earlier `asgardeo/thunder` name to it). It describes
itself as "a lightweight, open-source Identity and Access Management (IAM)
engine built to secure access for humans, AI agents, and machines", written in
Go, cloud-native, with declarative YAML/GitOps configuration and an immutable
runtime.
[README](https://github.com/thunder-id/thunderid/blob/main/README.md)
The repository was created in May 2025; releases progressed through v0.4x to
**v1.0.0-alpha (2026-07-21) and v1.0.0-alpha2 (2026-07-28)** — i.e. it is
pre-1.0, alpha-stage software as of this research date.
[Releases](https://github.com/thunder-id/thunderid/releases)
Product docs live at `thunderid.dev` (source in `docs/` of the repo).

### OAuth2/OIDC flows supported today

The protocol documentation lists exactly these grant types: Authorization
Code (RFC 6749 §4.1, "PKCE is required for public clients"), Client
Credentials, Refresh Token, Token Exchange (RFC 8693), and Backchannel
Authentication (CIBA Core 1.0).
[OAuth/OIDC protocol index](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/protocols/oauth-oidc/index.mdx)
The implementation matches: the grant-handler provider registers handlers for
client credentials, authorization code, refresh token, token exchange, CIBA,
and JWT bearer — and nothing else. There is **no device authorization grant
handler and no password grant handler** in the source tree.
[granthandlers/provider.go](https://github.com/thunder-id/thunderid/blob/main/backend/internal/oauth/oauth2/granthandlers/provider.go),
[granthandlers/ directory](https://github.com/thunder-id/thunderid/tree/main/backend/internal/oauth/oauth2/granthandlers)

Supporting surface (from the same protocol index and the
[`backend/internal/oauth/oauth2/`](https://github.com/thunder-id/thunderid/tree/main/backend/internal/oauth/oauth2)
packages): client auth methods `client_secret_basic`, `client_secret_post`,
`private_key_jwt`, and `none` (public clients); PKCE (RFC 7636); PAR
(RFC 9126); DPoP (RFC 9449); issuer identification (RFC 9207); **Resource
Indicators (RFC 8707)** for audience-targeted tokens; token introspection
(RFC 7662); revocation; dynamic client registration; and OAuth server
metadata / OIDC discovery. Allowed grant types are configurable server-wide
via an allow list (`OAuth.AllowedGrantTypes`).

Thunder also exposes a native flow-execution ("journey") API — login defined
as orchestrated journeys executable by the application rather than
browser-redirect-only — plus agent identities as first-class OAuth clients.
[README features](https://github.com/thunder-id/thunderid/blob/main/README.md)

### Roadmap and differences from IS

No public roadmap document covering a device grant was found, and no open
issue requesting it was found in the tracker; device-grant plans are
**unknown from public sources**. The API surface is not IS-compatible: it is
a new Go codebase with its own REST APIs (users, applications, agents, OUs,
flows, resources), YAML resource definitions, and different management
endpoints — nothing in the public docs claims IS API compatibility.

## 4. apictl (WSO2 API Manager CLI)

Source: [`wso2/product-apim-tooling`, `import-export-cli/`](https://github.com/wso2/product-apim-tooling/tree/master/import-export-cli).

- **Login methods.** `apictl login <env>` takes username/password (flags,
  prompt, or `--password-stdin`) or `--token <personal access token>`.
  [cmd/login.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/cmd/login.go)
- **DCR + password grant.** On first username/password login, apictl calls
  APIM's DCR endpoint (default suffix `client-registration/v0.17/register`)
  with HTTP **Basic auth using the user's username/password**, registering a
  client with `"grantType":"password refresh_token"`; it then obtains tokens
  from `oauth2/token` with `grant_type=password` and a hard-coded list of
  `apim:*` scopes.
  [utils/tokenManagement.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/utils/tokenManagement.go)
  (`GetClientIDSecret`, `GetOAuthTokens`),
  [utils/constants.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/utils/constants.go)
  (`defaultClientRegistrationEndpointSuffix`, `defaultTokenEndPoint`,
  `GrantTypesToBeSupported = ["refresh_token", "password", "client_credentials"]`)
- **Storage: plaintext, not keyring.** Credentials (username, password,
  client ID, client secret, PAT) are stored **base64-encoded in a JSON file**
  (`keys.json`), and the store itself prints
  `WARNING: credentials are stored as a plain text in %s`. The `Store`
  interface has a `credStore` field hook, but the only implementation in the
  credentials package is the JSON store; no OS-keyring implementation exists.
  [credentials/jsonstore.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/jsonstore.go),
  [credentials/credentials.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/credentials.go),
  [credentials/store.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/store.go)
- **Token use.** Each invocation re-runs the password grant (or uses the
  stored PAT verbatim); logout revokes the token at `oauth2/revoke`.
  [credentials/credentials.go](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/credentials/credentials.go)
  (`GetOAuthAccessToken`, `RevokeAccessToken`)
- **Non-interactive mode.** Flags and `--password-stdin`/`--token` only; no
  environment-variable credential path was found in the login code.
- **"PAT" meaning.** APIM's apictl docs describe the `--token` value as a
  personal access token generated with the required scopes — i.e. a manually
  obtained OAuth access token, not a distinct product feature with its own
  lifecycle UI.
  [apictl getting started](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/api-controller/getting-started-with-wso2-api-controller/)
- **No browser or device flow** exists anywhere in the login path.

## 5. amctl (WSO2 Agent Manager CLI)

Source: [`wso2/agent-manager`, `cli/`](https://github.com/wso2/agent-manager/tree/main/cli).

- **Login methods.** `amctl login --url <instance>` runs **Authorization Code
  + PKCE in the browser by default**; passing `--client-secret` (with
  `--client-id`) switches to **client credentials**. Default interactive
  client ID is the pre-registered public client `"amctl"`.
  [pkg/cmd/login.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/cmd/login.go),
  [pkg/auth/login.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/auth/login.go)
- **PKCE implementation.** Loopback redirect `http://127.0.0.1:10325/callback`
  on a local listener, S256 challenge, random `state` verification, HTML
  success/error pages, browser opened via `pkg/browser`.
  [pkg/auth/pkce.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/auth/pkce.go),
  [pkg/browser/browser.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/browser/browser.go)
- **Discovery.** amctl resolves the authorization server from the resource:
  it fetches `/.well-known/oauth-protected-resource` (RFC 9728 protected
  resource metadata) from the instance URL, takes the first
  `authorization_servers` entry, then fetches
  `/.well-known/oauth-authorization-server` (RFC 8414) for the endpoints.
  `--auth-server` skips discovery.
  [pkg/clients/discovery.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/clients/discovery.go)
- **Storage: plaintext YAML.** Access token, refresh token, and (for client
  credentials) the client secret are written to `~/.amctl/config` (0600 file,
  0700 dir) as YAML. No OS keyring use exists in the repository.
  [pkg/config/config.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/config/config.go)
  (`AuthConfig`, `DefaultPath`, `Save`)
- **No device flow, no PAT.** No device-code code path exists in the CLI
  source.
- **Backend identity provider.** The Agent Manager service itself provisions
  and reconciles **Thunder** instances (models and reconcilers named
  `thunder_instance`, `agent_thunder_client`, `agent_thunder_reconciler`),
  so amctl's authorization server in a standard deployment is Thunder.
  [agent-manager-service/models/thunder_instance.go](https://github.com/wso2/agent-manager/blob/main/agent-manager-service/models/thunder_instance.go),
  [agent-manager-service/services/agent_thunder_reconciler.go](https://github.com/wso2/agent-manager/blob/main/agent-manager-service/services/agent_thunder_reconciler.go)
  This is consistent with amctl's absence of a device flow: its backend does
  not offer one.

## 6. ap (WSO2 API Platform CLI, next-generation APIM)

Source: [`wso2/api-platform`, `cli/`](https://github.com/wso2/api-platform/tree/main/cli)
(monorepo of the next-generation API Platform: gateway, portals, and a
`platform-api` control plane).

### Login and auth commands

There is **no `ap login` command and no OAuth flow implementation in the CLI**.
Instead, each target kind is registered into local config with a static auth
method:

- `ap gateway add --auth none|basic|bearer` — basic username/password or a
  user-supplied bearer token for the gateway-controller admin API.
  [cmd/gateway/add.go](https://github.com/wso2/api-platform/blob/main/cli/src/cmd/gateway/add.go)
- `ap devportal add --auth basic|oauth|api-key` — where `oauth` does **not**
  trigger any grant: it stores or reads a pre-obtained token and sends it as
  `Authorization: Bearer <token>`; `api-key` sends a portal API-key header.
  [cmd/devportal/add.go](https://github.com/wso2/api-platform/blob/main/cli/src/cmd/devportal/add.go),
  [internal/devportal/client.go](https://github.com/wso2/api-platform/blob/main/cli/src/internal/devportal/client.go)
- `ap aiworkspace add` follows the same basic/oauth/api-key pattern.
  [cmd/aiworkspace/add.go](https://github.com/wso2/api-platform/blob/main/cli/src/cmd/aiworkspace/add.go)

So today's `ap` is bring-your-own-token: no browser PKCE, no device code, no
client-credentials execution, no DCR, and no password grant. The auth-type
vocabulary (`none`, `basic`, `bearer`, `oauth`, `api-key`) is defined in
[utils/constants.go](https://github.com/wso2/api-platform/blob/main/cli/src/utils/constants.go).

### Discovery, storage, provisioning, CI

- **Discovery:** none. Server URLs are supplied by the user; there is no OIDC
  or RFC 9728 discovery in the CLI source.
- **Storage:** plaintext YAML at `~/.wso2ap/config.yaml` (dir 0700, file
  0600); `AuthConfig` persists `username`, `password`, `token`, and `apiKey`
  verbatim, and the command explicitly warns "Credentials will be stored in
  plaintext in the configuration file (mode 0600)". No keyring. No refresh
  tokens exist because no grant is ever executed.
  [internal/config/config.go](https://github.com/wso2/api-platform/blob/main/cli/src/internal/config/config.go),
  [cmd/gateway/add.go](https://github.com/wso2/api-platform/blob/main/cli/src/cmd/gateway/add.go)
- **Client provisioning:** not applicable — the CLI is never an OAuth client.
- **Non-interactive/CI:** first-class. Credentials can be omitted from config
  entirely and supplied via environment variables that override stored values
  at runtime: `WSO2AP_GW_USERNAME`/`WSO2AP_GW_PASSWORD`/`WSO2AP_GW_TOKEN`,
  `WSO2AP_DEVPORTAL_*`, `WSO2AP_AIWORKSPACE_*`; `--no-interactive` suppresses
  prompts.
  [utils/constants.go](https://github.com/wso2/api-platform/blob/main/cli/src/utils/constants.go)

### What backs it: the platform accepts IdP-issued tokens

The important finding is on the server side. The new control plane
(`platform-api`) has three authentication modes: `file` (local users, a login
endpoint issuing RS256-signed local JWTs, explicitly "not recommended for
production; please configure an IDP of your choice"), `internal_token`
(platform-internal signed tokens), and **IdP mode — "obtain a token from your
identity provider"**.
[platform-api/README.md](https://github.com/wso2/api-platform/blob/main/platform-api/README.md),
[platform-api/internal/server/server.go](https://github.com/wso2/api-platform/blob/main/platform-api/internal/server/server.go)

In IdP mode, requests are validated by a generic JWT authenticator configured
with an issuer URL and JWKS URL (its code even carries an Asgardeo-specific
JWKS validation workaround), followed by OpenAPI-driven scope enforcement and
resolution of the IdP's organization claim to the platform organization.
[common/authenticators/jwt_authenticator.go](https://github.com/wso2/api-platform/blob/main/common/authenticators/jwt_authenticator.go),
[server.go](https://github.com/wso2/api-platform/blob/main/platform-api/internal/server/server.go)

Both WSO2 IdPs are exercised in the monorepo: the API Portal has a production
**Asgardeo** setup guide (one shared app, per-devportal-org Asgardeo
sub-organizations, `org_id` claim verification on every request) and
**ThunderID** login end-to-end tests; the AI Workspace BFF performs standard
OIDC issuer discovery.
[asgardeo-setup.md](https://github.com/wso2/api-platform/blob/main/portals/api-portal/docs/administer/asgardeo-setup.md),
[thunderid-login.cy.js](https://github.com/wso2/api-platform/blob/main/portals/api-portal/it/ui/cypress/e2e/auth/thunderid-login.cy.js),
[ai-workspace bff oidc.go](https://github.com/wso2/api-platform/blob/main/portals/ai-workspace/bff/internal/auth/oidc.go)

Net: unlike legacy APIM, **the next-generation APIM management plane accepts
IdP-issued JWTs** (Asgardeo or Thunder, or any OIDC issuer with a JWKS
endpoint). What is missing is the client side — `ap` performs no OAuth flow,
so obtaining that token is currently the user's problem.

## 7. Gap analysis

Legend: **S** supported · **P** partially supported · **U** unsupported ·
**?** unknown from public sources.

| Planned wso2-cli method | Asgardeo | IS 7.x | Thunder | Legacy APIM mgmt plane (apictl's backend) | API Platform (ap's backend, platform-api) | Agent Manager (amctl's backend, Thunder) |
|---|---|---|---|---|---|---|
| Browser Authorization Code + PKCE | **S** — grant + PKCE documented and advertised in live metadata; loopback-redirect registration unverified ([grants](https://wso2.com/asgardeo/docs/references/grant-types/)) | **S** — public client + PKCE-mandatory app settings ([guide](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-auth-code-with-pkce/)) | **S** — auth code with PKCE required for public clients ([docs](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/protocols/oauth-oidc/index.mdx)) | **U/?** — no documented browser login for the Publisher/Admin REST APIs; apictl has none | **P** — platform-api in IdP mode validates JWTs from the deployment's OIDC issuer, so a PKCE-obtained token works as bearer; the `ap` CLI itself implements no flow ([server.go](https://github.com/wso2/api-platform/blob/main/platform-api/internal/server/server.go)) | **S** — implemented in amctl today ([pkce.go](https://github.com/wso2/agent-manager/blob/main/cli/pkg/auth/pkce.go)) |
| Device Authorization Grant (RFC 8628) | **S** — `device_authorization_endpoint` live; grant documented ([grants](https://wso2.com/asgardeo/docs/references/grant-types/)) | **S** — enabled by default, configurable ([guide](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-device-flow/)) | **U** — no grant handler in source ([granthandlers](https://github.com/thunder-id/thunderid/tree/main/backend/internal/oauth/oauth2/granthandlers)) | **?** — APIM's key manager heritage includes a device grant for API consumers, but nothing public shows the management REST APIs used with it | **P/?** — inherits the configured IdP's capability (Asgardeo/IS yes, Thunder no); `ap` has no device path | **U** — backend is Thunder; amctl has no device path |
| Personal access token | **U** — no PAT feature found; M2M client credentials is the automation path | **U** — no PAT feature found | **U** — no PAT feature found | **P** — `apictl login --token` accepts a manually generated access token blessed as a "personal access token" by APIM docs ([apictl docs](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/api-controller/getting-started-with-wso2-api-controller/)) | **P** — bring-your-own bearer token is `ap`'s native model, and the DevPortal accepts API keys as an auth type ([client.go](https://github.com/wso2/api-platform/blob/main/cli/src/internal/devportal/client.go)); no user-lifecycle PAT feature found | **U** — not implemented |
| Client credentials | **S** — M2M applications ([guide](https://wso2.com/asgardeo/docs/guides/applications/register-machine-to-machine-app/)) | **S** — grant + M2M template ([grants](https://is.docs.wso2.com/en/latest/references/grant-types/)) | **S** — grant handler present ([source](https://github.com/thunder-id/thunderid/blob/main/backend/internal/oauth/oauth2/granthandlers/client_credentials.go)) | **P** — APIM token endpoint supports it and apictl lists it in `GrantTypesToBeSupported`, but apictl's own flow registers password-grant clients | **P** — via the configured IdP (e.g. an M2M app), then presented as bearer; the CLI does not execute the grant | **S** — implemented in amctl today |
| Refresh token grant (broker dependency) | **S** — default 86400s, rotation opt-in | **S** — same shared behavior | **S** — handler present, rotation with token-family tracking in source | **S** — DCR clients registered with `refresh_token` | **P** — IdP-side capability; `ap` holds only static tokens and never refreshes | **S** — refresh token persisted by amctl |

### Where the planned design would not work today

1. **`wso2 login --device-code` fails against Thunder-backed deployments**
   (including Agent Manager). Thunder has no device grant. Fallback: browser
   PKCE where a browser exists; client credentials for automation. Thunder's
   CIBA support is the closest headless-interactive analogue but is a
   different protocol and not in the planned method list.
2. **PAT as a first-class login method has no identity-provider backing
   anywhere.** None of Asgardeo, IS, or Thunder expose a PAT feature in
   public sources. The only "PAT" in the WSO2 estate today is APIM's
   convention of hand-generating a scoped access token for `apictl --token`.
   A context with `Auth: pat` is therefore only meaningful where the target
   product itself accepts long-lived product tokens; against the three IdPs
   the CI fallback is client credentials.
3. **The APIM password-grant gap is a legacy-only concern.** Legacy APIM's
   management plane cannot be logged into with any planned method: apictl
   authenticates via DCR (basic-auth with user credentials) plus the password
   grant — a mechanism the planned design deliberately excludes — and no
   public documentation shows the Publisher or Admin REST APIs accepting an
   Asgardeo-, IS-, or Thunder-issued token directly (**unknown from public
   sources**). The workable fallbacks there are the PAT-style pre-generated
   token or an APIM-side change. The **next-generation API Platform removes
   this gap on the server side**: platform-api's IdP mode validates JWTs from
   any configured OIDC issuer via JWKS, with Asgardeo and ThunderID both
   exercised in the monorepo (§6). What remains missing is the client side —
   `ap` today has no login flow at all, so the planned broker would be
   *adding* the missing piece rather than fighting an incompatible one.
4. **No universal pre-registered CLI client exists.** amctl assumes a client
   named `amctl` is registered in the backend; Asgardeo/IS require an app to
   be registered per organization/tenant (or created via DCR, which itself
   requires credentials). The planned design needs a client-provisioning
   story per backend before browser/device login can work at all.
5. **Loopback redirect registration on Asgardeo is unverified.** amctl-style
   fixed-port loopback redirects presumably require registering the exact
   `http://127.0.0.1:<port>/callback` URL; whether Asgardeo permits loopback
   HTTP redirect URIs (and variable ports per RFC 8252 §7.3) is unknown from
   public sources and needs empirical verification. Device flow is the
   fallback if loopback registration is restricted.

### What these systems require that the planned design does not yet cover

- **Password grant / basic-auth legacy (apictl, and `mi` similarly).**
  Migrating apictl users requires either a password-grant escape hatch in a
  product module (contradicting §7.2) or acceptance that APIM contexts use
  PAT/client-credentials only until APIM accepts IdP tokens.
- **Organization switching.** Acting in an Asgardeo/IS B2B sub-organization
  requires the `organization_switch` token exchange after login; the broker
  must be able to perform per-context org switches, not just refreshes.
- **Refresh-token rotation handling.** Rotation is opt-in per app; when
  enabled, the broker must atomically persist the replacement refresh token
  and can rely on at most a 60-second/5-reuse graceful window on the hosted
  platform.
- **Two discovery styles.** Asgardeo/IS are OIDC-discovery-first
  (tenant-qualified `.well-known/openid-configuration`); amctl demonstrates
  resource-first discovery (RFC 9728 → RFC 8414). A broker that only does
  OIDC discovery cannot resolve an Agent-Manager-style resource URL.

## Design implications

Implications only; decisions belong to the architecture and product
requirements documents.

- **The planned method set is directionally right but unevenly available.**
  Browser PKCE and client credentials are the only two methods supported by
  all three identity backends today. Device code is Asgardeo/IS-only; PAT is
  backed by no IdP and only by APIM convention. The context `Auth` field's
  legal values are justified by evidence as: `oauth-browser` (auth code +
  PKCE), `oauth-device` (valid only where the backend advertises
  `device_authorization_endpoint` / the grant), `client-credentials`, and
  `pat` (valid only for products that accept product-issued long-lived
  tokens, currently APIM). Evidence does not justify a shared/implicit
  default — availability must be resolved per context, which matches the
  §4.6 rule that on-prem methods are explicitly configured.
- **Capability detection should come from server metadata, not assumptions.**
  `grant_types_supported` and `device_authorization_endpoint` in the
  discovery document are reliable, live signals (verified against Asgardeo);
  the broker can refuse `--device-code` with a stable error when the
  advertisement is absent — exactly the failure mode Thunder produces today.
- **The broker needs both discovery styles**: OIDC issuer discovery for
  Asgardeo/IS contexts and RFC 9728 protected-resource → RFC 8414 resolution
  for resource-URL-first products (Agent Manager pattern).
- **Rotation-safe refresh handling is mandatory**, with atomic single-writer
  persistence of rotated refresh tokens; the hosted platform's graceful
  window (≤60 s, ≤5 reuses) is a recovery aid, not a correctness guarantee.
- **Audience/scope restriction is achievable on every backend** — API
  resources with `aud`-carrying JWTs and RBAC scopes on Asgardeo/IS 7.x, and
  RFC 8707 resource indicators on Thunder — so the broker's
  audience/scope-narrowing contract in §4.6 has real enforcement points, but
  not on IS 6.x, where narrowing degrades to scope selection only.
- **B2B operations imply an org-switch step in the broker**: a root-org
  refresh chain plus on-demand `organization_switch` exchanges per
  sub-organization, rather than one credential per organization.
- **A client-provisioning story is a prerequisite for interactive login**:
  either WSO2-published well-known public clients per backend (the amctl
  model), per-tenant registration during `wso2 context create`, or DCR; the
  secure store and broker design cannot assume a client ID simply exists.
- **The OS-secure-store decision is a genuine improvement, not parity**:
  all three existing WSO2 CLIs persist secrets (passwords, client secrets,
  static bearer tokens, refresh tokens) in plaintext files — apictl and `ap`
  each print an explicit plaintext warning; nothing in the existing tooling
  constrains the planned keychain design.
- **The APIM stance splits by generation.** For the next-generation API
  Platform, the planned design fits: platform-api accepts IdP-issued JWTs, so
  the broker can obtain tokens from Asgardeo/IS/Thunder with browser PKCE or
  client credentials and present them as bearer tokens — the wso2-cli would
  supply the login capability `ap` currently lacks (its env-var token model
  also validates the planned CI stdin/env secret sourcing). Only a **legacy
  APIM module** needs an explicit stance on the password-grant gap (accept
  PAT/client-credentials-only, or depend on an APIM-side change that is
  unknown from public sources today).
