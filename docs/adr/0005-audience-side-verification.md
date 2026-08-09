# ADR 0005: Audience-Side Verification of Brokered Access

**Status:** Accepted

An audience receiving brokered access establishes trust for itself rather than
inheriting the shell's. It verifies an issuer-minted token's signature against
the keys that issuer publishes, matches the token's issuer exactly, and
accepts the token only when the audience it serves is among those the token
names — membership, not equality, because an Asgardeo access token's `aud` is
the client identifier and an Identity Server 7.3.0 token's is the client
identifier together with the API resource, and a service demanding equality
would refuse every token one of them issues. The permission check asks whether
the token covers what the endpoint requires, which is deliberately weaker than
the broker's own check that the issued scopes equal the request: the broker is
proving it did not obtain more than it asked for, while the audience is
deciding whether what arrived is enough. A token whose permissions cannot be
read is refused rather than read as carrying none.

The issuer is the organization boundary. A deployment is not obliged to state
an organization in a token — Asgardeo mints no such claim outside a
sub-organization setup — so an audience is configured with the issuer that
speaks for its organization, and a token minted anywhere else is refused
however valid its signature and claims. Where a deployment mints an
organization claim, it must additionally match. The organization it reports is
always its configured value and never one read out of a token, so what a user
is told cannot be steered by a claim.

Binding access to a single shell invocation does not survive the move to real
identity providers. No OAuth issuer mints an invocation claim, so a header
naming the invocation remains required of every caller while nothing can hold
an issuer-minted token to it. The architecture proof's development fixture,
which does carry such a claim, is the only place that binding is enforced, and
it is not a property a product service can be built on.

## Considered Options

- Letting an audience trust the shell's verification would remove the second
  check, but the shell reads a token's claims without verifying them — safe
  for the shell, which received it over the issuer's own connection in answer
  to its own request, and worthless to a service receiving a bearer token from
  a caller it knows nothing about.
- Requiring `aud` to equal the audience's own identifier is the stricter
  reading and is what the OpenID client library does by default. It was
  measured against both supported deployments and would refuse every Asgardeo
  token, so the audience performs its own membership check instead.
- Treating an absent organization claim as satisfying the organization check
  would let any issuer with a trusted signature act for any organization. The
  binding is the exact issuer match instead, and the fixture format — whose
  tokens always carry the claim — refuses its own when they omit it, so the
  tolerance is exercised only by the format that legitimately omits one.
- Deriving the accepted signing algorithms from the issuer's discovery
  document would let a deployment widen what an audience verifies by editing
  its own configuration. One algorithm is stated explicitly instead. The
  client library already refuses unsigned and symmetric algorithms before a
  caller's configuration is read; what the explicit statement adds is
  narrowing the library's asymmetric allowlist to the one algorithm the
  audience accepts.
