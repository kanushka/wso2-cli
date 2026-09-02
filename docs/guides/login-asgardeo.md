# Logging in with the WSO2 CLI: Asgardeo

Register the OAuth application `wso2 login` needs on Asgardeo. When you reach
the end, go to [section 2 of the login guide](login.md#2-the-context-document).
Identity Server and ThunderID have [their](login-identity-server.md)
[own](login-thunder.md) walkthroughs; everything after registration is the same
for all three.

Everything below happens in the Asgardeo console, for the organization you are
targeting.

> Measured against a live tenant on 2026-08-06. Asgardeo does not document the
> audience behaviour in section 1, so the date is part of the claim.

---

## 1. What is different about Asgardeo

**Asgardeo binds an access token's `aud` claim to the client ID, not to the API
resource whose scopes the token carries.** A token issued for
`reference:status:read reference:status:write`, from an application authorized
against the `reference-status` API resource, carried `"aud": "<client id>"` and
nothing else. There is no setting for this.

So `products.<namespace>.audience` in your context document must be the client
ID, not the API resource identifier. Otherwise every brokered acquisition
refuses with `auth.narrowing_unavailable`. Section 9 repeats this where you
record the value.

One consequence: the audience check cannot tell one product from another here.
It still proves a token was minted for this client, but no more. Identity Server
and Thunder both bind `aud` to the resource, so an `audience` value is never
portable between products
([research](../research/asgardeo-redirect-uri-and-scope-narrowing.md)).

[Section 5 of the login guide](login.md#5-what-the-shell-needs-from-a-deployment)
lists everything else the shell needs. The steps below configure all of it.

---

## 2. Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it something a user will recognize on a consent screen, such as
   `WSO2 CLI`.
3. Protocol: **OpenID Connect**.
4. Create.

---

## 3. Make it a public client with PKCE

On the application's **Protocol** tab:

1. Under **Allowed grant types**, select **Code** and **Refresh Token**, and
   clear everything else. Without the refresh grant, login succeeds and no
   module can be granted anything.
2. Select **Public client**. The shell is installed on people's machines and
   cannot hold a secret.
3. Under **PKCE**, select **Mandatory**, and leave "Support PKCE 'Plain'"
   unselected. The shell only offers `S256`.

---

## 4. Register the four callback URLs

Still on the **Protocol** tab, under **Authorized redirect URLs**, add all four:

```text
http://127.0.0.1:10425/callback
http://127.0.0.1:10426/callback
http://127.0.0.1:10427/callback
http://127.0.0.1:10428/callback
```

The shell binds these in order, taking the first free one.

Asgardeo does appear to waive the port when matching loopback redirect URIs, the
way RFC 8252 §7.3 asks: a login completed through `127.0.0.1:16000`, a port the
application had never registered. That was measured on one tenant, Asgardeo does
not document it, and it may change without notice, so do not build on it.
Register all four. The shell binds only these ports, so you gain nothing by
registering fewer, and against a deployment that does match exactly, any missing
entry becomes a redirect-mismatch error for whichever developer's machine has
that port busy.

---

## 5. Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes. This takes two screens: an
API resource is an organization-level object many applications can share, so you
create it outside your application and then authorize it for your application.

**First, create the resource.** API Resources is a top-level item in the left
navigation, a sibling of Applications rather than a tab inside it.

1. **API Resources → New API Resource**.
2. Give it an **Identifier** and record it. This is the string a module's
   `audience` names. It is not what lands in `aud` on Asgardeo (section 1).
3. Give it a **Display Name**, which is what a user sees on a consent screen.
4. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`. Register at least two even if the module uses one:
   the narrowing experiment asks for a strict subset of what a session carries,
   and it has nothing to measure against a single scope.
5. The wizard's last step offers **Requires authorization**, checked by default,
   and you cannot change it afterwards. Checked means these scopes only ever
   reach a token through a role. Clear it if the application's own authorization
   should be enough. Section 7 covers the role path.

**Then authorize it on the application.** In **Applications → your application →
Authorization → Authorize resource**, select the resource, then its scopes.

> Watch the policy shown beside the resource on that tab. It can read `Role
> Based Access Control (RBAC)` even when the resource did not require
> authorization. `No Authorization Policy` means the scopes selected here are
> enough on their own; anything else means section 7 applies, and skipping it
> gives you a login that succeeds followed by a refusal naming scopes.

---

## 6. Issue JWT access tokens

On the **Protocol** tab, under **Access Token**, set the token type to **JWT**.
An opaque access token cannot be checked, and the broker refuses what it cannot
check.

There is nothing to configure for the audience here. The **Access Token**
section offers only a token type and an attribute list, and the Audience field
nearby belongs to **ID Token**. On Identity Server 7.3.0 that same-looking list
does reach access tokens, so the control does different work on the two
products.

---

## 7. Create a user who can sign in, and grant it the scopes

The account you sign in to the Console with is not, by default, one your
application can authenticate. Your account administers the organization, while
the application asks for a user in the organization's user store. If you signed
up through Google or GitHub there is no password in that store at all.

1. **User Management → Users → Add User**, under *Users* rather than
   *Administrators*.
2. Give it a username or email, for example `cli-smoke@example.com`.
3. Choose to **set a password directly** rather than emailing an invitation. The
   invitation path needs a working inbox, and login waits only five minutes.

If, and only if, section 5 left you with an authorization policy, that user also
needs a role carrying the scopes. Authorizing the resource sets what the
application may ask for, not what a user is entitled to, and the gap shows up at
the first brokered acquisition as `auth.narrowing_unavailable`.

1. **Applications → your application → Roles → New Role**, with **Role Audience**
   set to **Application**.
2. Attach the API resource and select every scope the context document lists,
   not just the one a module uses. A session that carries less than it later
   asks for cannot be narrowed.
3. Assign the user to that role, from the role's users list or from
   **User Management → Users → your user → Roles**.

A console change never reaches an existing session, so sign in again afterwards.
A browser SSO session finishes that sign-in without showing a login form, which
is expected: scopes are computed when a token is issued, not frozen into the
browser session.

---

## 8. A machine-to-machine client for CI, if you need one

A CI job has no browser and no secure store, so it uses a separate identity that
carries its own credential. Register a second application:

1. **Applications → New Application → M2M Application**.
2. Grant types: **Client Credentials** only. No redirect URLs and no PKCE.
3. Authorize the same API resource and scopes from section 5.
4. Issue **JWT** access tokens, as in section 6.
5. Record the **client ID** and the **client secret**.

Its `audience` is the M2M application's own client ID, not the API resource
identifier. Under RBAC, a client-credentials grant has no user, so assign the
role granting the scopes to the **application** rather than to a person.

[Section 4 of the login guide](login.md#4-ci-authenticate-without-a-login) has
the context document and the job wiring.

---

## 9. Record what you need

| Value | Where it comes from |
| --- | --- |
| **Client ID** | The application from section 2. |
| **Audience** | The client ID again (section 1), not the API resource identifier. |
| **Scopes** | The ones you authorized in section 5. |
| **Issuer** | `https://api.asgardeo.io/t/<organization>/oauth2/token`. |
| **Endpoint** | The product API's base URL, `https://api.asgardeo.io` for a module served by Asgardeo. |

Confirm the issuer rather than assuming it. Fetch
`https://api.asgardeo.io/t/<organization>/oauth2/token/.well-known/openid-configuration`
and use the `issuer` value verbatim: the shell checks that the document belongs
to the issuer it was fetched from, so a value that is close but not exact fails
at login.

---

## 10. Log in, and check what it wrote

```console
$ wso2 login --url https://api.asgardeo.io/t/acme/oauth2/token \
    --client-id <client-id> --context acme
```

It reports the names it assigned, and `wso2 context list` shows them. What it
writes is spare: the issuer and client ID you passed, `"type": "onprem"`, a
`credentialRef` equal to the identity name, and no products.

**Record the product before running a product command.** Login stores no
product, so `wso2 reference status` fails with `auth.product_not_configured`
until you add one. On Asgardeo the audience is the client ID (section 1):

```console
$ wso2 identity add-product acme reference \
    --endpoint https://api.asgardeo.io \
    --audience <client-id> \
    --scopes reference:status:read,reference:status:write

Added product "reference" to identity "acme".
Identity   acme
Product    reference
Endpoint   https://api.asgardeo.io
Audience   <client-id>
Scopes     reference:status:read,reference:status:write
Replaced   no
```

Check what is recorded:

```console
$ wso2 identity list
IDENTITY   TYPE     ISSUER                                        PRODUCT     ENDPOINT                  SCOPES
acme       onprem   https://api.asgardeo.io/t/acme/oauth2/token   reference   https://api.asgardeo.io   reference:status:read,reference:status:write
```

The module's own commands work from here. The rest is
[the main login guide](login.md), from section 2.

The record below is the fuller shape, not what login leaves behind. Add products
with `wso2 identity add-product`, and set `tenant` and `"type": "cloud"` by hand
if you want them; no shell logic reads that field. Note the `audience` is the
client ID:

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
which is right on Identity Server. Thunder needs an absolute resource-server
URI, and Asgardeo needs the client ID, so that example is wrong here.

To log in without a browser, add the **Device Code** grant to the application's
allowed grant types; nothing else changes. See
[section 1.1 of the login guide](login.md#11-logging-in-without-a-browser).

---

## 11. Proving it against a live tenant

The live runs in `test/smoke/` work against Asgardeo, and `make
empirical-asgardeo` produced the verdicts cited above. `test/smoke/env.example`
carries an Asgardeo block: fill in what section 9 told you to record, and note
that `WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` are different fields that
Asgardeo happens to force to the same value. See
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

The measurements behind everything above are in
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
§3, with the date and deployment for each verdict.
