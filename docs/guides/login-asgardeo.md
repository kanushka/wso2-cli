# Logging in with the WSO2 CLI: Asgardeo

This is the registration walkthrough for **Asgardeo**, one of the three
deployments `wso2 login` supports. WSO2 Identity Server and ThunderID have
[their](login-identity-server.md) [own](login-thunder.md) walkthroughs, and
everything after registration is the same document for all three products:
writing the context document, logging in, CI, troubleshooting. Read this one for
the registration, then return to
[section 2 of the login guide](login.md#2-the-context-document).

**Measured against a live tenant on 2026-08-06.** The audience behaviour in
section 1 is the reason this guide exists as its own file rather than as a
variant of another product's; it is undocumented by Asgardeo, so the date is
part of the claim.

---

## 1. What is different about Asgardeo

One fact here has a consequence in every later section and in the context
document you will write, so it is worth reading before registering anything.

**Asgardeo binds an access token's `aud` claim to the client ID, not to the API
resource whose scopes the token carries.** Measured against a live tenant on
2026-08-06: a token issued for `reference:status:read reference:status:write`,
from an application authorized against the `reference-status` API resource,
carried `"aud": "<client id>"` and nothing else. There is no setting for this.

Two things follow:

- **`products.<namespace>.audience` in your context document must be the client
  ID**, not the API resource identifier, or every brokered acquisition refuses
  with `auth.narrowing_unavailable`. Section 9 says it again where you record
  the value.
- **The audience check cannot tell one product from another here.** It still
  proves a token was minted for this client, which is what it is for; it just
  cannot do more. Identity Server and Thunder both bind `aud` to the resource,
  so an `audience` value is not portable between products in either direction.
  The cost is recorded in
  [the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md).

The login guide
[lists everything else the shell needs](login.md#1-what-the-shell-needs-from-a-deployment):
a public client, mandatory S256 PKCE, the four loopback callbacks, the refresh
token grant, and JWT access tokens. The sections below configure all of it.

Everything that follows is in the Asgardeo console, for the organization you are
targeting.

---

## 2. Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it something a user will recognize in a consent screen, for example
   `WSO2 CLI`.
3. Protocol: **OpenID Connect**.
4. Create.

---

## 3. Make it a public client with PKCE

On the application's **Protocol** tab:

1. Under **Allowed grant types**, select **Code** and **Refresh Token**. Clear
   everything else. The refresh grant is what every later per-module acquisition
   narrows from; without it a login succeeds and no module can be granted
   anything.
2. Select **Public client**. This is what removes the client secret; the shell
   is installed on people's machines and cannot hold one.
3. Under **PKCE**, select **Mandatory**. Leave "Support PKCE 'Plain'"
   **unselected**. The shell only offers `S256`, and allowing plain would
   weaken the flow without the shell ever using it.

---

## 4. Register the four callback URLs

Still on the **Protocol** tab, under **Authorized redirect URLs**, add all four
of these, one at a time:

```text
http://127.0.0.1:10425/callback
http://127.0.0.1:10426/callback
http://127.0.0.1:10427/callback
http://127.0.0.1:10428/callback
```

These are the ports the shell binds, in order, taking the first that is free.
Asgardeo matches redirect URIs exactly by default, so a missing entry becomes a
mismatch error for whichever developer's machine happens to have that port busy.

Asgardeo does in fact waive the port when matching loopback redirect URIs, the
way Identity Server 6.0.0 and later document and as RFC 8252 §7.3 asks. That was
[measured against a live tenant on 2026-08-06](../research/asgardeo-redirect-uri-and-scope-narrowing.md):
a login completed through `127.0.0.1:16000`, a port the application did not
register.

Register all four anyway. The verdict was measured on one tenant, it is
undocumented by Asgardeo and so may change without notice, and the shell binds
only these four ports regardless, so nothing is gained by registering fewer,
and a deployment that stops waiving the port breaks every developer whose first
choice is busy.

---

## 5. Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes.

This is two screens, not one. An API resource is an organization-level object
that many applications can share, so it is created outside your application;
authorizing it *for* your application is a separate step afterwards.

**First, create the resource.** **API Resources** is a top-level item in the
Console's left navigation, a sibling of Applications rather than a tab inside
the one you just made.

1. **API Resources → New API Resource**.
2. Give it an **Identifier** and record it. This is the string a module's
   `audience` names. It is *not* what lands in an issued token's `aud` claim on
   Asgardeo; see section 1.
3. Give it a **Display Name**. This is what a user sees on a consent screen.
4. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`. Register at least two even when the module only
   uses one: the narrowing experiment works by asking for a strict subset of
   what a session carries, and it has nothing to measure against a single scope.
5. The wizard's last step offers **Requires authorization**, checked by default.
   **This field cannot be changed after the resource is created.** Checked means
   these scopes only ever reach a token through a role. Clear it if you want the
   application's own authorization to be enough on its own. Section 7 covers the
   role path, which is also the way out if you left it checked.

**Then authorize it on the application.** Back in **Applications → your
application → Authorization → Authorize resource**: select the resource, then
select its scopes.

Watch the policy shown beside the resource on that tab. It can read
`Role Based Access Control (RBAC)` even when the resource itself did not require
authorization. The resource setting decides whether a policy is *mandatory*,
and this tab is where one is actually chosen. `No Authorization Policy` means
the scopes selected here are sufficient by themselves. Anything else means
section 7 applies, and skipping it produces a login that succeeds followed by a
refusal that names scopes rather than roles.

---

## 6. Issue JWT access tokens

On the application's **Protocol** tab, under **Access Token**, set the token type
to **JWT**. An opaque access token cannot be checked, and the broker refuses
what it cannot check.

There is nothing to configure for the audience here, and the control that looks
like it does is not one. The **Access Token** section offers only a token type
and an attribute list; the Audience field you will find nearby belongs to **ID
Token** and does not affect access tokens. Section 1 has the measurement.

That field sitting under the application's ID token settings is what makes this
easy to get wrong in the other direction: on Identity Server 7.3.0 the
same-looking list reaches access tokens too, so the same control does different
work on the two products.

---

## 7. Create a user who can sign in, and grant it the scopes

**The account you sign in to the Console with is not, by default, an account
your application can authenticate.** Console access and application sign-in are
two different populations: your own account administers the organization, while
what the application asks for is a user in the organization's user store. If you
signed up through Google or GitHub there is no password in that store at all,
and no amount of typing your real one will work.

Create a user for this instead:

1. **User Management → Users → Add User**, under *Users* rather than
   *Administrators*.
2. Give it a username or email, for example `cli-smoke@example.com`.
3. Choose to **set a password directly** rather than emailing an invitation. The
   invitation path needs a working inbox, and login waits only five minutes.

**If, and only if, section 5 left you with an authorization policy**, that
user also needs a role carrying the scopes. Authorizing the resource on the
application establishes what the application *may* ask for; under a policy it
does not establish what a user is *entitled to*, and the gap surfaces at the
first brokered acquisition as `auth.narrowing_unavailable` naming permissions.

1. **Applications → your application → Roles → New Role**, with **Role Audience**
   set to **Application**.
2. Attach the API resource and select **every** scope the context document
   lists, not just the one a module uses. A session that carries less than it
   later asks for cannot be narrowed.
3. Assign the user to that role, from the role's users list or from
   **User Management → Users → your user → Roles**.

A console change never reaches an existing session. Sign in again after either
step. Note that a browser SSO session will complete that sign-in without
showing you a login form, which is expected and does not mean the change was
skipped. Scopes are computed when a token is issued, not frozen into the browser
session.

---

## 8. A machine-to-machine client for CI, if you need one

A CI job has no browser and no secure store, so it uses a separate identity that
carries its own credential. Register a second application for it:

1. **Applications → New Application → M2M Application**.
2. Grant types: **Client Credentials** only. No redirect URLs, no PKCE, since
   there is no browser and no user.
3. Authorize the same API resource and scopes from section 5.
4. Issue **JWT** access tokens, for the same reason as section 6.
5. Record the **client ID** and the **client secret**.

Its `audience` follows the same rule as everything else here: the **M2M
application's own client ID**, not the API resource identifier. Under RBAC there
is one further difference from a browser login. A client-credentials grant has
no user, so a role granting the scopes must be assigned to the **application**
rather than to a person.

[Section 5 of the login guide](login.md#5-ci-authenticate-without-a-login) has
the context document and the job wiring.

---

## 9. Record what you need

- **Client ID**.
- **Audience**, which on Asgardeo is **the client ID again** (section 1), not
  the API resource identifier.
- **Scopes**, the ones you authorized on the application in section 5.
- **Issuer**, which for Asgardeo takes the shape
  `https://api.asgardeo.io/t/<organization>/oauth2/token`. Confirm it rather
  than assuming it. Fetch
  `https://api.asgardeo.io/t/<organization>/oauth2/token/.well-known/openid-configuration`
  and use the `issuer` value verbatim. The shell discovers the token endpoint
  from that document and checks that the document belongs to the issuer it was
  fetched from, so a value that is close but not exact fails at login.

---

## 10. Log in, and check what it wrote

With the issuer and client ID from section 9, one command creates the identity
and the context and signs you in:

```console
$ wso2 login --url https://api.asgardeo.io/t/acme/oauth2/token \
    --client-id <client-id> --context acme
```

It reports the names it assigned, and `wso2 context list` shows them.
What it writes is deliberately spare: the issuer and client ID you passed,
`"type": "onprem"`, a `credentialRef` equal to the identity name, and no
products. Everything from here is [the main login guide](login.md), from
section 2.

The record below is the fuller shape, not what login leaves: add products with
`wso2 identity add-product`, and set `tenant` and `"type": "cloud"` by hand if
you want them — an Asgardeo identity is conceptually cloud, and no shell logic
reads the field. Its `audience` is the client ID:

```json
{
  "name": "acme-cloud",
  "type": "cloud",
  "auth": {
    "kind": "oauth-browser",
    "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
    "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
    "tenant": "acme",
    "credentialRef": "acme-cloud-login"
  },
  "products": {
    "reference": {
      "endpoint": "https://api.asgardeo.io",
      "audience": "REPLACE_WITH_YOUR_CLIENT_ID",
      "scopes": ["reference:status:read"]
    }
  }
}
```

The example in the login guide shows the resource-identifier form of `audience`,
which is right on Identity Server and Thunder and wrong here.

**Logging in without a browser** works on Asgardeo: add the **Device Code**
grant to the application's allowed grant types, and nothing else in the
registration changes. See
[section 3.1 of the login guide](login.md#31-logging-in-without-a-browser).

---

## 11. Proving it against a live tenant

The live runs in `test/smoke/` work against Asgardeo, and the two one-time
experiments behind `make empirical-asgardeo` are what produced the verdicts
cited above. `test/smoke/env.example` carries an Asgardeo block; fill in what
section 9 told you to record. Note that `WSO2_SMOKE_CLIENT_ID` and
`WSO2_SMOKE_AUDIENCE` are different fields that Asgardeo happens to force to the
same value. See [`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

The measured behaviour behind everything above is recorded in
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
§3, with the date and the deployment each verdict came from.
