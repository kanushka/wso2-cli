# Single login proof: ThunderID login, API Manager management

Measured 2026-09-06 against the running exercise containers: ThunderID
v1.0.1 (`cli-thunder3`, port 8492) and API Manager 4.7.0 (`cli-apim`,
port 9443, OAuth component 6.14.14). Every request below was made with
curl against the products' own endpoints, not through the shell. The
scripts that reproduce it live in the exercise directory beside the
repository (`cli-exercise-2/`). No token, secret or password is recorded
here.

## Result in one line

One ThunderID credential entry yields a Thunder management call and a
scoped API Manager Publisher call, with no client secret anywhere, no
second browser window, and a refresh path that keeps working after the
first tokens expire. A user outside the administrator group is refused by
both products through the same route.

## The route

```text
wso2 login (Thunder, public client wso2-cli, PKCE, resource = System)
   |
   +-- access token  (aud System, scope system)          -> Thunder APIs
   |
   +-- ID token      (aud wso2-cli, groups, email)
          |
          v
       APIM token endpoint, grant urn:ietf:params:oauth:grant-type:jwt-bearer
       assertion = ID token, client_id = public APIM CLI client, no secret
          |
          v
       APIM access token (scope apim:api_view apim:api_create ...) -> Publisher
```

The assertion is the **ID token**, not the access token, and not a token
from Thunder's exchange endpoint. The reason is below under "What did not
work".

## Configuration that was applied

Thunder (`cli-thunder3`, through its management API with a system token):

- Application `WSO2 CLI` (client `wso2-cli`): access tokens carry the user
  attributes `given_name family_name email groups name ouId`; ID tokens
  carry `groups` and `email` in addition to the defaults; a `groups`
  scope maps to the `groups` claim; the token-exchange grant is enabled
  (it turned out not to be needed for the working route).
- Resource server `APIM Management`, identifier
  `https://localhost:9443/oauth2/token` (needed only for the exchange
  experiment; not used by the working route).
- User `cliuser`, in no group, for the denied case.

API Manager (`cli-apim`):

- A public OAuth application `wso2-cli-public` registered through the
  identity DCR endpoint (`/api/identity/oauth2/dcr/v1.1/register`) with
  grants `authorization_code refresh_token jwt-bearer` and the shell's four
  loopback callbacks. This DCR version rejects every `ext_*` member, so
  the public-client, mandatory-PKCE and JWT-token settings were applied
  afterwards through the `OAuthAdminService` admin service
  (`bypassClientCredentials=true`, `pkceMandatory=true`,
  `tokenType=JWT`).
- An identity provider `Thunder3` registered through the
  `IdentityProviderMgtService` admin service: property `idpIssuerName`
  = `http://localhost:8492`, property `jwksUri` =
  `http://host.docker.internal:8492/oauth2/jwks`, alias `wso2-cli`, a
  claim configuration in its own dialect declaring `groups`, `email` and
  `sub`, mapping `groups` to the local role claim and `email` to the local
  email claim, and one role mapping `Administrators` to the local `admin`
  role. Adding the identity provider without the `idpClaims` list fails
  with "No Identity Provider claim URIs defined" and leaves a half-written
  record that must be deleted.
- A key manager `Thunder3` through the admin REST API, the same payload
  `wso2 apim key-managers add` sends. It is not what makes the JWT bearer
  grant work: the grant handler reads only the identity-provider record.
  It is still needed for the gateway to accept Thunder tokens on
  subscribed APIs.

Nothing else on either product was changed. The debug loggers added to
the container's `log4j2.properties` for diagnosis were reverted.

## What was measured

| Step | Result |
| --- | --- |
| Thunder login as `admin`, scopes `openid email groups system`, resource System | Access token `aud` System, `scope openid email groups system`, `groups ["Administrators"]`. ID token `aud wso2-cli`, `typ JWT`, `groups`, `email`. |
| `GET /users` on Thunder with that access token | 200, one user listed. |
| JWT bearer at APIM with the ID token, `client_id` only, scopes `apim:api_view apim:app_manage apim:api_create` | 200; `scope` exactly the three requested; a refresh token was also issued. |
| `GET /api/am/publisher/v4/apis` with that APIM token | 200, two APIs listed. |
| `GET /api/am/admin/v4/key-managers` with that token (scope not requested) | 401. |
| Thunder refresh grant narrowed to `openid email groups` | New access token, new refresh token, and a new ID token carrying `groups`; so a later command can mint a fresh assertion without a browser. |
| ID token issued to the Console client presented as the assertion | `invalid_grant`, "None of the audience values matched the tokenEndpoint Alias wso2-cli". |
| ID token with one signature character changed | `invalid_grant`, "Signature validation failed". |
| Login as `cliuser` (no groups) | Thunder narrowed the grant to `openid email groups`, dropping `system`; `GET /users` 403. ID token has no `groups`. |
| JWT bearer at APIM with `cliuser`'s ID token | 200 but `scope default` only; Publisher 401. |

Credential prompts: one. Browser redirects: one (the Thunder
authorization). Client secrets: none.

## What did not work, and why

- **Thunder access tokens are refused by APIM's JWT bearer grant.** They
  are signed correctly (verified locally against the live JWKS), the key
  identifier matches, and APIM's JWKS validator reports "Matching key
  found", then "Signature validation failed". The difference between the
  refused access token and the accepted ID token is the header:
  `typ: at+jwt` (RFC 9068) versus `typ: JWT`. The Nimbus JOSE library
  APIM ships (9.37.4) admits only `JWT` or no type unless the caller
  configures otherwise, and APIM's validator does not. Thunder's exchange
  endpoint cannot help: `requested_token_type` `jwt` still yields
  `at+jwt`, and `id-jag` requires an ID token as subject and the client
  to be permitted ID-JAGs (it was not).
- **Thunder's exchanged tokens initially carried no user attributes.**
  Only after the application's access-token attribute list was set did the
  exchanged token carry `groups`. Moot for the ID-token route.
- **APIM's DCR endpoint ignores public-client settings.** They must be
  applied through the admin service or the Carbon console.
- **Thunder's exchange grant is per application.** Until it is added to
  the application's grant list the endpoint answers `unauthorized_client`.

## What this means for the shell

The shell already stores a refresh token per identity and narrows it per
product. For a product served by a different issuer it needs one more
strategy: refresh the session for an ID token, present that ID token to
the product's token endpoint under the JWT bearer grant as a public
client, and verify the answer exactly as it verifies a narrowed refresh.
The context document has to say, per product, which issuer and client to
present the assertion to. The design is in
`docs/superpowers/specs/2026-09-06-single-login-derived-grant-design.md`.

## Not tested

- Identity Server, Asgardeo and Platform Gateway as targets or as the
  login provider. No instance was running.
- Thunder as a target of an external issuer (its `trusted_issuer` block).
- APIM's RFC 8693 exchange grant for public clients (still absent from the
  rendered grant policy).
- Behaviour after Thunder's refresh token expires (one day by default).
- Logout and revocation propagation to the APIM-issued tokens.
- Tenants other than `carbon.super`, organization units other than the
  root.

## Live run through the shell, 2026-09-06

The derived grant was built into the shell (a product may name a
`jwt-bearer` grant; the broker refreshes the session for an identity token
and presents it to the product's issuer) and run against `cli-thunder3`
and `cli-apim`. Findings:

- **One login, `wso2 iam users list` works.** The Thunder session
  answers the direct `iam` product; the command listed both users after a
  single browser sign-in.
- **`wso2 apim apis list` fails in the same session**, and the cause is a
  Thunder property, not the grant. Thunder mandates a resource indicator
  on every authorization ("No resource parameter supplied and no default
  resource server is configured"), and the shell sends that indicator on
  the authorization-code token exchange as well as the authorization
  request. Thunder then binds the refresh token's grant to that one
  resource server's scopes. The System resource server defines only
  `system`, so the stored refresh token can re-issue only `system`, and
  the assertion refresh for `openid email groups` is refused with
  `invalid_scope`.
- **Not sending the indicator on the token exchange is not enough.** A
  refresh token that keeps the full authorized set still shrinks on first
  use: Thunder rotates the refresh token on every refresh and binds the
  new one's grant to the scopes that refresh requested. So the first
  per-command narrowing (`system` for iam, or `openid email groups` for
  apim) leaves a refresh token that cannot serve the other product.
- **A resource-less Thunder session is impossible.** An identity
  configured for scoped refresh cannot even log in against Thunder,
  because the authorization is refused without a resource.

The protocol route in the sections above still holds: one Thunder login
does yield both a Thunder management call and a scoped API Manager
Publisher call, when each token is obtained without narrowing the shared
refresh token. What the live run establishes is that the shell's
per-command scoped-refresh model, with Thunder's mandatory resource
binding and narrow-and-rotate refresh, cannot keep one Thunder session
usable for two different scope sets.

### What this means

The derived-grant mechanism is correct and unit-tested, and it is the
right shape for a login provider whose refresh token is not
resource-bound and does not narrow on rotation — WSO2 Identity Server and
Asgardeo, the documented federation route, neither of which was running
to test here. With **Thunder as the login provider**, single login across
a direct product and a derived product is blocked by the product's
refresh behavior. The resolutions are the ones already on record:

1. One Thunder identity, and so one login, per resource server — the
   product's own rule, measured on 2026-09-05.
2. Change the shell's Thunder derivation to refresh with the full granted
   scope union every time and bind only the audience per command, giving
   up per-command scope narrowing on Thunder. This is an architecture
   decision, recorded in the design spec's open questions, not made here.

No shell regression: the existing single-product Thunder journey
(`wso2 iam …` after one login) still works unchanged.

## Live run through the shell with Identity Server as the login provider, 2026-09-06

The derived grant was then tested against the provider it is designed for:
WSO2 Identity Server 7.1.0 (`cli-is`, https on 9444, http on 9764) as the
login authority, federating to API Manager 4.7.0 (`cli-apim`). Unlike
Thunder, Identity Server's authorization takes no resource indicator and
its refresh token is not bound to one resource server's scopes.

### Result

One `wso2 login` against Identity Server, then `wso2 apim apis list`
returns the two published APIs — through the shell, with the APIM token
derived from the login session by the JWT bearer grant, no client secret,
no second browser window. The command was run three times in a row from
the same session and worked every time; the session is not consumed, which
is the behaviour that failed on Thunder. A non-administrator user
(`cliuser`, in no group) logs in the same way but is refused:
`the "apim" module asked for the permissions apim:api_view and the
deployment issued default`, because Identity Server maps the user to no
role that carries `apim:` scopes. Certificate-expiry enforcement on API
Manager's JWT validator was on for the final run.

### Configuration applied

Identity Server (`cli-is`):

- A public OIDC application `wso2-cli`: authorization code, refresh,
  mandatory S256 PKCE, JWT access tokens, the four loopback callbacks,
  login consent skipped.
- Its requested-claims set includes `http://wso2.org/claims/groups`
  (optional), so the id_token carries `groups` for a user that has any.
  With the claim marked mandatory, a user with no group fails
  post-authentication (`claim.request.missing`), so it must be optional.
- A non-admin user `cliuser` for the denied case.

API Manager (`cli-apim`):

- A public OAuth application `wso2-cli-is` (through DCR, then made public
  with mandatory PKCE and JWT tokens over the admin service), which the
  shell presents at the JWT bearer endpoint.
- A trusted identity provider `ISLocal`: `idpIssuerName`
  `https://localhost:9444/oauth2/token`, `jwksUri`
  `http://host.docker.internal:9764/oauth2/jwks` (Identity Server's http
  JWKS, so no cross-container TLS is needed), alias the Identity Server CLI
  client id, and a role mapping from the remote group `admin` to the local
  `admin` role.

Shell identity: issuer `https://localhost:9444/oauth2/token`, provider
`identity-server` (scoped refresh, no resource indicator), one product
`apim` reached by a `jwt-bearer` grant whose issuer is API Manager's token
endpoint and whose client is the public API Manager CLI application.

### Two obstacles met and fixed

- **Published port.** Identity Server publishes its OIDC endpoints on its
  internal port, so a container mapped to a different host port fails
  discovery. Running it with a port offset so the published port matches
  the host port fixes it.
- **Expired demo certificate.** The 17-month-old image ships a signing and
  TLS certificate that expired in January 2026. Two symptoms followed: API
  Manager's JWT validator rejected the assertion (`X509Certificate has
  expired`), and the shell could not complete the TLS handshake for
  discovery. Renewing Identity Server's primary key with a fresh validity
  **and a subjectAltName for `localhost`** fixes both — the SAN matters
  because Go's TLS stack, unlike curl, ignores a certificate's common name.
  With a valid certificate, API Manager's expiry enforcement can stay on.

### What this establishes

The derived-grant mechanism delivers single login across products when the
login provider's refresh token is not resource-bound and does not narrow
on rotation. Identity Server is such a provider, and the shell's
per-command derivation works against it unchanged. The Thunder limitation
recorded above is a property of Thunder's refresh behaviour, not of the
design. Not tested: Asgardeo (the hosted equivalent), and a direct
Identity Server management product in the same session alongside the
derived one.
