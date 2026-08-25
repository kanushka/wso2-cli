# Logging in with the WSO2 CLI: Identity Server 7.x

This is the registration walkthrough for **WSO2 Identity Server 7.x**, one of
the three deployments `wso2 login` supports. Asgardeo and ThunderID have
[their](login-asgardeo.md) [own](login-thunder.md) walkthroughs, and everything
after registration is the same document for all three products: writing the
context document, logging in, CI, troubleshooting. Read this one for
the registration, then return to
[section 2 of the login guide](login.md#2-write-the-context-document).

**Measured against 7.3.0 on 2026-08-06.** Where this guide states what a
deployment does rather than what a control is called, that is the version it was
measured on.

---

## 1. What is different about Identity Server

Two facts here have consequences further down, so they are worth reading before
registering anything.

**An access token's `aud` carries the API resource identifier, but only once
the resource is in the application's Audience list.** Measured against 7.3.0:

| Application's **Audience** list | An access token's `aud` |
| --- | --- |
| empty | `"<client id>"` |
| `reference-status` | `["<client id>", "reference-status"]` |

So on Identity Server the API resource identifier *is* the right value for
`products.<namespace>.audience`. Leave the list empty and `aud` names the client
alone, exactly as on Asgardeo, which has no such list at all, and every
brokered acquisition refuses with `auth.narrowing_unavailable` naming the
audience. Section 7 is where you populate it.

**The Audience field sits under the application's ID token settings on both
Identity Server and Asgardeo, and does different work on each.** On Asgardeo
that list reaches the ID token only; here it reaches both. That is what makes
this easy to get wrong in either direction, and it is why an `audience` value is
not portable between the two products. What the difference costs is recorded in
[the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md):
the broker's audience check can distinguish one product from another here, and
cannot on Asgardeo.

The login guide
[lists everything else the shell needs](login.md#1-what-the-shell-needs-from-a-deployment):
a public client, mandatory S256 PKCE, the four loopback callbacks, the refresh
token grant, and JWT access tokens. The sections below configure all of it.

---

## 2. Run a deployment

The quickest deployment to register against is a container, which images have
published for arm64 as well as amd64 since 7.2.0:

```sh
docker run -d --name wso2is -p 9443:9443 -p 9763:9763 wso2/wso2is:7.3.0
```

It answers in about a minute, with `admin` / `admin`. Nothing is persisted
outside the container, so `docker rm -f wso2is` returns the machine to where it
started. That is the reason to prefer it to an unpacked distribution here,
where a half-registered application from a previous attempt is hard to tell from
a correct one.

An unpacked distribution works identically. Check
`repository/conf/deployment.toml` for `offset` before assuming the ports: a
deployment with `offset = 1` answers on 9444, and the issuer you record in
section 11 has to say so.

Everything below is in the Identity Server console, at
`https://localhost:9443/console` by default. All of it can also be done through
the management REST APIs, which accept the administrator's credentials over
basic auth: `POST /api/server/v1/api-resources`, `POST /api/server/v1/applications`,
`POST /api/server/v1/applications/{id}/authorized-apis`, `POST /scim2/Users`.
That is the better route when you expect to rebuild the deployment more than
once.

---

## 3. Trust the deployment's certificate

A default deployment serves a self-signed certificate, the shell uses the
process's ordinary HTTP client, and there is no flag anywhere in the shell for a
custom certificate authority. So until the certificate is in the OS trust store,
login cannot even reach discovery:

```text
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

On macOS, note that Go **ignores `SSL_CERT_FILE`**, since `crypto/x509` honors
it on every Unix except Darwin, so the keychain is the only way in. Take the
certificate from the port rather than out of a keystore. A container has no
keystore on your filesystem to read, and the port is in any case the only place
that answers what the deployment actually serves:

```sh
openssl s_client -connect localhost:9443 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > wso2carbon-localhost.pem

security add-trusted-cert -r trustRoot -p ssl \
  -k ~/Library/Keychains/login.keychain-db wso2carbon-localhost.pem
```

Use the port the deployment answers on, which is not 9443 if it carries an
offset. Against 7.3.0 this produces the same bytes as
`keytool -exportcert -alias wso2carbon -keystore repository/resources/security/wso2carbon.p12 -storepass wso2carbon`
from an unpacked distribution's root. Reach for that command if you need the
certificate before the deployment is running.

**Understand what that second command grants before running it.** The default
certificate is `CA:TRUE`, and its private key ships inside every Identity Server
download and every copy of the public container image, behind the published
password `wso2carbon`; the zip and the image serve a byte-identical
certificate. Trusting it as a root means trusting a signing key that anyone can
obtain, for any hostname, not just this deployment. `-p ssl` confines it to TLS
and the login keychain confines it to your user. Remove it when the runs are
done:

```sh
security delete-certificate -c localhost ~/Library/Keychains/login.keychain-db
```

The alternative, if that trade is not one you want to make even briefly, is to
replace the deployment's keypair with one whose private key only you hold.

Thunder's equivalent trade is narrower, because its certificate is generated
on the deployment that serves it, and Asgardeo needs none of this. See also
`auth.discovery_failed` in
[the login guide's troubleshooting](login.md#6-troubleshooting).

---

## 4. Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it `WSO2 CLI`. Protocol: **OpenID Connect**. Create.

---

## 5. Make it a public client with PKCE

On the application's **Protocol** tab:

1. **Allowed grant types**: **Code** and **Refresh Token** only. The refresh
   grant is what every later per-module acquisition narrows from; without it a
   login succeeds and no module can be granted anything.
2. **Public client** selected. This is what removes the client secret; the shell
   is installed on people's machines and cannot hold one.
3. **PKCE Mandatory** selected, PKCE 'Plain' unselected. The shell only offers
   `S256`, and allowing plain would weaken the flow without the shell ever using
   it.

---

## 6. Register the callback URLs

These are the ports the shell binds, in order, taking the first that is free:

```text
http://127.0.0.1:10425/callback
http://127.0.0.1:10426/callback
http://127.0.0.1:10427/callback
http://127.0.0.1:10428/callback
```

Either add the four individually, or use Identity Server's regex form as a
single entry:

```text
regexp=(http://127.0.0.1:10425/callback|http://127.0.0.1:10426/callback|http://127.0.0.1:10427/callback|http://127.0.0.1:10428/callback)
```

Identity Server waives the port when matching loopback redirect URIs from 6.0.0
onwards, so a single `http://127.0.0.1:10425/callback` entry is enough. Measured
on 7.3.0, the waiver is stronger than the documentation implies: a login through
`127.0.0.1:16000` completed against the regexp above, which enumerates four
ports and does not include that one. Loopback flexibility is applied ahead of
the registered pattern rather than as a fallback when none matches.

Register all four anyway. It keeps the same configuration valid on Asgardeo, and
it keeps the registration honest about which ports the shell actually binds.

---

## 7. Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes.

This is two screens, not one. An API resource is a server-level object that many
applications can share, so it is created outside your application; authorizing
it *for* your application is a separate step afterwards.

**First, create the resource.** **API Resources → New API Resource**.

1. Give it an **Identifier** and record it. This is the string a module's
   `audience` names. Unlike on Asgardeo, it is also what an issued token's
   `aud` will carry, once section 1's Audience list is populated below.
2. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`. Register at least two even when the module only
   uses one: the narrowing experiment works by asking for a strict subset of
   what a session carries, and it has nothing to measure against a single scope.
3. **Requires authorization** cannot be changed after the resource is created.
   Checked means these scopes only ever reach a token through a role. Section 9
   covers the role path, which is also the way out if you left it checked.

**Then authorize it on the application**, on the application's **API
Authorization** tab: select the resource, then select its scopes.

**Then add the audience, which is the step that makes the difference.** On the
application's **Protocol** tab, find **Audience** and add the API resource
identifier. Section 1 has the measurement: without this the token's `aud` names
the client alone and every brokered acquisition refuses.

---

## 8. Issue JWT access tokens

Identity Server issues JWT access tokens by default. If the deployment has been
changed to opaque, change it back for this application: an opaque access token
cannot be checked, and the broker refuses what it cannot check.

---

## 9. Create a user who can sign in, and grant it the scopes

**The account you sign in to the Console with is not, by default, an account
your application can authenticate.** Console access and application sign-in are
two different populations: the administrator account administers the server,
while what the application asks for is a user in the user store.

Create a user for this instead, under **User Management → Users**, and set its
password directly rather than emailing an invitation. The invitation path needs
a working inbox, and login waits only five minutes.

**If, and only if, section 7 left you with an authorization policy**, that
user also needs a role carrying the scopes. Authorizing the resource on the
application establishes what the application *may* ask for; under a policy it
does not establish what a user is *entitled to*, and the gap surfaces at the
first brokered acquisition as `auth.narrowing_unavailable` naming permissions.
Create a role with the application as its audience, attach the API resource and
select **every** scope the context document lists, not just the one a module
uses, because a session that carries less than it later asks for cannot be
narrowed. Then assign the user to it.

The reasoning is identical to Asgardeo's and only the console differs; if a
control is not where this says,
[the Asgardeo walkthrough](login-asgardeo.md#7-create-a-user-who-can-sign-in-and-grant-it-the-scopes)
names the equivalent screens in more detail.

A console change never reaches an existing session. Sign in again after either
step. Note that a browser SSO session will complete that sign-in without
showing you a login form, which is expected and does not mean the change was
skipped. Scopes are computed when a token is issued, not frozen into the browser
session.

---

## 10. A confidential client for CI, if you need one

A CI job has no browser and no secure store, so it uses a separate identity that
carries its own credential. Register a second application for it:

1. A standard-based application with the **Client Credentials** grant and **no**
   public-client setting.
2. No redirect URLs, no PKCE. There is no browser and no user.
3. Authorize the same API resource and scopes from section 7, and add the
   resource to this application's **Audience** list too.
4. Issue **JWT** access tokens, for the same reason as section 8.
5. Record the **client ID** and the **client secret**.

Under RBAC there is one further difference from a browser login. A
client-credentials grant has no user, so a role granting the scopes must be
assigned to the **application** rather than to a person.

[Section 5 of the login guide](login.md#5-ci-authenticate-without-a-login) has
the context document and the job wiring.

---

## 11. Record what you need

- **Client ID**.
- **Audience**, which is the **API resource identifier** from section 7, and
  which only reaches `aud` because you added it to the application's Audience
  list. This is not the same value as the client ID, and it is not what an
  Asgardeo deployment wants.
- **Scopes**, the ones you authorized on the application in section 7.
- **Issuer**, which for a default 7.x deployment takes the shape
  `https://localhost:9443/oauth2/token`. Confirm it from
  `https://localhost:9443/oauth2/token/.well-known/openid-configuration` and use
  the `issuer` value verbatim. A deployment carrying an offset answers on
  another port, and the port is part of the issuer.
- **Whether this machine trusts the deployment's certificate** (section 3).

---

## 12. Write the context document

Everything from here is [the main login guide](login.md), from section 2. An
Identity Server identity is `"type": "onprem"`, and its `audience` is the API
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

**Logging in without a browser** works on Identity Server 7.x: add the **Device
Code** grant to the application's allowed grant types, and nothing else in the
registration changes. See
[section 3.1 of the login guide](login.md#31-logging-in-without-a-browser).

---

## 13. Proving it against this deployment

The live runs in `test/smoke/` work against Identity Server exactly as they do
against the other two products. `test/smoke/env.example` carries an Identity
Server block; fill in what section 11 told you to record and see
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

`WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` **differ here**, where Asgardeo
forces them to the same value. Copying one deployment's env file to another and
changing only the issuer is therefore the mistake to expect; it costs a browser
sign-in and ends in `auth.narrowing_unavailable` naming the audience.

The measured behaviour behind everything above is recorded in
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
§3.1, with the date and the deployment each verdict came from.
