# Design: one login, product tokens derived by grant

**Status:** Proposed, 2026-09-06
**Builds on:** the measured route in
`docs/research/2026-09-06-single-login-thunder-apim-proof.md`, the
setup guide `docs/research/2026-09-06-single-login-product-setup.md`,
and the broker in `internal/auth`.

## 1. Goal

After one `wso2 login`, both `wso2 iam …` and `wso2 apim …` run against a
deployment where ThunderID is the login provider and API Manager trusts
ThunderID, with no second credential entry, no second browser window,
and no client secret in the CLI. The measured route: the shell refreshes
its Thunder session for an ID token and presents that ID token to API
Manager's token endpoint under the JWT bearer grant as a public client.

```sh
wso2 identity create thunder --issuer http://localhost:8492 --client-id wso2-cli \
  --provider thunder --product iam --endpoint http://localhost:8492 \
  --audience https://localhost:8090/mcp --scope system
wso2 identity add-product thunder apim --endpoint https://localhost:9443 \
  --audience <apim public client id> --scopes apim:api_view,apim:api_create,… \
  --grant jwt-bearer --grant-issuer https://localhost:9443/oauth2/token \
  --grant-client-id <apim public client id> --grant-scopes openid,email,groups
wso2 login --context thunder          # one browser sign-in
wso2 iam users list                   # Thunder access token from the session
wso2 apim apis list                   # APIM token derived from the session
```

Out of scope, recorded for later: caching derived tokens between
commands (each command derives afresh: one refresh, one JWT bearer
request); a module-side `apim bootstrap --trust-issuer` that performs the
operator setup the proof script performs; Identity Server, Asgardeo and
Platform Gateway as targets; using the derived grant from a
client-credentials identity; exchange (RFC 8693) as a further strategy.

## 2. The rule that changes

Today an identity is one issuer and one session, and every product under
it is reached by narrowing that session. The new rule: a product may name
a **grant** that says how access for it is obtained from the session when
the product's issuer is not the identity's. The session stays the single
credential; the grant is a derivation, not a second login.

## 3. Context document

`Product` gains one optional member, `grant`:

```json
"apim": {
  "endpoint": "https://localhost:9443",
  "audience": "<apim public client id>",
  "scopes": ["apim:api_view", "apim:api_create"],
  "grant": {
    "kind": "jwt-bearer",
    "issuer": "https://localhost:9443/oauth2/token",
    "clientId": "<apim public client id>",
    "scopes": ["openid", "email", "groups"]
  }
}
```

- `kind` is `jwt-bearer` (the only kind this release implements). Any
  other value is refused as malformed.
- `issuer` is the target's OpenID issuer; the token endpoint is discovered
  from it, exactly as for an identity's issuer. Same URL rules as an
  issuer: http or https, a host, no embedded credentials.
- `clientId` is the public client the shell presents at that issuer. No
  secret member exists; the type has nowhere to put one (ADR 0012).
- `scopes` are what the shell asks the identity's issuer for when it
  refreshes the session for the assertion. `openid` is required and is
  added when absent; the others are whatever makes the ID token carry the
  claims the target maps (for Thunder to API Manager: `email`, `groups`).
- `audience` on the product stays required and is what the derived token
  is proved bound to. On API Manager that is the client id.

Schema version stays 2: the member is additive and a shell that predates
it ignores it (the decoder tolerates unknown members), then refuses the
product at the broker with `auth.product_not_configured` because the
narrowed refresh cannot bind that audience. No document a v2 shell wrote
becomes unreadable.

The token-resource derivation's "exactly one product" rule counts only
products without a grant. A Thunder identity may serve `iam` directly and
`apim` by grant.

`wso2 identity add-product` gains `--grant`, `--grant-issuer`,
`--grant-client-id` and `--grant-scopes`; the four are legal only
together, and `--grant` takes only `jwt-bearer`. `wso2 identity create`
is unchanged: the product it creates is the one the login binds to.

## 4. Login

`productScopeUnion` includes every grant's `scopes`, so the session's
refresh token is authorized for the assertion claims from the start. The
resource indicator is unchanged: it is the one product without a grant.
Nothing else about login changes; the report still lists every product.

## 5. Broker

`resolveSource` gains one branch: an interactive identity whose product
for this namespace names a grant is answered by an `assertionSource`.
`checkProduct` and `checkHomeTenant` apply first, unchanged. A
client-credentials identity with a grant is refused with
`auth.kind_not_implemented`.

`assertionSource` does, under the session's rotation lock:

1. Refresh the stored session with the grant's `scopes` (the same
   `sessionSource` refresh, with the rotated refresh token persisted
   before anything else happens). Take the ID token from the answer;
   refuse with `auth.narrowing_unavailable` if there is none, saying the
   identity provider issued no identity token for the assertion.
2. Discover the grant issuer's token endpoint.
3. POST `grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer`,
   `assertion=<id token>`, `client_id=<grant.clientId>`,
   `scope=<request.Scopes>` through the existing `requestToken` (a public
   client names itself in the body).
4. Verify the answer with the existing `tokenResponse.verify` against the
   request and the product audience. Issued scopes must equal the request
   exactly and the token must carry the audience. A target that answers
   `scope default`, as API Manager does for a user with no mapped role,
   is therefore refused before the module runs, with the same
   `auth.narrowing_unavailable` denial a refused narrowing produces.
5. Return the access token and its expiry. The target's refresh token,
   if any, is discarded: the session is the one credential.

Refusals from the target's token endpoint map as: `invalid_scope` (400)
to `auth.narrowing_unavailable`; `invalid_grant` and `invalid_client` to
a new denial `auth.trust_not_configured` in the authentication class,
whose guidance names the grant issuer and says the target did not accept
the identity provider's assertion for this client (trust, audience alias
or role mapping on the target). Transport failure maps to the existing
issuer-unreachable denial. No denial names the token or the assertion.

The ID token is verified nowhere in the shell: it is an opaque assertion
the target verifies. The shell never stores it.

`tokenResponse` gains `IDToken string` (`id_token`). The session store
is unchanged.

## 6. Diagnostics

The existing "brokering module access" debug line gains `grant_kind`
and `grant_issuer` when a grant applies. `wso2 whoami` and `wso2 identity
list` show the grant issuer beside a product that has one.

## 7. Testing

- `internal/contexts`: a product with a grant round-trips; a grant with an
  unknown kind, a missing client id, or a bad issuer URL is refused with
  the malformed-document problem; a token-resource identity with one
  direct product and one grant product validates, and with two direct
  products does not.
- `internal/auth`: the fake issuer gains a JWT bearer grant that issues
  when the assertion is an ID token it minted for its own client and the
  requested scopes are within a configured set, and refuses otherwise.
  Tests cover: a grant product is answered from the session with exactly
  the requested scopes bound to the product audience; a target that
  issues fewer scopes is refused; a target that refuses the assertion is
  refused with `auth.trust_not_configured`; the rotated refresh token is
  persisted before the assertion is presented; no denial carries the ID
  token or the refresh token; a session without `openid` produces the
  no-identity-token refusal.
- `internal/app`: `identity add-product` with the grant flags writes the
  member; the flags are refused when partial; login's scope union
  includes the grant scopes.
- Live: the journey in section 1 on `cli-thunder3` and `cli-apim`, then
  `wso2 apim apis list` after `wso2 logout`, after a CLI restart, and as
  `cliuser`. Recorded in the research document.

## 7a. Known limitation: Thunder as the login provider

Measured live 2026-09-06 (see the proof document). Thunder mandates a
resource indicator on every authorization and binds the refresh token to
that one resource server's scopes, and it rotates the refresh token on
every refresh, binding the new one to the scopes that refresh requested.
Two consequences for this design:

- A Thunder session's refresh token can re-issue only its resource
  server's scopes, so it cannot yield the `openid`-bearing identity token
  the assertion needs unless that resource defines those scopes.
- Even a full-scope refresh token shrinks on first per-command use, so one
  Thunder session cannot serve two different scope sets.

So with Thunder as the login provider the derived grant does not deliver
single login across a direct product and a derived one. It is correct for
a provider whose refresh is not resource-bound and does not narrow on
rotation — Identity Server and Asgardeo, the documented federation route,
untested here. Open question, not decided in this spec: whether the
Thunder derivation should refresh with the full granted union and bind
only the audience per command, giving up per-command scope narrowing on
Thunder to make one session serve several products.

## 8. Security notes

- The assertion route adds no credential. The shell already holds the
  refresh token; the ID token is minted from it per command and never
  stored.
- Trust is configured on the target by an administrator (issuer, JWKS,
  alias, role mapping). The shell cannot widen it: a target that maps no
  role issues no management scope, and the exact-scope check refuses the
  result.
- The audience alias on the target is the CLI client id, so an ID token
  minted for any other Thunder application is refused by the target.
