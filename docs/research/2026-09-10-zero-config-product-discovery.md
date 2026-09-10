# Zero-config product discovery from one issuer and one login

**Status:** Research
**Research date:** 2026-09-10
**Scope:** Whether `wso2 login --url <issuer>` (or plain `wso2 login` for WSO2
Cloud) could discover every product a deployment offers — endpoint, token
audience, and trust — from one issuer URL and one browser sign-on, with no
per-product `connect` step. Products in scope: ThunderID, WSO2 Identity
Server 8 ("IS 8"), WSO2 API Platform (`platform-api`, gateway controller,
Envoy gateway), WSO2 Agent Manager, OpenChoreo, and WSO2 Cloud.
**Source policy:** Only public primary sources are used: official product
documentation, public GitHub source code (cited with file path and, where
useful, the tag or commit the repository was cloned at), and IETF RFC text.
A block of facts was measured directly against running instances on
2026-09-09/10 and is marked "measured" below with no further citation;
everything else not traceable to a primary source is marked "unverified"
with the reason.

## Terms used throughout

- **Issuer** — the OAuth/OIDC authorization server's identity, a URL that
  appears in every token it signs (the `iss` claim) and that a client
  combines with `/.well-known/...` paths to find that server's metadata.
- **Audience** — the intended recipient of a token (the `aud` claim); a
  resource server should refuse a token whose audience is not itself.
- **Resource server** — the API that accepts a bearer token and checks it,
  as distinct from the authorization server that issued it.
- **JWKS (JSON Web Key Set)** — the JSON document of public keys a resource
  server fetches (usually from a `jwks_uri`) to verify a JWT's signature.
- **Scope** — the permissions a token carries, requested at authorization
  time and (on some servers) narrowable later.
- **Token exchange (RFC 8693)** — swapping one OAuth token for another, e.g.
  to narrow scope or change audience, without a fresh interactive login.
- **DCR, Dynamic Client Registration (RFC 7591)** — an API letting a client
  register itself as an OAuth client at runtime, instead of an administrator
  configuring it by hand in advance.
- **Protected Resource Metadata (RFC 9728)** — a JSON document, conventionally
  at `/.well-known/oauth-protected-resource`, that tells a client which
  issuer(s) and scopes a resource server requires, so the client does not
  need to be told the issuer out of band.
- **Authorization Server Metadata (RFC 8414)** — the analogous document for
  an authorization server itself, conventionally at
  `/.well-known/oauth-authorization-server`, naming its endpoints and
  capabilities (OIDC's `/.well-known/openid-configuration` is the
  OpenID-Connect-flavored predecessor of the same idea).
- **`WWW-Authenticate` with `resource_metadata`** — RFC 9728 also lets a
  resource server name its own metadata document in the `WWW-Authenticate`
  header of a 401 response, so a client that guessed wrong can recover
  without being told anything in advance.

## Result

**Mostly no, with two clear exceptions on the "find my own issuer" half of
the problem, and no exceptions at all on the "find every other product"
half.** OpenChoreo and WSO2 Agent Manager are the two products in scope
whose *server* — not just a CLI that happens to know the product's own
conventions — publishes RFC 9728 Protected Resource Metadata at
`/.well-known/oauth-protected-resource`, naming the issuer(s) it trusts.
OpenChoreo's version goes furthest: its document also names its own CLI's
registered client ID, and the CLI (`occ`) reads that document and chains
into the issuer's RFC 8414/OIDC discovery document with no issuer or client
ID configured by hand — a complete, working instance of the flow this
document was asked to evaluate, for one product talking to its own issuer.
Agent Manager's server serves the same RFC 9728 document and additionally
sends a `WWW-Authenticate: resource_metadata=...` header on 401s (on both
its REST API and its MCP surface), which is the recovery path RFC 9728
defines for a client that did not know to ask — but it does not itself
serve RFC 8414, so the second discovery hop lands on Thunder, an external
server, not on Agent Manager itself.

Nothing else in scope reaches even that: ThunderID serves RFC 9728 metadata
only when a resource server built on top of it (such as an MCP server)
explicitly turns it on — not for ThunderID's own general REST/OAuth API —
and sends no `WWW-Authenticate: resource_metadata=...` on its main surface.
WSO2 API Platform's `platform-api`, gateway controller, and gateway publish
no RFC 9728 document and send no such header on a 401 (measured). And two
of the six products in scope turned out not to have a settled identity to
research at the protocol level at all: no GA or announced "WSO2 Identity
Server 8" exists as of this date (the current Identity Server line stops at
7.3.0, and WSO2's new Go-based identity work ships under the separate
ThunderID brand instead), and no dated official page defines "WSO2 Cloud"
as a single bundled offering with one login surface — only a live,
undocumented console was found.

Going the other direction — one issuer URL naming every *other* product a
deployment offers, the part of the question that would let `wso2 login`
populate a whole product list from one URL — has no exception anywhere in
this survey. None of ThunderID, Agent Manager, or OpenChoreo's control
plane serves a document listing "these are the other resource servers this
deployment trusts you for." Each resource server names its own trusted
issuer; nothing publishes the reverse map.

Single sign-on *itself* — one browser round trip, multiple products
readable afterwards, once someone has told each product which issuer to
trust — is closer to real than product enumeration is, but it is still
by-hand configuration, not client-side discovery. It already works,
measured, between ThunderID and WSO2 API Platform's `platform-api`/gateway
(`[platform_api.auth] mode = "idp"` plus `jwks_url`/`issuer`, and the
equivalent on the gateway controller and the gateway's `jwt-auth` policy);
between an external OIDC issuer and OpenChoreo's control plane (proved
pluggable, not just configurable, by a test overlay that swaps in a
third-party IdP with no code change); and, for Agent Manager, on the data
plane specifically — any org can add "an Auth0 tenant or Microsoft Entra
ID" as a gateway identity provider via the same discover-from-URL pattern,
though Agent Manager's own console/CLI login is Thunder-only by default and
by documentation.

The practical implication: `wso2 login --url <issuer>` can plausibly do a
browser sign-on and hold one token, and if pointed at an OpenChoreo or
Agent Manager deployment it could discover that one product's own issuer
(and, for OpenChoreo, its own client ID) without being told them — but it
cannot, for any product in scope, discover which *other* products exist,
their endpoints, or their audiences from one issuer URL alone. A CLI still
needs each product's endpoint and audience recorded by hand (or by a
product-specific bootstrap/admin API, where one exists), even after single
sign-on to one product is solved.

## ThunderID

Repository: [`thunder-id/thunderid`](https://github.com/thunder-id/thunderid),
read at commit `b3f4c434e4c2d15913f9f6daa4529c32f98169d2`.

| Capability | Verdict | Source |
| --- | --- | --- |
| RFC 8414 Authorization Server Metadata (`/.well-known/oauth-authorization-server`) and OIDC discovery (`/.well-known/openid-configuration`) | Supported | Registered routes in [`backend/internal/oauth/oauth2/discovery/init.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/oauth/oauth2/discovery/init.go#L35-L47) |
| RFC 9728 Protected Resource Metadata (`/.well-known/oauth-protected-resource`) | Supported for MCP only, not the general REST API | Mounted only when the MCP (Model Context Protocol) server is initialized with a `resourceMeta` value ([`backend/internal/system/mcp/init.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/system/mcp/init.go#L44-L113)); there is no protected-resource-metadata endpoint for the main OAuth/REST surface or for arbitrary resource servers registered in ThunderID |
| `WWW-Authenticate` with `resource_metadata` on 401 | Not supported on the main REST/OAuth surface; unverified for MCP | The main security middleware's 401 challenge is a plain `Bearer` or `Bearer error="invalid_token", ...` with no `resource_metadata` parameter ([`backend/internal/system/security/middleware.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/system/security/middleware.go#L45-L63)). The MCP path passes a `ResourceMetadataURL` into a third-party SDK (`modelcontextprotocol/go-sdk` v1.6.1) whose own 401-header behavior lives outside this repository, so whether MCP responses actually carry the header is unverified from ThunderID's own source |
| RFC 8693 token exchange | Supported (measured) | Measured 2026-09-09/10: exchange keeps OIDC scopes but drops resource-server permissions; `resource=` sets `aud` correctly, `audience=` incorrectly sets `aud` to the client ID |
| RFC 7591 DCR, anonymous | Not supported (measured and confirmed in source) | Measured: `/oauth2/dcr/register` refuses anonymous registration. Source confirms there is no intermediate tier: `checkDCRAuthorization` requires the caller to hold the same root/"system" permission gating every other admin action ([`backend/internal/oauth/oauth2/dcr/handler.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/oauth/oauth2/dcr/handler.go#L64-L73), [`permissions.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/system/security/permissions.go#L321-L330)); RFC 7591's lighter-weight "initial access token" concept does not exist anywhere in the repository (grepped `InitialAccessToken`, `registration_access_token`, `registration_client_uri`, `software_statement` — zero hits). DCR can only be turned fully open by an operator setting `cfg.OAuth.DCR.Insecure` (default `false`) |
| RFC 7591 DCR, with admin token | Supported (measured) | Measured: succeeds with an admin token, issuing a public client |
| Seeded well-known public client | Supported (measured), exact shape confirmed in source | Bootstrap data: [`backend/cmd/server/bootstrap/01-default-resources.yaml`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/cmd/server/bootstrap/01-default-resources.yaml#L5008-L5046) — `clientId: CONSOLE`, `publicClient: true`, `tokenEndpointAuthMethod: none`, `pkceRequired: true`, `grantTypes: [authorization_code, refresh_token]`, redirect URIs templated as `{{.PUBLIC_URL}}/console` plus any `{{.CONSOLE_REDIRECT_URIS}}` |
| Non-admin "my apps" / "my resource servers" enumeration | Not supported | No route resembling `/users/{id}/apps` or `/me/applications` exists. `/applications` and `/resource-servers` carry no entry in the API permission table, and any path not explicitly listed falls back to requiring the root/admin permission by default ([`backend/internal/system/security/service.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/system/security/service.go#L151-L168)); only `GET`/`PUT /users/me` are self-service. No JWT claim carries an authorized-app or authorized-resource-server list either (grepped `AuthorizedApps`, `allowed_apps`, `my_apps` — no hits) |
| Organization/role claim mapping | Supported, but the unit is "Organization Unit" (OU), not "organization" | `ouId`/`ouName`/`ouHandle` claims come from a hierarchical Organization Unit tree, and a separate `roles` claim comes from group membership plus direct role assignment — both opt-in per OAuth client ([`backend/internal/oauth/oauth2/tokenservice/utils.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/oauth/oauth2/tokenservice/utils.go#L553-L698), constants in [`constants.go`](https://github.com/thunder-id/thunderid/blob/b3f4c434e4c2d15913f9f6daa4529c32f98169d2/backend/internal/oauth/oauth2/constants/constants.go#L259-L335)); there is no claim literally named "organization" |

**On the "is ThunderID the same thing as IS 8" question:** it is not, and
more fundamentally there is no product publicly called "WSO2 Identity
Server 8" as of this research date. WSO2's own site introduces ThunderID as
"WSO2's next-generation identity core, built from the ground up in Go,"
named alongside — not as a version of — "Identity Server"
([Meet ThunderID](https://wso2.com/library/conference/2026/05/meet-thunderid-an-identity-core-for-the-future)),
and the highest published Identity Server version in WSO2's own
version-selector docs is **7.3.0**
([is.docs.wso2.com](https://is.docs.wso2.com/en/latest/get-started/about-this-release/),
checked 2026-09-10). Identity Server and ThunderID are different codebases
(Java/Carbon-based vs. Go-based); no official WSO2 source uses "Identity
Server 8" branding at all. See the next section for what that means for
this document's scope.

## WSO2 Identity Server 8

**"WSO2 Identity Server 8" does not exist, in any primary source, as of this
research date.** The current stable release of the legacy Carbon/Java-based
Identity Server is **7.3.0**
([is.docs.wso2.com/en/versions](https://is.docs.wso2.com/en/versions/)),
and [`wso2/product-is`](https://github.com/wso2/product-is) has no `v8` tag
or branch — its newest in-progress branch is `release-7.4.0`, still the
legacy codebase. WSO2's own press release announcing ThunderID
([2026-05-21](https://wso2.com/about/news/wso2-accelerates-agentic-enterprise-adoption-with-new-agent-identity-forward-deployed-engineers-and-expanded-delivery-partner-ecosystem/))
treats "the WSO2 Identity Platform" (still on IS 7.3) and ThunderID as two
separate, parallel offerings; ThunderID is never described as replacing,
rebranding, or versioning Identity Server, and it is headed to the
OpenWallet Foundation as an independent open-source project rather than
being merged into `product-is`.

Because the premise names a product that has not shipped or been announced,
this section cannot report "IS 8 behavior." The nearest primary-source
analog for what a next-generation, Thunder-lineage identity server would
look like is ThunderID itself, already covered in full in the section
above; nothing here should be read as a claim about IS 8, only as a record
of why that section is empty. One point from that research is worth adding
to the ThunderID picture directly, since it did not fit the discovery-only
framing of the table above:

| Capability | Verdict | Source |
| --- | --- | --- |
| ThunderID federating an external OIDC identity provider (acting as a relying party) | Supported | Per-connection config of client ID/secret, authorization/token/userinfo/JWKS endpoints, issuer, scopes, and a `trustedTokenAudience` field for token exchange — [add-oidc-provider.mdx](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/identity-providers/add-oidc-provider.mdx) |
| ThunderID's own APIs trusting an external issuer (acting as a pure resource server) | Supported | `server.security.trusted_issuer` in `deployment.yaml` (`issuer`, `jwks_url`, `audience`, `required_claims`) — [trusted-issuer.mdx](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/trusted-issuer.mdx) |
| RFC 9728 Protected Resource Metadata and `WWW-Authenticate: resource_metadata=...`, as a documented pattern | Documented as guidance for resource servers built on ThunderID (e.g. an MCP server), not exposed by ThunderID's own core APIs by default | [secure-your-mcp-server.mdx](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/mcp/mcp-server/secure-your-mcp-server.mdx) — consistent with the source-level finding above that the route is only mounted when a resource server (MCP) explicitly initializes it |
| RFC 7591 DCR authentication requirement, per the docs | Documented as "optional and configurable" — "When enabled, the caller must authenticate as a privileged client" | [dynamic-client-registration.mdx](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/protocols/oauth-oidc/dynamic-client-registration.mdx); the measured/source-level finding above is that ThunderID's own default configuration has it enabled, i.e. admin-only, matching this document's "with admin token" row. Full RFC 7592 registration management (retrieving/updating/deleting a client record after registration) is explicitly not supported — DCR here is one-shot |

If a next-generation Identity Server does ship, the open questions this
document could not answer for it are the same ones the ThunderID section
answers today: whether it serves RFC 9728 on its general API surface (not
just for MCP), whether a non-admin user can enumerate their own
applications, and what its seeded public client (if any) is scoped to.

## WSO2 API Platform

Repository: [`wso2/api-platform`](https://github.com/wso2/api-platform).

| Capability | Verdict | Source |
| --- | --- | --- |
| RFC 9728 Protected Resource Metadata | Not supported (measured) | Measured: `platform-api`, the gateway controller, and the gateway publish no `/.well-known/oauth-protected-resource` |
| `WWW-Authenticate` with `resource_metadata` on 401 | Not supported (measured) | Measured: none of the three send a `WWW-Authenticate` header on 401 |
| External IdP trust, `platform-api` | Supported | Measured/[platform-api/README.md](https://github.com/wso2/api-platform/blob/main/platform-api/README.md): `[platform_api.auth] mode = "idp"` with `jwks_url` + `issuer` |
| External IdP trust, gateway controller | Supported | Measured: `[controller.auth.idp]` |
| External IdP trust, gateway data plane | Supported | Measured: a `jwt-auth` policy naming a keymanager |
| Org/role claim mapping | Supported | [wso2-authentication-landscape.md §6](wso2-authentication-landscape.md): resolution of the IdP's organization claim to the platform organization, generic JWT authenticator, OpenAPI-driven scope enforcement |
| RFC 8693 / RFC 7591 at `platform-api` itself | Not applicable | `platform-api` is a resource server/relying party here, not an identity provider; it does not issue or exchange tokens |

## WSO2 Agent Manager

Repository: [`wso2/agent-manager`](https://github.com/wso2/agent-manager),
read at commit `2e1db5d87442986be5088ecd283764460d2b2aa7`. The product is
confirmed as "WSO2 Agent Manager" (internal short name AMP, "Agent
Management Platform," found as a default config value). This section covers
the server (`agent-manager-service`); the `amctl` CLI's own resource-first
discovery behavior was already established in
[wso2-authentication-landscape.md §5](wso2-authentication-landscape.md) and
is not re-derived here. Agent Manager turns out to be the second product in
scope, after OpenChoreo, whose *server* — not just its CLI — publishes real
discovery metadata.

| Capability | Verdict | Source |
| --- | --- | --- |
| RFC 9728 Protected Resource Metadata, served by the server itself | Supported | `GET /.well-known/oauth-protected-resource` and a second, MCP-specific `/.well-known/oauth-protected-resource/mcp`, both registered directly on the service's own router and built from config (`cfg.ServerPublicURL`, `cfg.OAuthAuthorizationServers`, `cfg.OAuthScopesSupported`) — [`agent-manager-service/api/well_known_routes.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/api/well_known_routes.go#L36-L76). This is a genuine positive: `amctl`'s resource-first discovery (landscape §5) is reading a document the product's own control plane serves, not one borrowed from Thunder |
| RFC 8414 Authorization Server Metadata, served by the server itself | Not supported | No `/.well-known/oauth-authorization-server` route exists anywhere in `agent-manager-service` (grepped repo-wide). The RFC 9728 document's `authorization_servers` field instead names an external issuer (Thunder, by default) whose *own* `/.well-known/oauth-authorization-server`/OIDC discovery document `amctl` fetches as the second hop — the two-hop discovery is split across two different servers |
| `WWW-Authenticate` with `resource_metadata` on 401 | Supported | Set on both a missing-bearer-header 401 and an invalid-token 401, with the exact wire format `Bearer realm="agent-manager", resource_metadata="https://.../.well-known/oauth-protected-resource"` (plus `error="invalid_token"` on the second case), and resolved per route so the REST API and the MCP surface each advertise their own metadata URL — [`agent-manager-service/middleware/jwtassertion/auth.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/middleware/jwtassertion/auth.go#L224-L277), asserted in [`auth_test.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/middleware/jwtassertion/auth_test.go#L48-L54). The same pattern recurs at the gateway (data-plane) MCP policies for deployed agent endpoints, so this is a platform-wide convention |
| JWKS-based bearer-token validation | Supported | `KEY_MANAGER_ISSUER`/`KEY_MANAGER_AUDIENCE`/`KEY_MANAGER_JWKS_URL` config keys feed exact-match issuer validation, prefix-tolerant audience validation, and JWKS fetch/cache (1-hour TTL, `kid`-keyed) with RSA-only signature checking — [`agent-manager-service/middleware/jwtassertion/auth.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/middleware/jwtassertion/auth.go#L427-L696); a dev-only bypass skips verification if no JWKS URL is set and `IsLocalDevEnv` is true, otherwise it fails closed |
| Organization claim mapping | Supported | Custom `OuId`/`OuHandle` claims (Thunder's Organization Unit id/handle) are required and injected into request context as the sole source of org identity — "the token is the single source of truth for org identity" — [`agent-manager-service/middleware/authorization.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/middleware/authorization.go#L59-L101) |
| Role claim mapping | Not a JWT claim; roles are flattened to scopes upstream | There is no `roles` field read from the token at all. Authorization is scope-based (`amp:<resource>:<action>` strings in the standard `scope` claim); the product's own docs state that role-to-scope binding happens in Thunder at bootstrap, so a token already carries flattened scopes by the time it reaches the service — [`documentation/docs/reference/authorization.mdx`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/documentation/docs/reference/authorization.mdx) |
| Thunder-only, or any OIDC issuer, for the platform's own console/CLI login | Thunder by default and by documentation, though the config surface is generic | The issuer/audience/JWKS config keys are plain strings, not hard-coded to a Thunder hostname, but Helm ships them pointed at the bundled Thunder and the docs state flatly that "access is governed by OAuth 2.0 scopes issued by the bundled Thunder identity provider"; no code path or doc describes repointing the platform's own login at Asgardeo or Identity Server |
| Thunder-only, or any OIDC issuer, for securing the agents/APIs the platform deploys (data plane) | Explicitly generic — this is a separate, first-class feature | A per-organization "Gateway Identity Provider" can be added by SSRF-protected discovery against *any* issuer's `/.well-known/openid-configuration` — the guide names "an Auth0 tenant or Microsoft Entra ID" as examples — [`agent-manager-service/services/oidc_discovery.go`](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/services/oidc_discovery.go#L41-L79), [configure-identity-providers-at-the-gateway.mdx](https://github.com/wso2/agent-manager/blob/2e1db5d87442986be5088ecd283764460d2b2aa7/agent-manager-service/documentation/docs/guides/configure-identity-providers-at-the-gateway.mdx#L17-L60) |
| RFC 8693 token exchange / RFC 7591 DCR in the service itself | Not supported | Zero hits repo-wide for token-exchange or DCR grant types/routes; the only grant the service itself uses, as a client, is `client_credentials` to fetch its own service-to-service token from Thunder |
| Non-admin "my agents"/"my apps" enumeration | Not supported | Every agent-listing route is org- and project-scoped and gated purely by an RBAC scope (e.g. `amp:agent:read`), not filtered by caller identity — a member with read access sees every agent in the org, not "agents I can reach." No route matching `me`/`self`/`my-*` exists beyond profile self-service |

Agent Manager also confirms, independently of the CLI-side research, that
Thunder issues an **Organization Unit** claim (`OuId`/`OuHandle`), matching
the ThunderID section above rather than a claim literally named
"organization."

## OpenChoreo

Repository: [`openchoreo/openchoreo`](https://github.com/openchoreo/openchoreo),
read at commit `176bd19497fdb1f2d5f92b4e8e5d3905f92fde21`. This is the one
product in scope that already does what the design question asks for, on
both ends: its control-plane API serves RFC 9728 metadata and its own CLI
(`occ`) consumes it, then chains into RFC 8414, with no issuer told to it
out of band.

| Capability | Verdict | Source |
| --- | --- | --- |
| Pluggable external OIDC IdP trust | Supported | Config-driven, not hard-coded to one product: [`internal/openchoreo-api/config/identity.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/openchoreo-api/config/identity.go) (`OIDCConfig{Issuer, JWKSURL, AuthorizationEndpoint, TokenEndpoint}`); proven pluggable, not just configurable, by a test overlay that swaps in Dex, a third-party OIDC provider, with no code change ([`test/ui/k3d/values-cp-ext-idp.yaml`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/test/ui/k3d/values-cp-ext-idp.yaml)). The default/reference deployment IdP is WSO2 Thunder ([`internal/openchoreo-api/config/env.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/openchoreo-api/config/env.go), Helm defaults in [`install/helm/openchoreo-control-plane/values.yaml`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/install/helm/openchoreo-control-plane/values.yaml)) |
| How trust is configured | Supported (config file + env vars, not a CRD) | `identity.oidc.{issuer,jwks_url,authorization_endpoint,token_endpoint}` in [`cmd/openchoreo-api/config.yaml`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/cmd/openchoreo-api/config.yaml), overridable via `OC_API__<SECTION>__<FIELD>` env vars or discrete vars (`JWKS_URL`, `JWT_ISSUER`, `JWT_AUDIENCE`); no `IdentityProvider`-shaped CRD exists in the repo |
| JWKS signature validation | Supported | Full fetch/cache/verify with per-`kid` RSA key lookup and background refresh: [`internal/server/middleware/auth/jwt/jwks.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/server/middleware/auth/jwt/jwks.go), issuer/audience claim checks in [`middleware.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/server/middleware/auth/jwt/middleware.go) |
| Roles/entitlement claim mapping | Supported, configurable claim name | `security.subjects.<type>.mechanisms.jwt.entitlement.claim` picks the claim (default `groups` for users, `sub` for service accounts); matched against `AuthzRoleBinding` CRDs to grant roles ([`api/v1alpha1/types.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/api/v1alpha1/types.go)) |
| Organization claim mapping | Not supported | No "organization" concept exists in the authn/authz code; the resource hierarchy is namespace/project/component, not tenant/organization-scoped. The one `organization` field found (`EndpointStatus.Organization`) is explicitly marked `// TODO: ... This is not being used` |
| RFC 9728 Protected Resource Metadata, served | Supported | `GET /.well-known/oauth-protected-resource` on both the main control-plane API and the observer (telemetry) API, returning `resource`, `authorization_servers`, `scopes_supported`, and an OpenChoreo extension `openchoreo_clients` naming pre-registered client IDs by name (e.g. `cli`) — [`internal/openchoreo-api/api/handlers/oauth_metadata.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/openchoreo-api/api/handlers/oauth_metadata.go) |
| RFC 8414 Authorization Server Metadata | Consumed, not served | OpenChoreo's own CLI fetches `<issuer>/.well-known/openid-configuration` as the second discovery step ([`internal/occ/auth/oidc_config.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/occ/auth/oidc_config.go), which cites RFC 8414 in a code comment); OpenChoreo itself is a resource server, not an authorization server, so it has no metadata of this kind to serve |
| `WWW-Authenticate` with `resource_metadata` on 401 | Supported, on one surface | Implemented for the MCP endpoint specifically via an `Auth401Interceptor` that sets `Bearer resource_metadata="<url>"` on a 401 ([`internal/server/middleware/mcp/auth401.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/server/middleware/mcp/auth401.go)); plain REST 401s outside the MCP surface do not carry it (checked repo-wide for `WWW-Authenticate`, all hits are the MCP path) |
| CLI does zero-config, resource-first discovery | Supported | `occ`'s `FetchOIDCConfig` does exactly the flow this document is asking whether any product supports: GET the control plane's own protected-resource metadata to learn the issuer and the CLI's own registered client ID, then GET the issuer's OIDC discovery document for the token/authorization endpoints — no issuer or client ID configured by hand ([`internal/occ/auth/oidc_config.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/occ/auth/oidc_config.go)); login is then browser Authorization Code + PKCE or client credentials for CI ([`internal/occ/cmd/login/login.go`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/internal/occ/cmd/login/login.go)) |
| RFC 8693 token exchange / RFC 7591 DCR | Not supported | Grepped case-insensitively across all source, docs, and OpenAPI specs for `8693`, `token exchange`, `token_exchange`, `7591`, `dynamic client registration`, `registration_endpoint` — zero matches. OpenChoreo is purely a relying party here: it validates externally issued JWTs and its CLI performs standard PKCE/client-credentials flows against an external authorization server, but never issues, exchanges, or dynamically registers OAuth clients itself. Its "OAuth clients" (e.g. `cli`) are statically pre-registered in `identity.clients` config |

One further finding worth carrying into the design discussion: a design
proposal in the repo
([`docs/proposals/0186-integrate-external-idp.md`](https://github.com/openchoreo/openchoreo/blob/176bd19497fdb1f2d5f92b4e8e5d3905f92fde21/docs/proposals/0186-integrate-external-idp.md))
covers securing user-*deployed* data-plane APIs (not the control plane
itself) through an `APIClass` CRD naming an external IdP; its own
"Non-Goals" section excludes control-plane API security, and no
`APIClass`/`AuthenticationPolicy` type exists in the codebase yet — this is
a proposal, not shipped behavior.

## WSO2 Cloud

There is no dated, official WSO2 page that defines "WSO2 Cloud" as a single
bundled offering combining Identity, API Platform, Agent Manager, and
OpenChoreo, and no `wso2 cloud login`-style command is publicly documented
anywhere. What primary sources do establish: a live web console exists at
`console.cloud.wso2.com` (confirmed 2026-09-10 by a direct HTTPS request;
the page title is "WSO2 Cloud Console" and it serves the `wso2-cloud-logo.svg`
favicon), so the name refers to a real, currently-live surface, but its
scope and GA status are unverified. Each constituent product has its own,
separately dated announcement: API Platform reached general availability
2026-03-31
([wso2.com/about/news/wso2-launches-api-platform](https://wso2.com/about/news/wso2-launches-api-platform/)),
Agent Manager entered beta 2026-05-05 with GA targeted for June 2026
([GlobeNewswire](https://www.globenewswire.com/news-release/2026/05/05/3287760/0/en/wso2-launches-agent-manager-to-bring-identity-governance-and-scale-to-enterprise-ai-agents.html)),
and ThunderID was announced 2026-05-21 as an open-source IAM stack
contributed to the OpenWallet Foundation, alongside references to Identity
Server 7.3 and OpenChoreo 1.1 (CNCF Sandbox) as current
([GlobeNewswire](https://www.globenewswire.com/news-release/2026/05/21/3299496/0/en/wso2-accelerates-agentic-enterprise-adoption-with-new-agent-identity-forward-deployed-engineers-and-expanded-delivery-partner-ecosystem.html)).

| Capability | Verdict | Source |
| --- | --- | --- |
| A defined, single "WSO2 Cloud" bundled product (Identity + API Platform + Agent Manager + OpenChoreo under one login) | Unverified | No dated official page found describing the bundle's scope; only a live, undocumented console at `console.cloud.wso2.com` (checked 2026-09-10) |
| Login issuer is ThunderID-based | Unverified | No official WSO2 doc states this explicitly; only non-primary community write-ups (a Medium post and a dev.to explainer) claim Thunder is "a default identity provider in some WSO2 cloud environments" — not treated as evidence here, cited only to explain why the question remains open |
| Live OIDC/OAuth discovery document at a WSO2 Cloud login domain | Unverified | `https://console.cloud.wso2.com/.well-known/openid-configuration` returned HTTP 200 but the single-page app's HTML shell, not a JSON discovery document (2026-09-10); `cloud.wso2.com`, `login.cloud.wso2.com`, and `auth.cloud.wso2.com` all returned HTTP 403, which is not evidence either way |
| SSO across the bundled products documented as working today | Unverified | No dated official statement found either confirming or denying this for the current product set |
| A published `wso2 cloud login`-equivalent CLI command | Not found | Documented CLI logins are per product: `apictl login` (username/password), `mi remote login`, and the Choreo/WDP CLI's browser link flow; no unified cloud-wide login command is documented |

## What a one-URL discovery flow would need from each product that it does not have today

**ThunderID**

- RFC 9728 Protected Resource Metadata on its general REST/OAuth surface,
  not only when a downstream MCP server opts in — today a CLI pointed at a
  bare ThunderID URL has nothing to fetch that would confirm "this is your
  issuer, here is your client ID."
- A `WWW-Authenticate: resource_metadata=...` header on 401s from the main
  API, mirroring what it already does for MCP-hosted resource servers.
- A "my apps"/"my resource servers" self-service endpoint, or an
  authorized-app claim, so a signed-in non-admin user (not just an
  administrator) can learn what they are allowed to reach — every
  enumeration route today falls back to requiring the root/admin
  permission.
- A registration tier between "wide open" (`DCR.Insecure`) and "requires
  full admin/root permission" — RFC 7591's initial-access-token concept, so
  a CLI could self-register as a public client without an administrator's
  own credential.

**WSO2 Identity Server 8**

- To exist. As of this research date there is nothing to evaluate: the
  premise names a product with no GA release, no announced codebase, and no
  primary-source mention anywhere in WSO2's own documentation or press
  material. If a next-generation Identity Server is planned, the gap list
  is an open question rather than a finding — it inherits whichever of
  ThunderID's gaps (above) it does or does not carry forward.

**WSO2 API Platform**

- RFC 9728 Protected Resource Metadata on `platform-api`, the gateway
  controller, and the gateway's data plane (measured absent on all three),
  so a client could learn the trusted issuer and JWKS URL from the product
  itself instead of being handed `jwks_url`/`issuer` by an administrator.
- A `WWW-Authenticate: resource_metadata=...` header on 401s from any of
  the three surfaces (measured absent on all three).
- Some way for a CLI to learn a product's expected audience without an
  administrator naming it — today the audience is whatever the IdP-side API
  resource/OAuth application was registered with, invisible to the client
  until it is told.

**WSO2 Agent Manager**

- RFC 8414 Authorization Server Metadata (or OIDC discovery) served by
  `agent-manager-service` itself, not only by the external Thunder instance
  its RFC 9728 document points at — today the second discovery hop leaves
  the product entirely, so "one product, one discovery chain" is still two
  servers.
- A documented way to point the platform's own console/CLI login at an
  identity provider other than the bundled Thunder — the per-org gateway
  identity provider feature already proves this pattern works for securing
  *deployed* agents; the platform's own login has no equivalent.
- A "my agents" enumeration scoped to the caller, not just an RBAC scope
  check against the whole org — today anyone with `amp:agent:read` in an
  org sees every agent in it, which answers "what can this role see," not
  "what can this signed-in person see."

**OpenChoreo**

- The one product that already does the client-side half of this document's
  question; what it lacks is the *other* direction and a couple of adjacent
  primitives:
  - No document, anywhere, names the other products/resource servers a
    deployment offers — OpenChoreo's own RFC 9728 metadata describes only
    itself.
  - No organization concept in its claim-mapping model (the one
    `organization` field found in source is marked unused), which matters
    if a one-issuer flow needs to resolve which WSO2 organization or tenant
    a signed-in user belongs to across products.
  - No RFC 8693 token exchange or RFC 7591 DCR — OpenChoreo's OAuth clients
    (like `cli`) are statically pre-registered in config, so a CLI cannot
    self-register the way it can against ThunderID with an admin token.
  - `WWW-Authenticate: resource_metadata=...` is wired for its MCP surface
    only, not its plain REST 401s.

**WSO2 Cloud**

- A dated, official definition of what "WSO2 Cloud" actually is, and
  whether it is one login surface across Identity, API Platform, Agent
  Manager, and OpenChoreo, or a marketing umbrella over products that still
  authenticate separately.
- A publicly reachable discovery document at whatever domain fronts its
  login — the one candidate checked, `console.cloud.wso2.com`, answers
  `/.well-known/openid-configuration` with its single-page app's HTML
  shell, not JSON.
- An explicit statement of which identity provider backs it (ThunderID,
  Asgardeo, or something else), so the rest of this document's findings
  about that provider would actually transfer to the cloud offering.
- A documented `wso2 cloud login`-equivalent, or an equivalent statement
  that the plain `wso2 login` case for WSO2 Cloud is meant to reuse one of
  the existing per-product CLIs' flows.
