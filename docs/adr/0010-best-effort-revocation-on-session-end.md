# ADR 0010: Best-Effort Revocation on Session End

**Status:** Accepted

Ending a session is more than forgetting a local credential: the refresh token
the shell holds stays usable at the issuer until the issuer is told otherwise.
`wso2 logout` therefore asks the issuer to revoke the refresh token per
RFC 7009 when the deployment's discovery document advertises a
`revocation_endpoint`, removes the shell-owned session entry from the OS secure
store, and reports which of those two things actually happened. **It removes the
local session and exits zero under every outcome, including the outcome where
revocation was refused or never attempted.**

That last clause is the decision. A command shaped like a security boundary
succeeding when the boundary was not enforced looks wrong, and is not: the local
session is gone in all three cases, so the command did what the user asked, and
a non-zero exit invites a retry loop that cannot succeed. What differs between
the outcomes is what the shell *claims*, not whether it worked. So the three are
named separately wherever logout reports:

- the issuer was asked and confirmed;
- the issuer advertises no revocation endpoint, so it was never asked;
- the issuer was asked and refused, or could not be reached.

This is the same discipline as
[ADR 0005](0005-audience-side-verification.md) and the open question in
[#37](https://github.com/wso2/wso2-cli/issues/37): state what was proven rather
than what the command name implies.

## Considered Options

- **Local removal only**, claiming nothing about the issuer. Honest and free,
  and it leaves a live refresh token at the issuer after a user asked to end a
  session. Rejected because the token, not the keychain entry, is the thing with
  authority.
- **Revocation required** — refuse to end the session unless the issuer
  confirms. Makes the guarantee uniform and makes logout impossible on any
  deployment that does not advertise the endpoint, which as of this decision
  includes every deployment we have measured, because we have measured none.
  Worse, it strands a user whose issuer is unreachable inside a session they
  cannot leave. Rejected.
- **One collapsed message** — "session ended" — under all three outcomes. This
  is precisely the failure #37 objects to one layer up: a report that reads
  stronger than the verification behind it.
- **A distinct problem code for the non-revoking outcomes.** In this codebase
  a `problem` is a refusal; minting one on a success path would make logout
  appear wherever refusals are counted. The outcomes are report fields.

## Consequences

**The provider facts were unmeasured when this was decided.** Nothing in
`docs/research/` records whether Asgardeo, Identity Server or ThunderID
advertises `revocation_endpoint`, whether their revocation endpoints accept a
public client — ours are public, authorization code with PKCE and no stored
secret — or whether revoking a refresh token cascades to already-issued access
tokens. Thunder is documented as supporting revocation, from reading its
protocol index rather than from a discovery measurement. The only concrete prior
art is apictl revoking at `oauth2/revoke`, and apictl is a confidential client
using the password grant, so it is not evidence that we can. This design is the
one that survives that ignorance: each provider's branch is discovered at
runtime and reported for what it is. The first live run against each deployment
should be recorded in the research corpus with a date and a URL, matching how
those documents already distinguish measurement from inference.

**The browser SSO session survives all three outcomes.** Revoking a refresh
token does not end the single-sign-on session the identity provider holds in the
browser, and on Asgardeo and Identity Server that session will complete a later
sign-in without prompting. A user who reads "session ended" as "the next login
will ask who I am" is wrong, so logout says so rather than leaving it to be
discovered. RP-initiated logout via `end_session_endpoint` would address this
and is deliberately out of scope: it needs a browser round-trip, which turns a
local command into an interactive flow.

**Ending a session ends it for every context on that identity.** A session is
keyed by the identity's `credentialRef`, so contexts sharing an identity share
one session; logout names the affected contexts rather than refusing. This is a
property of the session model, recorded under **Session** in
[`CONTEXT.md`](../../CONTEXT.md), and not of this decision.
[#43](https://github.com/wso2/wso2-cli/issues/43) would rekey sessions by
identity *and* product, at which point one logout ends several sessions and
several revocations may partly fail. The reporting shape chosen here is already
per-session for that reason, and #43 does not block this work.

**Revocation happens inside the session lock.** The refresh token must be read
before it is deleted, and releasing the lock in between opens a window where a
concurrent `wso2 login` writes a fresh session that logout then deletes,
ending a session the user just created. The network call is therefore bounded by
a deadline strictly shorter than the 45-second lock deadline in
[ADR 0007](0007-os-advisory-lock-for-session-rotation.md).
