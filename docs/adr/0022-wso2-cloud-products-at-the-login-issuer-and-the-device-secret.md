# ADR 0022: WSO2 Cloud Products Reached at the Login Issuer, from the Device Secret When There Is One

**Status:** Accepted

This amends ADR 0014 for WSO2 Cloud, whose identity provider is ThunderID,
and records four decisions.

## A WSO2 Cloud product is reached by its own authorization

On a WSO2 Cloud context — one whose `type` is `cloud` and whose login names
the `thunder` provider — every product is reached by an authorization of its
own at the login issuer. An authorization is one grant of access by the
provider: here an OAuth authorization request that names one resource server
with a resource indicator (RFC 8707's `resource` parameter, which says which
API the token is for) and asks for that product's scopes, the permissions the
token may carry. The token it yields is bound to that resource server, its
audience, and carries exactly the product's scopes. These are ADR 0014's
`direct` and `sibling` strategies.

The grant a module's product descriptor names is set aside on such a context.
ADR 0014's `exchanged` strategy, which trades the login session's access
token for one bound to the product's audience under RFC 8693 token exchange,
is not used, because on WSO2 Cloud that exchange never carries a permission
scope: the token that comes back can name the product's audience but cannot
do anything there. A grant a person states in a context file is still
recorded as stated, as on any other context, so a team that knows its
deployment differs can say so.

The shell also derives each known product's URL and resource server from the
environment it signs in to, and records every installed one on the context
when it logs in, so nothing has to be typed. API Management, Integration and
Agent Management share one resource server, `urn:wso2:cloud`, each with a
scope tree of its own; Identity is its own resource server, named by its API
URL. A record already on the context, written by a login or by a person, is
never changed.

## A ThunderID context may record no product

A ThunderID context may hold a login and no product. Its login then names no
resource server, and ThunderID binds it to the deployment's default one,
which carries no product scope. Such a context is how a WSO2 Cloud login
starts on a machine with no module installed; it signs in and holds a session,
and reaches products once they are recorded. Before this, a ThunderID context
had to record a product for its login to bind to, because a local ThunderID
with no default resource server refuses an authorization without a resource
indicator. That refusal still happens there, and the shell still reports it.

## A product session comes from the device secret when the sign-on issued one

ThunderID can describe the sign-on a sign-in started with a device
secret: a signed token the provider issues beside the login's tokens when the
authorization request asks for the `device_sso` scope. Presented in a token
exchange, with the secret as the subject token, one resource indicator and a
scope set, it yields a fresh authorization for that resource, with a refresh
token of its own, exactly as a second browser authorization would, and opens
no browser. A refresh token is the long-lived credential a session keeps to
obtain new access tokens without the person.

So the login authorization asks for `device_sso` when the context's provider
is ThunderID and the provider's discovery document lists the scope as
supported. A device secret in the answer is kept in the OS secure store with
the login session, never in the context file, never printed, logged, or given
to a module. Each further product session at the login issuer — the
`sibling` sessions — is then obtained by exchanging the secret, both during
`wso2 login` and the first time a command needs a product that has no
session; the latter needs no browser, so it works under `--no-input`. The
session is stored, renewed and proved exactly like one a browser authorization
produced: its token is checked to be bound to the product's audience before it
is stored, and scopes the provider left out are named. The strategy stays
`sibling`; `wso2 whoami -o json` says `establishedBy: device-secret` for such
a session.

A missing or expired secret, or one the provider refuses as no longer usable
(`interaction_required`, `invalid_grant` or `invalid_request`), is discarded,
and the product is authorized in the browser as before, which `--no-input`
refuses as before. Any other refusal is reported. ThunderID issues a secret
only when the sign-in started a new sign-on, so a person already
signed in to the provider in their browser gets none, and their login runs
one browser authorization per product, in one tab, as the next decision
describes.

## The browser authorizations of one login share one tab

A login that authorizes several products in the browser opens the browser
once. The shell's loopback callback for each authorization but the last is
answered with a redirect to the next one's authorization URL, with its own
PKCE verifier, state and nonce, and `prompt=none`, the OpenID Connect request
not to show the person anything; only the last callback renders the
signed-in page, naming every product whose authorization completed. A
provider that refuses a silent authorization gets the same authorization
again, once, with no `prompt`, in the same tab. The usual refusals are
`login_required`, `interaction_required`, `consent_required` and
`account_selection_required`, but any refusal but `invalid_target` is retried,
because this applies to every provider, and a proxy that rejects `prompt=none`
with, say, `invalid_request` must not break a login that worked with one tab
per product. A tab the browser closed, or one no authorization followed for a
minute, ends, and the next authorization opens the browser again, with a
prompt.

Before this, each authorization opened a tab of its own, each ending on its
own signed-in page, so a person already signed in to WSO2 Cloud saw one tab
per product for nothing they had to do. A silent authorization in a hidden
frame, which is how a web page does this, needs a page to host the frame, and
the shell has none; the redirect chain is the same thing in the one tab the
login already has. The device secret stays the first choice: when there is
one, no product after the first reaches the browser at all, and the chain
covers the sign-on a person already had, for which ThunderID issues none.
Nothing about this is specific to WSO2 Cloud, so every browser login with
more than one authorization uses it. A product's first use by a command still
opens a tab of its own, since nothing came before it.

`wso2 logout` revokes each product session's refresh token itself and deletes
every session entry, and the secret with the login session, before it opens
the provider's sign-out page. It already did so before this decision; the
point is now load-bearing, because a sign-out at the provider ends only the
client's latest grant and leaves earlier authorizations in the same browser
session able to refresh.

## Evidence

Measured on 2026-11-13 against the WSO2 Cloud development environment, as the
public client the shell registers there.

- Token exchange with an access token as the subject changes the audience and
  carries no permission scope. A refresh token is fixed to one audience and one
  scope set.
- A separate authorization per product, each naming its own resource and
  scopes, passed with `prompt=none` and no password for four products, one of
  them under a different audience. With no sign-on, `prompt=none`
  answers `login_required`. Each authorization gets its own refresh token;
  they all refresh side by side, and replaying a rotated refresh token ends
  only that authorization's token family.
- Scopes the user's roles do not carry, and unknown scopes, are left out of
  the issued token with a 200. An authorization with no resource indicator is
  bound to a default resource server and carries no product scope.
- Through the shell, a refresh of the API Management session returned exactly
  the scopes asked for, on two commands in a row.
- With `device_sso` asked for, the authorization-code response carries
  `device_secret`, a provider-signed token typed `sso-session+jwt` whose
  audience is the issuer and which expires after 24 hours. It is issued only
  when the sign-in started a new sign-on; with one already present
  the response has none, even with `prompt=login` or `max_age=0`. The
  discovery document lists `device_sso` in `scopes_supported`.
- Exchanging the secret with `subject_token_type`
  `urn:ietf:params:oauth:token-type:jwt`, one `resource` and a `scope` answers
  with an access token and a refresh token bound to that resource, carrying
  the requested scopes the user's roles allow, and no identity token. Without
  `resource` the token is bound to the default resource server with no
  scopes; the exchange's `audience` parameter is ignored. Several exchanges
  live side by side, each with its own token family.
- Once the sign-on has ended, by sign-out or timeout, the exchange
  answers `interaction_required`; a secret from another user answers
  `invalid_grant`. Sign-out ends the secret at once but revokes only the
  client's latest grant.

## Considered Options

- **Token exchange from the login session for every product** (ADR 0014's
  `exchanged`). No browser for any product, and no session to store, but on
  WSO2 Cloud the token carries no permission, so no product command can
  succeed.
- **One authorization asking for every product's scopes.** ThunderID binds an
  authorization to one resource server, and WSO2 Cloud's Identity product has
  a resource server of its own, so this cannot serve it; it would also hand
  every module a token carrying every product's scopes, which ADR 0014
  rejects.
- **A browser authorization per product only, with no device secret.** It
  works everywhere and is what a reused sign-on falls back to, but it cannot
  serve a product's first use under `--no-input`.
- **A tab per browser authorization.** What the shell did before; each tab
  ended on its own signed-in page, which a person already signed in had
  nothing to do in.
- **Keeping the device secret in its own secure-store entry.** It would
  outlive a login session replaced by the next sign-in, and need its own
  deletion at logout; kept with the login session, every path that ends that
  session ends the secret too.

## Amendment (ADR 0024)

A WSO2 Cloud context's `organization` names the ThunderID organization unit
its sessions are signed in to, and every authorization for it goes to that
organization's own endpoint. A session records the scopes the provider
granted, and the broker serves those rather than refusing for the ones the
provider left out.
