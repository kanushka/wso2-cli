# Product authentication compatibility

**Status:** Research
**Research date:** 2026-08-03
**Scope:** Second-round follow-up to the
[WSO2 authentication landscape](wso2-authentication-landscape.md): (1) a
sufficiency-and-gap-ownership verdict for each planned `wso2` login method,
and (2) per-product authentication paths (cloud and on-premises) for every
module candidate in the
[public CLI inventory](public-wso2-cli-inventory.md), with a compatibility
verdict against the planned broker flow
([architecture §4.6](../architecture.md),
[product requirements §7.2](../product-requirements.md)).
**Source policy:** Only public primary sources are used: official product
documentation, public GitHub source code, and live first-party API metadata.
Claims that could not be traced to a primary source are marked "unknown from
public sources". Backend capabilities established in the landscape document
are cross-referenced, not re-derived.

## 1. Method sufficiency and gap ownership

Verdicts: **SUFFICIENT** (works as-is), **BACKEND GAP** (a named WSO2 team
must ship something), **OUR GAP** (the wso2-cli plan must change or add
something).

### 1.1 Browser Authorization Code + PKCE — SUFFICIENT, with one provisioning gap

Works today against Asgardeo, IS 6.x/7.x, and Thunder (landscape §§1–3), and
against every backend that validates IdP-issued JWTs (API Platform's
`platform-api` IdP mode, Agent Manager's Thunder). New evidence this round:

- **Plain-HTTP localhost redirect URIs are registerable on Asgardeo.** The
  official React quickstart registers `http://localhost:5173` as the
  authorized redirect URL, so a fixed-port loopback callback (the amctl
  pattern) is viable on the hosted service. Wildcard/ephemeral ports per
  RFC 8252 §7.3 remain **unknown from public sources**.
  [React quickstart](https://wso2.com/asgardeo/docs/quick-starts/react/)
- **The seeded-public-client pattern is documented practice on the Thunder
  side.** amctl's reference documentation ships a default client ID `amctl`
  for the interactive browser flow, i.e. the platform install seeds the CLI's
  OAuth client.
  [amctl login reference](https://github.com/wso2/agent-manager/blob/main/documentation/docs/reference/cli/login.mdx)

**BACKEND GAP (owner: Asgardeo — `wso2/identity-apps`/service team; IS —
`wso2/product-is`):** no WSO2-published, well-known public client for a CLI
exists in Asgardeo or IS; every tenant/deployment must register its own app.
Evidence of absence: no such client appears in the application guides or the
IS app-configuration references (landscape §§1–2). Until one ships, the
wso2-cli needs per-context client configuration or registration.

**Standing ask, recorded 2026-08-05 — owner: the Asgardeo service team
(`wso2/identity-apps`), and `wso2/product-is` for Identity Server.** The
wso2-cli login slice ships *against* this gap rather than waiting for it to
close: every tenant and every deployment registers its own public client by
hand, and [the login walkthrough](../guides/login.md) §§2–3 is that manual
registration written out in full — a standard-based application, public client,
PKCE mandatory with `S256`, the four loopback callbacks
`http://127.0.0.1:{10425,10426,10427,10428}/callback`, the refresh-token grant,
and an API resource whose identifier becomes the audience.

The ask is a WSO2-seeded, well-known `wso2cli` public client, provisioned in
every Asgardeo organization and every Identity Server deployment with exactly
that shape. What it buys is specific and measurable: the walkthrough's entire
registration half (§§2–3, two of its nine sections) collapses to a default
`clientId` the shell ships, `clientId` stops being a field a first-time user has
to supply in the context document, and the only value that user still provides
is the issuer. Until it ships, those sections stay, and every reported
`auth.discovery_failed` or redirect-mismatch from a hand-registered application
is a cost attributable to this gap.

**OUR GAP:** the plan should pin a fixed loopback callback port (or a small
documented range) rather than assuming ephemeral ports, and treat "loopback
URI not registrable" as a per-deployment condition with device code as the
documented fallback.

### 1.2 Device Authorization Grant — SUFFICIENT on Asgardeo/IS; BACKEND GAP on Thunder

Asgardeo advertises the grant and endpoint in live metadata; IS enables the
grant by default (landscape §§1–2).

**BACKEND GAP (owner: `thunder-id/thunderid`):** Thunder has no device-grant
handler — the grant-handler provider registers only authorization code,
client credentials, refresh, token exchange, CIBA, and JWT bearer
([provider.go](https://github.com/thunder-id/thunderid/blob/main/backend/internal/oauth/oauth2/granthandlers/provider.go)),
the protocol docs list no device grant
([index](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/protocols/oauth-oidc/index.mdx)),
and no tracked issue requesting it was found. Consequence: every
Thunder-backed product (Agent Manager today) cannot serve
`wso2 login --device-code`.

**OUR GAP (optional):** Thunder's CIBA grant is its only decoupled
"approve on another device" flow. If headless-interactive login on
Thunder-backed products matters before Thunder ships a device grant, the plan
would need a CIBA variant — otherwise the documented fallback is browser
PKCE or client credentials.

### 1.3 Personal access token — split verdict: BACKEND GAP at the IdP layer, SUFFICIENT at one product layer

- **IdP layer — BACKEND GAP.** No PAT feature exists in Asgardeo (owner:
  Asgardeo service team), IS (owner: `wso2/product-is`), or Thunder (owner:
  `thunder-id/thunderid`); evidence of absence in landscape §§1–3. An
  IdP-side PAT (user-lifecycle, revocable, scoped) would have to be built.
- **Product layer — SUFFICIENT for Choreo/WDP.** Choreo has a real,
  console-issued PAT feature: created under Account Settings → Personal
  Access Tokens with selectable scopes, consumed by
  `choreo login --with-token`, which "reads the token from the standard
  input" (`echo "$CHOREO_TOKEN" | choreo login --with-token`). Expiry
  policy is not stated.
  [Manage authentication with PATs](https://wso2.com/choreo/docs/platform-engineer/choreo-cli/manage-authentication-with-personal-access-tokens/)
- **Product layer — ADAPTER-grade for legacy APIM** (doc-blessed manual
  token via `apictl login --token`, landscape §4) and **partial for API
  Platform** (DevPortal accepts API keys; bring-your-own bearer everywhere,
  landscape §6).

**OUR GAP:** the plan's PAT method should be explicitly defined as a
*product-issued* credential presented to that product's API — not an
IdP credential — because that is the only form that exists anywhere in the
estate. Choreo's stdin-only intake matches the plan's CI secret-sourcing
rule and validates it.

### 1.4 Client credentials — SUFFICIENT everywhere

Supported by Asgardeo (M2M apps), IS, and Thunder (landscape §§1–3), and
proven in practice against IS by WSO2's own tooling: `iamctl` authenticates
with `grant_type=client_credentials` at `{server}/t/{tenant}/oauth2/token`,
followed where needed by an `organization_switch` exchange
([iamctl setup.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/pkg/utils/setup.go)).
No backend gap. The plan's env/stdin secret sourcing covers CI.

### 1.5 Cross-cutting OUR GAPs (exhaustive)

1. **Audience downscoping for modules is backend-divergent — the biggest
   finding this round.** The broker promise in §4.6 ("restricts audience and
   scope when the deployment supports them") maps onto very different
   primitives:
   - **Thunder: full support.** Its RFC 8693 token exchange accepts its own
     access/refresh/ID tokens as `subject_token`, allows scope narrowing
     ("downscoping is allowed, widening is rejected with `invalid_scope`"),
     and binds the issued token's `aud` to an RFC 8707 `resource` parameter
     ("The issued access token is bound to exactly this resource server";
     the `audience` parameter is accepted but ignored for `aud`).
     [Thunder token exchange](https://github.com/thunder-id/thunderid/blob/main/docs/content/guides/protocols/oauth-oidc/token-exchange.mdx)
   - **Asgardeo/IS: not with RFC 8693.** Both document token exchange
     exclusively as *inbound federation*: the subject token must come from a
     registered **trusted third-party token issuer**
     (`subject_token_type=urn:ietf:params:oauth:token-type:jwt`), and the
     product "only copies the `sub` claim" into the issued token. Nothing
     documents exchanging the product's *own* user token for a
     narrower one, and no `resource` parameter is documented.
     [Asgardeo guide](https://wso2.com/asgardeo/docs/guides/authentication/configure-token-exchange/),
     [IS guide](https://is.docs.wso2.com/en/latest/guides/authentication/configure-token-exchange/)
   - On Asgardeo/IS the remaining levers are scope selection at
     authorization time and (per RFC 6749 §6) possibly a narrower `scope` on
     the refresh grant — whether WSO2 honors refresh-time narrowing is
     **unknown from public sources** and needs empirical verification.
     Audience follows the API-resource authorization model, not a
     per-request parameter (landscape §1).
   The broker therefore needs a per-backend downscoping strategy
   (token-exchange+resource on Thunder; scoped-refresh-or-nothing on
   Asgardeo/IS; none for product-internal tokens), and §4.6's "when the
   deployment supports them" carve-out is doing real work.
2. **Resource-first discovery** (RFC 9728 → RFC 8414) alongside OIDC issuer
   discovery — carried over from landscape §5, still required.
3. **`organization_switch` exchange support** — carried over; reinforced by
   `iamctl`, which implements exactly this switch for org-level IS APIs
   ([setup.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/pkg/utils/setup.go)).
4. **Rotation-safe refresh persistence** — carried over from landscape.
5. **Per-product session multiplicity** — see §3; one context needs several
   concurrent product sessions, which §4.6's "relevant product session"
   wording anticipates but the evidence now makes mandatory.

### 1.6 Where the plan is strictly better than the estate

The keychain + broker design is ahead of, not behind, every shipping WSO2
CLI: **five** CLIs persist secrets in plaintext files — apictl
(base64 JSON, printed plaintext warning), amctl (YAML with refresh tokens and
client secrets), ap (YAML with an explicit plaintext note), mi (same
base64-JSON store as apictl, storing username, password, *and* token —
[miCredentials.go](https://github.com/wso2/product-mi-tooling/blob/master/cmd/credentials/miCredentials.go),
[jsonstore.go](https://github.com/wso2/product-mi-tooling/blob/master/cmd/credentials/jsonstore.go)),
and iamctl (client ID/secret in JSON server-config files, with env-var
substitution as the only alternative —
[setup.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/pkg/utils/setup.go)).
This is the concrete case for product teams adopting the shell's broker
rather than the shell accommodating five incompatible plaintext stores.

## 2. Per-product authentication and compatibility

Verdicts: **COMPATIBLE** (planned methods work today), **ADAPTER** (works
only via a product-specific token or legacy mechanism the module must
handle), **INCOMPATIBLE-TODAY** (needs a named backend change).

### 2.1 Legacy API Manager (`apictl`)

- **On-prem:** management REST APIs authenticate against APIM's own resident
  key manager — DCR with user-credential basic auth, then the password
  grant (landscape §4). No documented acceptance of external IdP tokens on
  the management APIs.
- **Cloud:** no current public SaaS auth story for legacy APIM; the
  next-generation API Platform (§2.2) is the successor surface.
- **Token verifier:** APIM resident key manager itself.
- **Verdict: ADAPTER** via a pre-generated "personal access token"
  (`apictl login --token` semantics); **INCOMPATIBLE-TODAY** for any
  interactive planned method — the backend change would be management-API
  acceptance of IdP-issued tokens (owner: `wso2/product-apim`), for which no
  public evidence exists.

### 2.2 API Platform (`ap`, next-generation APIM)

- **Cloud:** portals authenticate users via **Asgardeo** (production setup
  guide; per-devportal-org sub-organizations, `org_id` claim verification;
  landscape §6).
- **On-prem:** `platform-api` auth modes `file` (local users, RS256 JWT,
  dev-only), `internal_token`, and **`idp`** — any OIDC issuer with a JWKS
  endpoint, exercised in-repo with both Asgardeo and ThunderID (landscape
  §6).
- **Token verifier:** platform-api's JWT authenticator against the
  configured issuer/JWKS.
- **Verdict: COMPATIBLE** — browser PKCE and client-credentials tokens from
  the configured IdP work as bearer tokens today; the `ap` CLI's missing
  login flows are exactly what the shell supplies. Device code inherits the
  configured IdP's capability (yes on Asgardeo/IS, no on Thunder).

### 2.3 Agent Manager (`amctl`)

- **Cloud and on-prem:** the Agent Manager service provisions and reconciles
  **Thunder** instances as its IdP; amctl does browser PKCE (loopback
  `127.0.0.1:10325`, seeded public client `amctl`) and client credentials,
  discovered resource-first (landscape §5;
  [login reference](https://github.com/wso2/agent-manager/blob/main/documentation/docs/reference/cli/login.mdx)).
  Whether an external IS can replace the bundled Thunder is **unknown from
  public sources**.
- **Token verifier:** the instance's Thunder.
- **Verdict: COMPATIBLE** for browser PKCE and client credentials;
  **INCOMPATIBLE-TODAY** for device code (Thunder gap, §1.2) and PAT.

### 2.4 Micro Integrator (`mi`)

- **On-prem:** the CLI does basic-auth `GET` to the MI **Management API**
  `/management/login` resource and receives an `AccessToken` — a JWT issued
  by MI's **own internal token store**, default validity 3600s, default port
  9164; logout revokes at `/management/logout`.
  [miCredentials.go](https://github.com/wso2/product-mi-tooling/blob/master/cmd/credentials/miCredentials.go),
  [constants.go](https://github.com/wso2/product-mi-tooling/blob/master/cmd/utils/constants.go),
  [login.go](https://github.com/wso2/product-mi-tooling/blob/master/cmd/cmd/login.go)
  Users come from MI's user store (file/LDAP options); the security docs
  describe JWT authentication with an internal token store and offer toggles,
  but document **no acceptance of external IdP tokens** on the Management
  API.
  [Securing the Management API](https://mi.docs.wso2.com/en/latest/install-and-setup/setup/security/securing-management-api/)
- **Cloud:** no public cloud Management-API auth story found (**unknown from
  public sources**).
- **Token verifier:** MI itself.
- **Verdict: ADAPTER at best** — the module must perform the
  basic-auth→MI-JWT exchange, which requires a stored username/password
  (what mi does today) or per-invocation credential entry; both sit badly
  with §7.2's secret rules. For planned methods proper it is
  **INCOMPATIBLE-TODAY**; the backend change is external-IdP token
  acceptance on the Management API (owner: `wso2/micro-integrator` /
  `wso2/product-mi-tooling`). Since [architecture §12](../architecture.md)
  names `mi` a pilot candidate, the pilot will exercise the adapter path,
  not the broker's happy path — worth choosing deliberately.

### 2.5 Identity Server itself (`iamctl`)

- **On-prem (and Asgardeo tenants, which share the `/t/{tenant}` shape):**
  `iamctl` authenticates with **client credentials** using a management-app
  client ID/secret from JSON config or environment variables, optionally
  followed by `organization_switch`; a legacy interactive mode uses the
  password grant.
  [setup.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/pkg/utils/setup.go),
  [server.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/cmd/interactive/server.go)
- **Token verifier:** IS itself (it is the IdP).
- **Verdict: COMPATIBLE** — client credentials works today; browser PKCE and
  device code are backend-supported (landscape §2) even though iamctl never
  implemented them.

### 2.6 Choreo CLI (`choreo`) and WSO2 Developer Platform CLI (`wdp`)

- **Source availability:** both public repos contain only README, license,
  and install scripts — **no CLI source is public**, so the login
  *implementation* (PKCE vs device-style link) is **unknown from public
  sources**.
  [choreo-cli repo](https://github.com/wso2/choreo-cli),
  [wdp-cli repo](https://github.com/wso2/wdp-cli)
- **Documented behavior (cloud only):** `wdp login`/`choreo login` is
  browser-based — "Follow the instructions on the console to open the link
  in the browser and login" — and Choreo additionally supports console-issued
  **personal access tokens** consumed via `choreo login --with-token` from
  stdin (§1.3).
  [WDP CLI get started](https://wso2.com/engineering-platform/developer-platform/docs/choreo-cli/get-started-with-the-choreo-cli/),
  [PAT guide](https://wso2.com/choreo/docs/platform-engineer/choreo-cli/manage-authentication-with-personal-access-tokens/)
- **Token verifier:** the Choreo/WDP control plane; internal validation
  chain unknown from public sources.
- **Verdict: method-shape COMPATIBLE** (browser interactive + PAT + stdin
  sourcing is exactly the planned surface), but concretely **ADAPTER**: the
  control-plane APIs and token formats are proprietary and undocumented, so
  a module would consume product sessions/PATs rather than broker-issued IdP
  tokens.

### Summary table

| Product | Cloud auth path | On-prem auth path | Token verifier | Verdict vs broker |
|---|---|---|---|---|
| Legacy APIM (`apictl`) | none current | DCR (basic auth) + password grant to resident KM | APIM resident key manager | ADAPTER (manual token); INCOMPATIBLE-TODAY interactive |
| API Platform (`ap`) | Asgardeo | platform-api `file`/`internal_token`/**`idp`** (any OIDC issuer) | platform-api via issuer JWKS | COMPATIBLE |
| Agent Manager (`amctl`) | Thunder (provisioned) | Thunder (provisioned) | Thunder | COMPATIBLE (PKCE, client creds); device code blocked by Thunder |
| Micro Integrator (`mi`) | unknown | basic auth → MI-internal JWT (`/management/login`) | MI internal token store | ADAPTER; INCOMPATIBLE-TODAY for planned methods |
| Identity Server (`iamctl`) | Asgardeo-shaped tenants | client credentials (+ org switch); legacy password | IS itself | COMPATIBLE |
| Choreo (`choreo`) | browser link + console PATs | n/a | Choreo control plane (internals unknown) | ADAPTER (proprietary tokens; right method shape) |
| WDP (`wdp`) | browser link (PAT docs shared with Choreo) | n/a | WDP control plane (internals unknown) | ADAPTER (same) |

## 3. Can one login session serve multiple modules in one context?

The deciding property is whether a product **delegates token validation to a
configurable external IdP** or **bundles its own resident issuer**.

| Deployment shape | What one session can cover | What it cannot |
|---|---|---|
| **Cloud (WSO2 Cloud / Asgardeo identity)** | One Asgardeo login (browser PKCE or device code) covers every product whose backend validates Asgardeo JWTs — today that is API Platform (platform-api IdP mode with Asgardeo, plus its Asgardeo-org portals) | Agent Manager (validates only its Thunder issuer); Choreo/WDP control planes (proprietary sessions/PATs). These need their own product sessions in the same context |
| **On-prem with a shared external IS** | One IS login covers API Platform (point `idp` mode at the IS issuer) and IS-management operations themselves | Legacy APIM (resident KM only), MI (internal token store only), Agent Manager (bundled Thunder; external-IS option unknown from public sources) |
| **On-prem, products stand-alone** | Nothing is shared: every product is its own issuer (APIM resident KM, MI token store, Thunder per Agent Manager install) | One session per product; the context must hold several product sessions |

Resident-issuer vs delegating products, explicitly: **delegating** — API
Platform (`idp` mode); **resident** — legacy APIM (resident key manager), MI
(management token store), Agent Manager (bundled Thunder), Choreo/WDP
(control-plane sessions). Thunder and IS are themselves issuers.

Conclusion: the shell-owns-auth goal is achievable *per identity domain*,
not universally. A context therefore needs (a) one interactive IdP session
that multiple delegating modules share — the design's core value, already
real for API Platform + IS on-prem and for Asgardeo-backed products in
cloud — and (b) N product sessions for resident-issuer products, obtained
via adapter mechanisms (PAT, product login exchange). §4.6's "resolves the
selected context and relevant product session" wording matches this; the
evidence turns it from an allowance into a requirement.

## 4. Design implications

Implications only; decisions belong to the architecture and product
requirements documents.

- **Method verdicts justify the planned set with sharper legality rules**:
  browser PKCE (universal), client credentials (universal), device code
  (per-backend capability, refuse where not advertised), PAT (legal only for
  products with product-issued tokens — Choreo/WDP today, legacy APIM's
  manual token, potentially others later). Nothing found this round argues
  for adding the password grant to the planned set; everything that needs it
  (legacy APIM, MI, iamctl legacy mode) is adapter territory.
- **Downscoping must be a per-backend capability, not a uniform broker
  promise.** Full audience+scope narrowing exists only on Thunder
  (RFC 8693 + RFC 8707). On Asgardeo/IS the broker can at most narrow
  scopes, and whether refresh-time narrowing works needs an empirical test
  before the module contract promises it. The §4.6 carve-out ("when the
  deployment supports them") should be surfaced to modules as a queryable
  capability rather than silently degraded.
- **The context model needs first-class product sessions.** One IdP session
  plus N product sessions per context is the observed reality (§3). The
  broker API should distinguish "give me an IdP token for audience X" from
  "give me product P's session credential".
- **Backend asks worth filing, with owners** (§1): Thunder device grant
  (`thunder-id/thunderid`); Asgardeo/IS self-token exchange with
  downscoping/resource support (Asgardeo service team, `wso2/product-is`);
  IdP-side PATs (same owners); management-API IdP-token acceptance in legacy
  APIM (`wso2/product-apim`) and MI (`wso2/micro-integrator`); a
  WSO2-published public client for the CLI (Asgardeo/IS teams — Thunder-side
  products already seed one).
- **Pilot-selection consequence**: the §12 pilot pairing (amctl + apictl or
  mi) makes the second pilot exercise the *adapter* path by construction —
  MI's management API cannot consume any planned method today. That is
  valuable for proving the hard migration, but the pilot plan should not
  expect the broker's IdP flow to carry it.
- **Choreo's PAT intake validates the plan's CI secret rules**: stdin-only
  token intake (`--with-token`) is shipping WSO2 practice, matching §7.2's
  stdin/env sourcing and strengthening the case against flag-passed secrets.
- **Round-one correction of emphasis, not fact**: the landscape doc's PAT row
  ("no PAT feature found" at the IdPs) stands, but the estate-level picture
  is better than round one implied — Choreo/WDP already operate a
  console-issued, scoped PAT with exactly the ergonomics the plan wants; the
  gap is IdP-side only.
