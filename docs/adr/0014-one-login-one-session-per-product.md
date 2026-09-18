# ADR 0014: One Login, One Session per Product

**Status:** Accepted

An identity is one login provider and one person. It holds one session per
product it reaches, and every session is obtained from the same browser
sign-on: the person enters credentials once, and the shell runs as many
authorizations as the products need, each answered by the provider's
sign-on cookie. A product session is used for exactly one scope set, bound
to that product's audience, so the proof in ADR 0005 that a module receives
exactly what it asked for is unchanged, and no token ever carries another
product's scopes.

This replaces the earlier rule that an identity is one session narrowed
per command. That rule cannot serve several products: ThunderID binds one
authorization to one resource server, and both ThunderID and Identity
Server permanently narrow a refresh token to the smallest scope set ever
requested with it, so the first command shrinks a shared session and the
next product's command is refused. The alternative of one wide refresh
token with the audience bound per command is rejected: it hands every
module a token carrying every product's scopes, and on ThunderID it cannot
be obtained at all.

How a product's session is obtained is chosen per product from four
strategies, in this order: `direct` when the login authorization already
covers the product; `derived` when the product record names a jwt-bearer
grant at the product's own issuer and no browser is needed; `federated`
when the product's own issuer is a public client federated to the login
provider; `sibling` when the login provider issues for the product under a
different resource or scope set. The shell records the strategy on the
session and shows it. A product with none is refused, naming the command
that records one.

A fifth strategy, `exchanged`, is chosen when the product record names an
exchange grant. The login session's own access token is exchanged at the login
issuer under RFC 8693 for one bound to the product's audience, per command,
and the product holds no session at all: an exchange answers with an access
token alone, so there is nothing to store, rotate or revoke for it. It ranks
ahead of `sibling` for a product whose deployment validates the login
provider's tokens through its JWKS — measured 2026-09-09 against the WSO2 API
Platform, whose control plane, gateway controller and gateway all do — because
`sibling` costs a second authorization and this costs none. `wso2 login` runs
no authorization for such a product, and a command reaching one needs no
browser even on a machine that has never authorized it.

Two constraints follow from the grant rather than from this decision. The
login authorization must carry the login product's own scopes, because an
exchange preserves OIDC scopes and drops resource server permissions, so a
scope not asked for at login cannot be recovered later. And the exchange sends
the product's audience as an RFC 8707 resource indicator, so that audience
must be an absolute URI — the shell refuses one that is not when the product
is recorded, rather than letting the issuer answer `invalid_target`, which
reads as a resource server nobody registered.

A product may hold a second record at the login provider for its gateway.
The gateway validates tokens the login provider issues, so its session is
always obtained there, under the API's own resource server and the API's own
scope set, by the same strategies: `direct` when that happens to be the
login session's binding, `sibling` otherwise, and `inline` for a machine
client. The record lives inside the product's entry rather than as a second
namespace, so the module a command resolves to, the login product pin and
`wso2 login --only <namespace>` are untouched, and the session is reported
and stored under the product's gateway key, `<namespace>/gateway`. A module
asks for it by naming the record in its access request, and only a
descriptor that declares a gateway shape may be asked.

In a pipeline there is no browser and no sign-on, so nothing called a
session is shared. One machine client at the login provider is minted per
product with that product's resource indicator or scope set, and a product
that cannot map a machine client to its management roles carries its own
client credential on its record, still under one identity. The shell never
runs a scripted user login.

Consequences: an exchanged product has no session to store, report an expiry
for, or revoke, so `wso2 whoami` reports it as `exchanged` rather than as
having none — reporting none would read as "not logged in" and send a reader
to run a login that establishes nothing for it. The session store is keyed by
credential reference and
product namespace; `wso2 login` acquires every recorded product's session
and a command acquires a missing one on first use, refusing under
`--no-input`; `wso2 whoami`, `wso2 doctor` and `wso2 logout` report and
act per product; a client-credentials identity is healthy with no session.
The measurements are summarised under Evidence below.

## Evidence

Measured 2026-09-06 and 2026-09-07 against ThunderID 1.0.1, API Manager
4.7.0 and Identity Server 7.1.0 in local containers, first with curl and then
through the shell. The full write-ups (three research notes) are in git
history; this is what the decision rests on.

| Case | Result | Prompts | Redirects |
| --- | --- | --- | --- |
| ThunderID login serving the ThunderID product (`direct`) | Pass | 1 | 2 |
| Same login serving API Manager management (`federated`) | Pass, no second password | 0 | 5 |
| First use of a product after `wso2 login --no-products` | Pass, authorized through the sign-on | 0 | 5 |
| Same under `--no-input` | Refused before any browser opens (`auth.session_required`) | 0 | 0 |
| Identity Server login serving API Manager (`derived`, jwt-bearer) | Pass; commands needing different scopes answer in turn | 1 | 4 |
| API Manager gateway at the login provider (`sibling`) | Pass for the shell's leg; gateway answered 200 | 1 | 4 |
| User with no mapped group | Sessions established; both products refuse at the command | 1 | 7 |
| CI, one machine client, ThunderID product | Pass, no login step | 0 | 0 |
| CI, same machine client, API Manager | Refused as designed; needs its own client credential | 0 | 0 |
| Asgardeo | Not run, no tenant | | |

Findings the design depends on:

- **ThunderID binds one authorization to one resource server.** It refuses an
  authorization with no resource indicator when no default resource server is
  configured, so a resource-less ThunderID session is impossible.
- **Refresh narrowing is permanent on both providers.** ThunderID binds the
  refresh token to one resource server's scopes; Identity Server narrows it to
  the smallest scope set ever requested and never widens it back. A shared
  session narrowed per command therefore breaks the next product's command.
  Per-product sessions remove the conflict.
- **API Manager's jwt-bearer grant accepts only `typ: JWT`.** Every ThunderID
  access token is typed `at+jwt` (RFC 9068) and is refused with "Signature
  validation failed" whatever its audience, with the same signing key. The
  ThunderID ID token (`typ: JWT`, `aud` the CLI client) is accepted, so
  `derived` needs an ID token and stays an interactive strategy; a machine
  client has none.
- **The assertion's audience must match the identity provider alias** API
  Manager records; an ID token issued to another client is refused.
- **The assertion scopes are load-bearing.** Without `groups` in the ID token,
  API Manager maps no role and issues `default` only, for administrators too.
- **API Manager does not narrow a management session per command.** A module
  that named one scope per command looped into `auth.narrowing_unavailable`;
  modules now send no scopes and inherit the product record's scope set.
- **ThunderID grants `system` to a client-credentials client** holding a role
  with that permission, so ThunderID administration works from CI.
- **Federation to ThunderID from API Manager needs just-in-time provisioning**;
  without it the federated user's roles never reach the scope issuer.
- **Access-token expiry was not measured**; renewal through the sign-on is
  inferred from the first-use path.

## Amendment (ADR 0016)

What this ADR calls an identity is now a context, which owns its login and its
sessions: product sessions are keyed by the context's credential reference,
and no two contexts share one. A stored session also records the resource and
audience it was authorized for, and is presented only for a record that still
asks for them.
