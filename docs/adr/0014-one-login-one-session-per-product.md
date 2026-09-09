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
The measurements are in `docs/research/2026-09-06-single-login-spikes.md`
and the two proof documents it cites.
