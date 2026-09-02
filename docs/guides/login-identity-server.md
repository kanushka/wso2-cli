# Logging in with the WSO2 CLI: Identity Server 7.x

Register the OAuth application `wso2 login` needs on WSO2 Identity Server 7.x.
When you reach the end, go to
[section 2 of the login guide](login.md#2-the-context-document). Asgardeo and
ThunderID have [their](login-asgardeo.md) [own](login-thunder.md) walkthroughs;
everything after registration is the same for all three.

> Measured against 7.3.0 on 2026-08-06. Where this guide says what a deployment
> does, rather than what a control is called, that is the version behind it.

---

## 1. What is different about Identity Server

**An access token's `aud` carries the API resource identifier, but only once the
resource is in the application's Audience list.**

| Application's **Audience** list | An access token's `aud` |
| --- | --- |
| empty | `"<client id>"` |
| `reference-status` | `["<client id>", "reference-status"]` |

So the API resource identifier is the right value for
`products.<namespace>.audience` here. Leave the list empty and `aud` names the
client alone, the way Asgardeo does, and every brokered acquisition refuses with
`auth.narrowing_unavailable`. Section 7 is where you fill it in.

The Audience field sits under the application's ID token settings on both
Identity Server and Asgardeo and does different work on each: on Asgardeo it
reaches the ID token only, and here it reaches both. That is why an `audience`
value is never portable between the two
([research](../research/asgardeo-redirect-uri-and-scope-narrowing.md)).

[Section 5 of the login guide](login.md#5-what-the-shell-needs-from-a-deployment)
lists everything else the shell needs. The steps below configure all of it.

---

## 2. Run a deployment

The quickest deployment to register against is a container, published for arm64
as well as amd64 since 7.2.0:

```sh
docker run -d --name wso2is -p 9443:9443 -p 9763:9763 wso2/wso2is:7.3.0
```

It answers in about a minute, with `admin` / `admin`. Nothing persists outside
the container, so `docker rm -f wso2is` returns the machine to where it started.
That matters while you are learning the console, where a half-registered
application from a previous attempt is hard to tell from a correct one.

An unpacked distribution works the same way. Check
`repository/conf/deployment.toml` for `offset` before assuming the ports: with
`offset = 1` it answers on 9444, and the issuer you record in section 11 has to
say so.

The console is at `https://localhost:9443/console` by default. All of this can
also be done through the management REST APIs, which take the administrator's
credentials over basic auth (`POST /api/server/v1/api-resources`, `POST
/api/server/v1/applications`, `POST
/api/server/v1/applications/{id}/authorized-apis`, `POST /scim2/Users`). Take
that route if you expect to rebuild the deployment more than once.

---

## 3. Trust the deployment's certificate

A default deployment serves a self-signed certificate, and the shell has no flag
for a custom certificate authority, so until the certificate is in the OS trust
store login cannot even reach discovery:

```text
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

Take the certificate from the port rather than from a keystore: a container has
no keystore on your filesystem, and the port is the only place that answers with
what the deployment actually serves. On macOS, Go ignores `SSL_CERT_FILE`, so
the keychain is the only way in.

```sh
openssl s_client -connect localhost:9443 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > wso2carbon-localhost.pem

security add-trusted-cert -r trustRoot -p ssl \
  -k ~/Library/Keychains/login.keychain-db wso2carbon-localhost.pem
```

Use the port the deployment answers on, which is not 9443 if it carries an
offset.

> **Understand what the second command grants.** The default certificate is
> `CA:TRUE`, and its private key ships inside every Identity Server download and
> every copy of the public container image, behind the published password
> `wso2carbon`. Trusting it as a root means trusting a signing key anyone can
> obtain, for any hostname. `-p ssl` confines it to TLS and the login keychain
> confines it to your user. If you do not want that trade even briefly, replace
> the deployment's keypair with one whose private key only you hold.

Remove it when the runs are done:

```sh
security delete-certificate -c localhost ~/Library/Keychains/login.keychain-db
```

---

## 4. Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it `WSO2 CLI`. Protocol: **OpenID Connect**. Create.

---

## 5. Make it a public client with PKCE

On the application's **Protocol** tab:

1. **Allowed grant types**: **Code** and **Refresh Token** only. Without the
   refresh grant, login succeeds and no module can be granted anything.
2. **Public client** selected. The shell is installed on people's machines and
   cannot hold a secret.
3. **PKCE Mandatory** selected, PKCE 'Plain' unselected. The shell only offers
   `S256`.

---

## 6. Register the callback URLs

```text
http://127.0.0.1:10425/callback
http://127.0.0.1:10426/callback
http://127.0.0.1:10427/callback
http://127.0.0.1:10428/callback
```

The shell binds these in order, taking the first free one. Add them
individually, or use Identity Server's regex form as a single entry:

```text
regexp=(http://127.0.0.1:10425/callback|http://127.0.0.1:10426/callback|http://127.0.0.1:10427/callback|http://127.0.0.1:10428/callback)
```

Identity Server waives the port for loopback redirect URIs from 6.0.0 onwards,
so one entry is technically enough, and measured on 7.3.0 the waiver goes
further than documented: a login through `127.0.0.1:16000` completed against the
regexp above, which does not list that port. Register all four anyway. It keeps
the same configuration valid on Asgardeo and honest about which ports the shell
binds.

---

## 7. Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes. This takes two screens: an
API resource is a server-level object many applications can share, so you create
it outside your application and then authorize it for your application.

**First, create the resource**, under **API Resources → New API Resource**.

1. Give it an **Identifier** and record it. This is the string a module's
   `audience` names, and unlike on Asgardeo it is also what an issued token's
   `aud` carries, once the Audience list below is populated.
2. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`. Register at least two even if the module uses one:
   the narrowing experiment asks for a strict subset of what a session carries,
   and it has nothing to measure against a single scope.
3. **Requires authorization** cannot be changed after the resource is created.
   Checked means these scopes only ever reach a token through a role. Section 9
   covers the role path.

**Then authorize it on the application**, on the **API Authorization** tab:
select the resource, then its scopes.

**Then add the audience**, which is the step that makes the difference. On the
**Protocol** tab, find **Audience** and add the API resource identifier. Without
it the token's `aud` names the client alone and every brokered acquisition
refuses (section 1).

---

## 8. Issue JWT access tokens

Identity Server issues JWT access tokens by default. If the deployment has been
switched to opaque, switch it back for this application: an opaque access token
cannot be checked, and the broker refuses what it cannot check.

---

## 9. Create a user who can sign in, and grant it the scopes

The account you sign in to the Console with is not, by default, one your
application can authenticate. The administrator account administers the server,
while the application asks for a user in the user store.

Create one under **User Management → Users**, and set its password directly
rather than emailing an invitation. The invitation path needs a working inbox,
and login waits only five minutes.

If, and only if, section 7 left you with an authorization policy, that user also
needs a role carrying the scopes. Authorizing the resource sets what the
application may ask for, not what a user is entitled to, and the gap shows up at
the first brokered acquisition as `auth.narrowing_unavailable`. Create a role
with the application as its audience, attach the API resource, select every
scope the context document lists, and assign the user to it. A session that
carries less than it later asks for cannot be narrowed.

The reasoning matches Asgardeo's and only the console differs; if a control is
not where this says,
[the Asgardeo walkthrough](login-asgardeo.md#7-create-a-user-who-can-sign-in-and-grant-it-the-scopes)
names the equivalent screens in more detail.

A console change never reaches an existing session, so sign in again afterwards.
A browser SSO session finishes that sign-in without showing a login form, which
is expected: scopes are computed when a token is issued, not frozen into the
browser session.

---

## 10. A confidential client for CI, if you need one

A CI job has no browser and no secure store, so it uses a separate identity that
carries its own credential. Register a second application:

1. A standard-based application with the **Client Credentials** grant and no
   public-client setting.
2. No redirect URLs and no PKCE.
3. Authorize the same API resource and scopes from section 7, and add the
   resource to this application's **Audience** list too.
4. Issue **JWT** access tokens, as in section 8.
5. Record the **client ID** and the **client secret**.

Under RBAC, a client-credentials grant has no user, so assign the role granting
the scopes to the **application** rather than to a person.

[Section 4 of the login guide](login.md#4-ci-authenticate-without-a-login) has
the context document and the job wiring.

---

## 11. Record what you need

| Value | Where it comes from |
| --- | --- |
| **Client ID** | The application from section 4. |
| **Audience** | The API resource identifier from section 7, which reaches `aud` only because you added it to the Audience list. Not the client ID. |
| **Scopes** | The ones you authorized in section 7. |
| **Issuer** | `https://localhost:9443/oauth2/token` on a default 7.x deployment. |
| **Endpoint** | The product API's base URL, `https://localhost:9443` for a module served by this deployment. |

Confirm the issuer from
`https://localhost:9443/oauth2/token/.well-known/openid-configuration` and use
the `issuer` value verbatim. A deployment carrying an offset answers on another
port, and the port is part of the issuer.

Also confirm this machine trusts the deployment's certificate (section 3).

---

## 12. Log in, and check what it wrote

```console
$ wso2 login --url https://localhost:9443/oauth2/token \
    --client-id <client-id> --context is-local
```

Use the issuer you recorded in section 11, which carries the deployment's own
port. It reports the names it assigned, and `wso2 context list` shows them. What
it writes is spare: the issuer and client ID you passed, `"type": "onprem"`, a
`credentialRef` equal to the identity name, and no products.

**Record the product before running a product command.** Login stores no
product, so `wso2 reference status` fails with `auth.product_not_configured`
until you add one. Here the audience is the API resource identifier (section 1):

```console
$ wso2 identity add-product is-local reference \
    --endpoint https://localhost:9443 \
    --audience reference-status \
    --scopes reference:status:read,reference:status:write

Added product "reference" to identity "is-local".
Identity   is-local
Product    reference
Endpoint   https://localhost:9443
Audience   reference-status
Scopes     reference:status:read,reference:status:write
Replaced   no
```

Check what is recorded:

```console
$ wso2 identity list
IDENTITY   TYPE     ISSUER                                PRODUCT     ENDPOINT                 SCOPES
is-local   onprem   https://localhost:9443/oauth2/token   reference   https://localhost:9443   reference:status:read,reference:status:write
```

The module's own commands work from here. The rest is
[the main login guide](login.md), from section 2.

The record below is the fuller shape, not what login leaves behind. Add products
with `wso2 identity add-product`. An Identity Server identity is
`"type": "onprem"`, which login already writes, and its `audience` is the API
resource identifier:

```json
{
  "name": "is-local",
  "type": "onprem",
  "auth": {
    "kind": "oauth-browser",
    "issuer": "https://localhost:9443/oauth2/token",
    "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
    "credentialRef": "is-local-login"
  },
  "products": {
    "reference": {
      "endpoint": "https://localhost:9443",
      "audience": "reference-status",
      "scopes": ["reference:status:read"]
    }
  }
}
```

To log in without a browser, add the **Device Code** grant to the application's
allowed grant types; nothing else changes. See
[section 1.1 of the login guide](login.md#11-logging-in-without-a-browser).

---

## 13. Proving it against this deployment

The live runs in `test/smoke/` work against Identity Server the same way they do
against the other two products. `test/smoke/env.example` carries an Identity
Server block: fill in what section 11 told you to record, and see
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

`WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` differ here, where Asgardeo
forces them to the same value. So the mistake to expect is copying one
deployment's env file and changing only the issuer: it costs a browser sign-in
and ends in `auth.narrowing_unavailable`.

The measurements behind everything above are in
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
§3.1, with the date and deployment for each verdict.
