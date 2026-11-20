# ADR 0025: Device-Code Sign-In at ThunderID, and a Chosen Way to Product Sessions

**Status:** Accepted

This amends ADR 0022 and ADR 0024 for ThunderID contexts, WSO2 Cloud's
among them. It records five decisions: how a context says it signs in, what
a device-code sign-in at ThunderID asks for, how a product gets its session
after the sign-in, what happens when that way is not available, and how a
device-code context switches organization and signs out.

## A context's kind is its sign-in method

The sign-in method is the context's existing `login.kind`: `oauth-browser`
signs in in a browser on this machine, and `oauth-device` signs in with a
device code. A device code is the OAuth 2.0 Device Authorization Grant (RFC
8628): the shell asks the identity provider for two codes, prints the short
user code and the address of the provider's verification page, and the person
types the code on that page from any device with a browser, while the shell
polls the token endpoint until the provider answers with tokens. The shell
never opens a browser for it and needs no loopback callback, so it works over
SSH, in a container, and on a machine with no browser at all.

No new member says the same thing a second time. Personal access tokens stay
reserved (`pat`, refused with `auth.kind_not_implemented`), and a service
account is the existing `client-credentials` kind, which has no login.

`ws login --device` writes `oauth-device` on a context that login creates:
the WSO2 Cloud first run under `WSO2_CLOUD_ENV`, a login with `--url`, and
the setup wizard. Each records the kind only once the sign-in has succeeded,
so a provider that refuses the device code leaves a context that still signs
in. On a context that exists, the kind decides, and `--device` on one that
signs in another way is refused with `shell.invalid_argument`, naming
`ws context edit` and the member to change, before a WSO2 Cloud login
installs or records anything for it. A flag that overrode the
kind for one login would leave the next one signing in differently with
nothing on record to say why.

## A device-code sign-in at ThunderID asks what the browser sign-in asks

Before this, the device-code login sent no resource indicator and never asked
for a device secret, because no ThunderID served the grant. ThunderID now
does (thunder-id/thunderid#5909), so a device-code sign-in on a ThunderID
context asks for exactly what the browser sign-in asks for: the login
product's scopes, one RFC 8707 resource indicator (the `resource` parameter,
which names the API, its audience, that the tokens are for), and `device_sso`
when the provider's discovery document lists it. ThunderID answers with an
access token bound to that resource, an identity token, a refresh token, and
a device secret, as it does for the authorization code. The session is
stored the same way, marked `establishedBy: device-code`.

The terminal shows what it showed before, on standard error: the
verification address, the user code on a line of its own, and the complete
address that fills the code in, which ThunderID still asks the person to
confirm. No QR code is drawn: RFC 8628 makes one optional, it does not survive
being copied from a log or a narrow terminal, and it would add a dependency.
A provider that answers `unauthorized_client` is told apart from other
refusals: the OAuth application is not registered for the grant, so the
recovery names `kind: oauth-browser` rather than a retry.

`--no-input` and `WSO2_NO_INPUT` still refuse a device-code sign-in with
`auth.non_interactive`, as ADR 0023 has them refuse a browser sign-in: the
flag means nothing waits for a person, and a device code waits up to ten
minutes for one. Standard input that is not a terminal does not refuse it;
the instructions are on standard error for an agent to relay, as the browser
sign-in's address is.

## Products get their sessions the way `login.productSessions` says

A ThunderID context's products after the sign-in each need a session of
their own (ADR 0022). `login.productSessions` says how they get it:

- `exchange`: each is obtained by
  token exchange (RFC 8693, trading one credential for another at the token
  endpoint) of the sign-in's device secret, with no browser, during
  `ws login` and the first time a command needs the product. This is the
  same whichever kind the context signs in with: a device-code sign-in
  yields a device secret just as a browser one does.
- `browser`: each is authorized in the browser on this machine, chained in
  one tab as ADR 0022 describes (after a browser sign-in, the sign-in's own
  tab), and the sign-in asks for no device secret, since nothing would
  present it.

A context that states neither has one recorded the first time it is needed,
by its first login or the first command that needs a product: `exchange` where
the provider's discovery document lists `device_sso`, and on WSO2 Cloud,
whose provider does; `browser` otherwise, with one plain line saying so ("This
sign-in service can't share a sign-in between products, so each product signs
in through the browser. Change it with wso2 context edit."). It is decided
once and written to the context file; from then on the recorded value decides,
and a value a person stated is never changed. This is not a fallback at run
time: it is a default chosen from what the provider says it offers, before
anything is tried.

There is no third value and no choice at run time. In particular a ThunderID
context has no device code per product: a person who signed in with one code
types no second one. The setting is read on ThunderID contexts only; another
provider's products are authorized as before.

## No silent fallback

Before this, a product whose device secret was missing, expired, refused by
the provider, or answered in another organization was authorized in the
browser instead, without being asked, latterly with one plain line saying
so. Under `exchange` that is now refused
with `auth.device_secret_unavailable`, naming why, and every recovery names
`productSessions: browser` beside the way on that fits the reason. A secret
that expired or whose sign-on ended is discarded, and signing in again issues
a new one. When the sign-in kept none, the refusal says which of two things
happened, since a plain "sign in again" would loop in both: a provider that
does not offer `device_sso` (an explicit `exchange` there cannot work), or one
that handed out none, likely because the browser was already signed in, for
which the way on is `ws logout` then `ws login`. A recovery for a context other
than the selected one names it with `--context`, as ADR 0023 has refusals
name runnable commands. A device-code context never opens a browser
for a product it was not told to, which on a machine with no browser would have
failed anyway and on one with a browser would have been a surprise.

A login whose later product is refused keeps what it established, as before,
and does not offer `--only` as a retry, since that presents the same secret.

## What changes path on its own says so

Where the shell still takes another path without being asked, it says so
in one plain line on standard error, with no error code, address or protocol
term; the detail is for `--verbose`. A provider that asks for slower polling
gets "The sign-in service asked the shell to check less often, so it is
waiting a little longer between checks.", once per sign-in, and a product
whose session comes from the device secret gets "Authorized the "<product>"
product from this sign-in, without the browser."

The shell polls one second later than the interval the provider advertises.
ThunderID measures the interval between arrivals with no tolerance, so polls
on the exact interval arrive a few milliseconds early after ordinary network
jitter and draw `slow_down`, which would print the line above on most
sign-ins.

## Organizations and sign-out on a device-code context

ThunderID serves the device grant only at its plain endpoints: there is no
`/ou/<id>/oauth2/device-authorize`, and its organization-scoped token
endpoint refuses the device-code grant. So the sign-in cannot be sent to an
organization as ADR 0024 sends every other authorization. It lands in the
organization the verification page's sign-in chooses, and is checked
afterwards: a context that records an organization refuses a sign-in issued
in another with `auth.organization_mismatch`, revoking it, as ADR 0024 already
does for any session. Exchanges of the device secret still go to the recorded
organization's own token endpoint, which ThunderID allows.

`ws org use <id>` on a device-code Cloud context ends the sessions as ADR 0024
describes, tells the person which organization to choose on the page, and
records it only if the sign-in was issued there; otherwise the previous
organization stays recorded. With no argument it records the organization the
sign-in was issued in. ThunderID shows its organization picker only on a full
sign-in, so the instructions print the provider's sign-out address to open on
that device first when the page does not ask. Only the organization already
signed in to has a known name or handle, so the one to choose is named by id.

`ws logout` on a device-code context revokes and deletes every session as
before, and prints each provider's sign-out address instead of opening it: the
provider's browser session is on whichever device approved the code, not on
this machine. Printing asks nothing of anyone, so it happens under
`--no-input` too, and under `--output json` the addresses are in the report
as `signOutUrls`.

## Follow-up: a sign-in an agent can finish across calls

An agent's tool call usually times out in a minute or two, well before a
person types a code. A later change could split the sign-in in two: one
command that starts it and prints the verification address and user code as
JSON, and one that waits for the approval, so an agent can relay the code and
come back. It is not built here.

## Evidence

Read on 2026-11-19 from ThunderID's source and measured the same day
against the WSO2 Cloud development environment, as the public client the
shell registers there.

- The device grant: `POST /oauth2/device-authorize` with `client_id`,
  `scope` and `resource`, answering `device_code`, `user_code`,
  `verification_uri`, `verification_uri_complete` (the code as a prefill,
  still to be submitted), `expires_in` 600 and `interval` 5. Polling answers
  `authorization_pending`, `slow_down` (adding five seconds), `access_denied`,
  `expired_token` and `invalid_grant`, and a code is redeemed once.
- The token answer carries an identity token when `openid` is asked, a
  refresh token when the client may use `refresh_token`, and a device secret
  when `device_sso` was granted and the approval kept a sign-on.
- Since 2026-11-19 the development environment issues a device secret on a
  sign-in that reuses the browser's sign-on too, which ADR 0022 measured it
  not doing on 2026-11-13; it was confirmed live by authorizing a product from
  the secret with the browser already signed in. An environment not yet on
  that build still issues none on a reused sign-on, which is the case the
  "handed out no shared sign-in" refusal names.
- A client whose registration does not list the grant is answered
  `unauthorized_client`. The organization-scoped token endpoint refuses every
  grant but client credentials, the authorization code and token exchange;
  there is no organization-scoped device authorization endpoint.
- The development environment advertises `device_authorization_endpoint`,
  the grant and `device_sso`, but answers the shell's client
  `unauthorized_client`, with or without a resource, since the client is not
  yet registered for the grant. A device-code sign-in on WSO2 Cloud needs
  that registration.

Measured on 2026-11-20 against a local ThunderID built from its main branch
(3f32a54), with a device-code context whose second product is reached by the
exchange.

- A device approval issues a device secret and the second product is
  authorized from it with no browser, once the client's authentication flow
  keeps an SSO session. ThunderID's default authentication flow keeps none
  (it has no session step), and then no sign-in, device or browser, returns a
  device secret; the shell refused the second product with
  `auth.device_secret_unavailable` and opened nothing. WSO2 Cloud's
  authentication flow keeps one, and its Device Grant flow must call that
  flow.
- Polling on the exact advertised interval drew `slow_down` on both sign-ins
  tried; with the one-second margin, a sign-in that polled for half a minute
  drew none. Against the test issuer, a poll on the exact interval arrived
  0.1 ms short of it.

## Considered Options

- **A `signIn` member beside `kind`.** It would say what `kind` already says,
  and two members for one fact can disagree.
- **`ws login --device` as a per-login override.** Rejected for the reason
  above; the context file is where the setting lives for now.
- **A device code per product when there is no secret.** It works with no
  browser, but asks a person to type one code per product, and the product
  sessions setting has no such value.
- **Keeping the automatic fallback to the browser.** A path the person did
  not choose, taken without saying so, is the hardest to debug; a refusal
  that names the setting costs one edit and is never a surprise.
- **Letting `--no-input` allow a device-code sign-in.** It is the natural
  headless path, but the flag's promise is that nothing waits for a person,
  which is what CI relies on.
