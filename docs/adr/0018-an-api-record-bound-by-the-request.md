# ADR 0018: An API Record Bound by the Request

**Status:** Accepted

A module may ask the broker for access to one API its product serves, bound
to a resource the request names rather than one the context records. The
request names the `api` record and the API's own audience; the shell exchanges
the login session's token at the login issuer (RFC 8693, ADR 0014's
`exchanged` strategy) with that audience as the RFC 8707 resource, proves the
token that comes back is bound to exactly it, and hands it to the module for
this one command. Nothing is stored.

Until now every audience a module could be granted was one the context
recorded: the product's own, or its gateway's (ADR 0005, ADR 0014). That rule
is what lets the shell prove a module receives no more than the user recorded.
It also meant a person who had just published an API through `wso2 api` could
not call it: the API's audience is the API's, set in its `jwt-auth` policy at
creation, and no context records one entry per API. Testing a published API
needed a token from somewhere else, in practice a script driving the identity
provider's flow API with a password, which is the thing this CLI exists to
make unnecessary.

The rule is amended, not dropped. Three things bound the `api` record.

**The module opts in and the product must be exchangeable.** A descriptor
declares `invocation`, and only with the `exchange` grant: the access is
derived from the login session, so a product reached any other way has nothing
to exchange, and a client-credentials context has no login session at all.
Both are refused before anything is asked of the issuer.

**The resource is never a recorded audience.** A request whose resource is the
audience of any record the context holds, for any product, is refused
(`auth.invocation_refused`). Without this the `api` record would be a second
way to a product's management token, past that record's scope checks. The
resource must also be an absolute URI, which is what a resource server is
named by.

**The identity provider is the allowlist, and the binding is proved.** A
resource the provider does not register is refused by the provider
(`invalid_target`, reported as `auth.exchange_unavailable` naming the
registration to make), so an administrator decides which APIs can be called
this way by deciding which resource servers exist. The token that comes back is
checked to carry the requested resource in `aud` before the module sees it, as
every exchanged token already is; a provider that answered 200 with a token
bound elsewhere hands the module nothing.

Scopes are not part of the record. Measured against Thunder on 2026-09-18, an
exchange carries no resource-server permissions across whatever scope it is
asked for, so the API authorizes the call from the claims the token carries,
as ADR 0014 recorded for the `exchanged` strategy. The token's lifetime is the
provider's (an hour on Thunder) and cannot be shortened or revoked from the
shell; `wso2 api apis token` says so on standard error when it hands one over.

A command is granted each record once. Calling an API needs two: the product's
own, to read where the API is deployed and what it is bound to, and the API's.
The broker's once-per-command rule becomes once per record per command; a
module still cannot renew what it was given.

Only an identity provider that offers token exchange is served. Identity
Server and Asgardeo would need a different derivation, a further authorization
for the resource through the existing sign-on, and that is not measured. Until
it is, the refusal names what is missing rather than guessing.
