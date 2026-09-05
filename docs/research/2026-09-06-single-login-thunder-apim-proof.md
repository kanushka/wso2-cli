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
