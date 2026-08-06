# Logging in with the WSO2 CLI

**Status:** Working draft
**Last reviewed:** 2026-08-06
**Related:** [Architecture](../architecture.md),
[product requirements](../product-requirements.md),
[shell commands](../reference/commands.md),
[authentication context examples](../examples/authentication-contexts.md)

This guide takes you from an empty deployment to a working `wso2 login` and a CI
job that authenticates without one. It is written to be read on its own: every
value you need to produce is produced here, and nothing below asks you to go and
read another document first.

Two audiences, one path. Sections 2 and 3 are alternatives — register the
application in Asgardeo **or** in Identity Server 7.x, whichever you are
targeting — and everything after them is the same for both.

Registering against **ThunderID** is a third alternative, and it lives in
[its own walkthrough](login-thunder.md) because Thunder decides an access
token's audience differently enough to change what you register and what you
write down. Read that instead of sections 2 and 3, then rejoin this guide at
section 4.

---

## 1. What the shell needs from a deployment

The shell signs a person in with the browser Authorization Code flow and PKCE,
keeps the resulting refresh token in the operating system's secure store, and
derives a separate short-lived access token for each module that asks for one.
Nothing else is stored, and no module ever sees the session.

That design imposes five requirements on the application you are about to
register. Each one has a section below; this list is here so you know what the
clicking is for.

1. **A public client.** No client secret. The shell is installed on people's
   machines, so it cannot hold one, and PKCE is what replaces it.
2. **PKCE, mandatory, S256.** The shell refuses to start a login against an
   issuer that does not advertise `S256` in its discovery document.
3. **Four loopback callback URLs.** The shell listens on `127.0.0.1` and takes
   the first free port of four, so all four must be registered:

   ```
   http://127.0.0.1:10425/callback
   http://127.0.0.1:10426/callback
   http://127.0.0.1:10427/callback
   http://127.0.0.1:10428/callback
   ```

   Four rather than one so that a developer whose first choice is busy — the
   port is in the IANA dynamic range and something else may hold it — still
   lands on a registered redirect instead of a mismatch error.
4. **The refresh token grant.** The session the shell stores *is* the refresh
   token. Without this grant, login succeeds and every later command fails.
5. **An API resource with scopes, and JWT access tokens.** The shell proves that
   the token a module receives carries exactly the permissions that module asked
   for and is bound to the audience it asked for. It cannot prove that about an
   opaque token, and it refuses rather than hand over a grant it could not
   check. See `auth.narrowing_unavailable` in section 8.

---

## 2. Register the application in Asgardeo

In the Asgardeo console, for the organization you are targeting.

### 2.1 Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it something a user will recognize in a consent screen, for example
   `WSO2 CLI`.
3. Protocol: **OpenID Connect**.
4. Create.

### 2.2 Make it a public client with PKCE

On the application's **Protocol** tab:

1. Under **Allowed grant types**, select **Code** and **Refresh Token**. Clear
   everything else.
2. Select **Public client**. This is what removes the client secret; the shell
   cannot present one.
3. Under **PKCE**, select **Mandatory**. Leave "Support PKCE 'Plain'"
   **unselected** — the shell only offers `S256`, and allowing plain would
   weaken the flow without the shell ever using it.

### 2.3 Register the four callback URLs

Still on the **Protocol** tab, under **Authorized redirect URLs**, add all four
of the URLs listed in section 1.3, one at a time. Asgardeo matches redirect URIs
exactly by default, so a missing entry becomes a mismatch error for whichever
developer's machine happens to have that port busy.

Asgardeo does in fact waive the port when matching loopback redirect URIs, the
way Identity Server 6.0.0 and later document and as RFC 8252 §7.3 asks. That was
[measured against a live tenant on 2026-08-06](../research/asgardeo-redirect-uri-and-scope-narrowing.md):
a login completed through `127.0.0.1:16000`, a port the application did not
register.

Register all four anyway. The verdict was measured on one tenant, it is
undocumented by Asgardeo and so may change without notice, and the shell binds
only these four ports regardless — so nothing is gained by registering fewer,
and a deployment that stops waiving the port breaks every developer whose first
choice is busy.

### 2.4 Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes.

This is two screens, not one. An API resource is an organization-level object
that many applications can share, so it is created outside your application;
authorizing it *for* your application is a separate step afterwards.

**First, create the resource.** **API Resources** is a top-level item in the
Console's left navigation — a sibling of Applications, not a tab inside the one
you just made.

1. **API Resources → New API Resource**.
2. Give it an **Identifier** and record it. This is the string a module's
   `audience` names. Read section 2.5 before assuming it is also what lands in
   an issued token's `aud` claim on Asgardeo — it is not.
3. Give it a **Display Name**. This is what a user sees on a consent screen.
4. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`. Register at least two even when the module only
   uses one: the narrowing experiment in section 9 works by asking for a strict
   subset of what a session carries, and it has nothing to measure against a
   single scope.
5. The wizard's last step offers **Requires authorization**, checked by default.
   **This field cannot be changed after the resource is created.** Checked means
   these scopes only ever reach a token through a role. Clear it if you want the
   application's own authorization to be enough on its own. Section 2.7 covers
   the role path, which is also the way out if you left it checked.

**Then authorize it on the application.** Back in **Applications → your
application → Authorization → Authorize resource**: select the resource, then
select its scopes.

Watch the policy shown beside the resource on that tab. It can read
`Role Based Access Control (RBAC)` even when the resource itself did not require
authorization — the resource setting decides whether a policy is *mandatory*,
and this tab is where one is actually chosen. `No Authorization Policy` means
the scopes selected here are sufficient by themselves. Anything else means
section 2.7 applies, and skipping it produces a login that succeeds followed by
a refusal that names scopes rather than roles.

### 2.5 Issue JWT access tokens, and know what `aud` will say

On the application's **Protocol** tab, under **Access Token**, set the token type
to **JWT**. An opaque access token cannot be checked, and the broker refuses
what it cannot check.

**Asgardeo binds an access token's `aud` claim to the client ID, not to the API
resource whose scopes the token carries.** Measured against a live tenant on
2026-08-06: a token issued for `reference:status:read reference:status:write`,
from an application authorized against the `reference-status` API resource,
carried `"aud": "<client id>"` and nothing else. There is no setting for this.
The **Access Token** section offers only a token type and an attribute list; the
Audience field you will find nearby belongs to **ID Token** and does not affect
access tokens.

So on Asgardeo, `products.<namespace>.audience` in your context document must be
**the client ID**, not the API resource identifier, or every brokered
acquisition refuses with `auth.narrowing_unavailable`. Section 4.3 says the same
where the field is defined, and the consequence is recorded in
[the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md):
the audience check still proves a token was minted for this client, but it
cannot distinguish one product from another.

Identity Server 7.3.0 does **not** behave this way — the same-looking Audience
field there reaches access tokens too, so the resource identifier is the right
value on that product. Section 3.5 has the measurement. Do not carry an
Asgardeo `audience` over to an Identity Server deployment, or the reverse.

### 2.6 Record what you need

From the **Protocol** and **Info** tabs:

- **Client ID**.
- **Issuer**, which for Asgardeo takes the shape
  `https://api.asgardeo.io/t/<organization>/oauth2/token`. Confirm it rather
  than assuming it — fetch
  `https://api.asgardeo.io/t/<organization>/oauth2/token/.well-known/openid-configuration`
  and use the `issuer` value verbatim. The shell discovers the token endpoint
  from that document and checks that the document belongs to the issuer it was
  fetched from, so a value that is close but not exact fails at login.

### 2.7 Create a user who can sign in, and grant it the scopes

**The account you sign in to the Console with is not, by default, an account
your application can authenticate.** Console access and application sign-in are
two different populations: your own account administers the organization, while
what the application asks for is a user in the organization's user store. If you
signed up through Google or GitHub there is no password in that store at all,
and no amount of typing your real one will work.

Create a user for this instead:

1. **User Management → Users → Add User** — *Users*, not *Administrators*.
2. Give it a username or email, for example `cli-smoke@example.com`.
3. Choose to **set a password directly** rather than emailing an invitation. The
   invitation path needs a working inbox, and login waits only five minutes.

**If — and only if — section 2.4 left you with an authorization policy**, that
user also needs a role carrying the scopes. Authorizing the resource on the
application establishes what the application *may* ask for; under a policy it
does not establish what a user is *entitled to*, and the gap surfaces at the
first brokered acquisition as `auth.narrowing_unavailable` naming permissions.

1. **Applications → your application → Roles → New Role**, with **Role Audience**
   set to **Application**.
2. Attach the API resource and select **every** scope the context document
   lists, not just the one a module uses — a session that carries less than it
   later asks for cannot be narrowed.
3. Assign the user to that role, from the role's users list or from
   **User Management → Users → your user → Roles**.

A console change never reaches an existing session. Sign in again after either
step — and note that a browser SSO session will complete that sign-in without
showing you a login form, which is expected and does not mean the change was
skipped. Scopes are computed when a token is issued, not frozen into the browser
session.

---

## 3. Register the application in Identity Server 7.x

The quickest deployment to register against is a container, which images have
published for arm64 as well as amd64 since 7.2.0:

```sh
docker run -d --name wso2is -p 9443:9443 -p 9763:9763 wso2/wso2is:7.3.0
```

It answers in about a minute, with `admin` / `admin`. Nothing is persisted
outside the container, so `docker rm -f wso2is` returns the machine to where it
started — which is the reason to prefer it to an unpacked distribution for this,
where a half-registered application from a previous attempt is hard to tell from
a correct one.

An unpacked distribution works identically. Check `repository/conf/deployment.toml`
for `offset` before assuming the ports: a deployment with `offset = 1` answers
on 9444, and section 3.6 is where that matters.

Everything below is in the Identity Server console, at
`https://localhost:9443/console` by default. All of it can also be done through
the management REST APIs, which accept the administrator's credentials over
basic auth — `POST /api/server/v1/api-resources`, `POST /api/server/v1/applications`,
`POST /api/server/v1/applications/{id}/authorized-apis`, `POST /scim2/Users`.
That is the better route when you expect to rebuild the deployment more than
once.

### 3.1 Create the application

1. **Applications → New Application → Standard-Based Application**.
2. Name it `WSO2 CLI`. Protocol: **OpenID Connect**. Create.

### 3.2 Make it a public client with PKCE

On the **Protocol** tab: grant types **Code** and **Refresh Token** only;
**Public client** selected; **PKCE Mandatory** selected; PKCE 'Plain'
unselected. Same reasoning as section 2.2.

### 3.3 Register the callback URLs

Either add the same four URLs from section 1.3 individually, or use Identity
Server's regex form as a single entry:

```
regexp=(http://127.0.0.1:10425/callback|http://127.0.0.1:10426/callback|http://127.0.0.1:10427/callback|http://127.0.0.1:10428/callback)
```

Identity Server waives the port when matching loopback redirect URIs from 6.0.0
onwards, so a single `http://127.0.0.1:10425/callback` entry is enough there.
Measured on 7.3.0, the waiver is stronger than the documentation implies: a
login through `127.0.0.1:16000` completed against the regexp above, which
enumerates four ports and does not include that one. Loopback flexibility is
applied ahead of the registered pattern rather than as a fallback when none
matches.

Register all four anyway. It keeps the same configuration valid on Asgardeo, and
it keeps the registration honest about which ports the shell actually binds.

### 3.4 Add the API resource and its scopes

**API Resources → New API Resource**, with an identifier and scopes as in
section 2.4, then authorize it on the application's **API Authorization** tab.
Section 2.4's two warnings apply here too: the resource is created on a
different screen than the one that authorizes it, and the **Requires
authorization** setting cannot be changed afterwards.

### 3.5 Issue JWT access tokens, and add the audience

Identity Server issues JWT access tokens by default. If the deployment has been
changed to opaque, change it back for this application — see section 2.5 for
why.

**Identity Server does not behave like Asgardeo here, and this is the step that
makes the difference.** Measured against 7.3.0 on 2026-08-06:

| Application's **Audience** list | An access token's `aud` |
| --- | --- |
| empty | `"<client id>"` |
| `reference-status` | `["<client id>", "reference-status"]` |

So on Identity Server the API resource identifier *is* the right value for
`products.<namespace>.audience` — but only once it is in that list. Leave the
list empty and `aud` names the client alone, exactly as on Asgardeo, and every
brokered acquisition refuses with `auth.narrowing_unavailable` naming the
audience.

On the application's **Protocol** tab, find **Audience** and add the API
resource identifier from section 3.4.

The field sits under the application's ID token settings on both products, which
is what makes this easy to get wrong in the other direction: on Asgardeo that
list reaches the ID token only, and on Identity Server 7.3.0 it reaches both.
The same-looking control does different work. Section 2.5 records the Asgardeo
side, and
[the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
records what the difference costs — the broker's audience check can distinguish
one product from another on Identity Server, and cannot on Asgardeo.

### 3.6 Record what you need

- **Client ID**.
- **Issuer**, which for a default Identity Server 7.x deployment takes the shape
  `https://localhost:9443/oauth2/token`. Confirm it from
  `https://localhost:9443/oauth2/token/.well-known/openid-configuration` and use
  the `issuer` value verbatim.
- **Whether this machine trusts the deployment's TLS certificate.** A default
  deployment serves a self-signed one, the shell uses the process's ordinary
  HTTP client, and there is no flag anywhere in the shell for a custom CA. So
  until the certificate is in the OS trust store, login cannot even reach
  discovery:

  ```
  tls: failed to verify certificate: x509: certificate signed by unknown authority
  ```

  On macOS, note that Go **ignores `SSL_CERT_FILE`** — `crypto/x509` honors it
  on every Unix except Darwin — so the keychain is the only way in. Take the
  certificate from the port rather than out of a keystore. A container has no
  keystore on your filesystem to read, and the port is in any case the only
  place that answers what the deployment actually serves:

  ```sh
  openssl s_client -connect localhost:9443 -servername localhost </dev/null 2>/dev/null \
    | openssl x509 -outform pem > wso2carbon-localhost.pem

  security add-trusted-cert -r trustRoot -p ssl \
    -k ~/Library/Keychains/login.keychain-db wso2carbon-localhost.pem
  ```

  Use the port the deployment answers on, which is not 9443 if it carries an
  offset. Against 7.3.0 this produces the same bytes as
  `keytool -exportcert -alias wso2carbon -keystore repository/resources/security/wso2carbon.p12 -storepass wso2carbon`
  from an unpacked distribution's root — that is the command to reach for if you
  need the certificate before the deployment is running.

  **Understand what that second command grants before running it.** The default
  certificate is `CA:TRUE`, and its private key ships inside every Identity
  Server download and every copy of the public container image, behind the
  published password `wso2carbon` — the zip and the image serve a byte-identical
  certificate. Trusting it as a root means trusting a signing key that anyone
  can obtain, for any hostname, not just this deployment. `-p ssl` confines it
  to TLS and the login keychain confines it to your user. Remove it when the
  runs are done:

  ```sh
  security delete-certificate -c localhost ~/Library/Keychains/login.keychain-db
  ```

  The alternative, if that trade is not one you want to make even briefly, is to
  replace the deployment's keypair with one whose private key only you hold.

  See also `auth.discovery_failed` in section 8.

You also need a user to sign in as, and possibly a role granting the scopes.
Section 2.7 describes both; the reasoning is identical on Identity Server, only
the console differs.

---

## 4. Write the context document

The shell reads contexts and never writes them, so this file is authored by
hand.

### 4.1 Where it goes

```
~/.wso2/cli/contexts.json
```

Set `WSO2_HOME` to use a different state root; it must be an absolute path, and
the file then lives at `$WSO2_HOME/cli/contexts.json`.

```sh
mkdir -p ~/.wso2/cli
```

### 4.2 What it says

A context document names **identities** — how to authenticate, and what each
identity may reach — and **contexts**, which select an identity and the
organization to act within.

Copy this, then replace the four values marked in the comments below it:

```json
{
  "schemaVersion": 2,
  "defaultContext": "acme-dev",
  "identities": [
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
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ],
  "contexts": [
    {
      "name": "acme-dev",
      "identity": "acme-cloud",
      "organization": "acme"
    }
  ]
}
```

Replace:

- `issuer` — the value you confirmed in section 2.6 or 3.6.
- `clientId` — the client ID you recorded.
- `audience` — **on Asgardeo, the client ID again**, because that is the only
  value Asgardeo puts in an access token's `aud` claim (section 2.5). On a
  deployment that binds tokens to API resources, the resource identifier from
  section 2.4 or 3.4. The example above shows the resource-identifier form, so
  against Asgardeo it needs the client ID substituted here.
- `scopes` — the scopes you authorized on the application.

For an Identity Server deployment, also set `"type": "onprem"` and use the
`https://localhost:9443/oauth2/token` issuer shape.

### 4.3 What each field means

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Must be `2`. |
| `defaultContext` | The context used when no `--context` flag and no `WSO2_CONTEXT` is given. Must name a context declared below. |
| `identities[].name` | Lower-case letters, digits and dashes, starting with a letter, up to 64 characters. |
| `identities[].type` | `cloud` or `onprem`. Nothing else is accepted. |
| `auth.kind` | `oauth-browser` for a person at a browser. `client-credentials` for CI — see section 7. `oauth-device` and `pat` are named by the schema but not implemented in this release. |
| `auth.issuer` | The issuer, verbatim from its discovery document. |
| `auth.clientId` | The registered public client. |
| `auth.tenant` | The identity's home organization. |
| `auth.credentialRef` | The name the session is stored under in the OS secure store. **Required** for `oauth-browser`; **not allowed** for `client-credentials`. Same character rules as an identity name. |
| `products.<namespace>` | What this identity may reach for one module. The namespace is the module's own name, and follows the same character rules as an identity name. |
| `products.<namespace>.endpoint` | The product's base URL. **Required** on every product entry, and must be an absolute `http` or `https` URL with a host. |
| `products.<namespace>.audience` | What the issued token's `aud` claim must carry. A module asking for any other audience is refused. Conceptually this is the API resource identifier — but on Asgardeo it must be **the client ID**, because that is the only thing Asgardeo puts in `aud`. See section 2.5. |
| `products.<namespace>.scopes` | The permissions this identity carries. A module asking for one that is not listed is refused. |
| `contexts[].organization` | The organization to act within. Either leave it out, or set it to the identity's `auth.tenant` — this release cannot switch a session out of its home tenant, and any other value is refused. See `auth.organization_switch_unsupported` in section 8. |

### 4.4 Check it

```sh
wso2 login --context acme-dev
```

If the document is malformed, the shell says so before opening any browser.

---

## 5. Log in

```sh
wso2 login
```

or, to name a context other than the default:

```sh
wso2 login --context acme-dev
```

What happens, in order:

1. The shell reads the issuer's discovery document and confirms it advertises
   `S256`.
2. It binds the first free port of 10425-10428 on `127.0.0.1`.
3. It prints the authorization URL to standard error and opens your browser at
   it. If no browser can be opened, the printed URL is the whole fallback: open
   it yourself, on this machine.
4. You sign in and consent.
5. The browser is redirected back to the loopback listener, and the shell
   exchanges the code — with the PKCE verifier — for tokens.
6. It verifies the identity token, including the nonce it sent.
7. It writes the refresh token to the operating system's secure store and
   reports who you are.

On a machine with no browser at all — a remote shell, a container — set
`WSO2_NO_BROWSER=1`. The shell then prints the URL and does not attempt to open
anything. You still have to complete the sign-in in a browser that can reach
`127.0.0.1` **on this machine**, so this helps with a missing browser, not with
a missing desktop.

The command waits up to five minutes for you.

---

## 6. What login stored, and where

- **The refresh token** goes to the operating system's secure store — Keychain
  on macOS, Secret Service on Linux, Credential Manager on Windows — under the
  service `wso2-cli` and the name you put in `credentialRef`.
- **Nothing under `~/.wso2` holds a credential.** The state root holds the
  context document you wrote, the managed module store, and the advisory lock
  files that keep refresh-token rotation single-writer. No session material is
  ever written there — not the refresh token, not an access token, not a client
  secret.
- **Modules never receive the session.** When a module needs access, the shell
  exchanges the refresh token for a fresh, short-lived access token narrowed to
  exactly the permissions that module declared, proves the result carries what
  was asked for, and hands over only that.

To sign out, delete the secure-store entry named by `credentialRef`.
(`wso2 logout` is a proposed command and is not in this release.)

---

## 7. CI: authenticate without a login

A CI job has no browser and no secure store, so it does not use a session at
all. It uses a machine-to-machine identity that carries its own credential and
exchanges it inline, on every command. **There is no login step in CI** — a job
that runs `wso2 login` is refused with `auth.login_not_required`.

### 7.1 Register the machine-to-machine application

In Asgardeo: **Applications → New Application → M2M Application**. In Identity
Server: a standard-based application with the **Client Credentials** grant and
**no** public-client setting.

Then, for either:

- Grant types: **Client Credentials** only. No redirect URLs, no PKCE — there is
  no browser and no user.
- Authorize the same API resource and scopes from section 2.4 or 3.4.
- Issue **JWT** access tokens, for the same reason as section 2.5.
- Record the **client ID** and the **client secret**.

### 7.2 Write the CI context

```json
{
  "schemaVersion": 2,
  "defaultContext": "acme-ci",
  "identities": [
    {
      "name": "acme-machine",
      "type": "cloud",
      "auth": {
        "kind": "client-credentials",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_M2M_CLIENT_ID",
        "tenant": "acme",
        "clientSecretVariable": "WSO2_ACME_CI_SECRET"
      },
      "products": {
        "reference": {
          "endpoint": "https://api.asgardeo.io",
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ],
  "contexts": [
    {
      "name": "acme-ci",
      "identity": "acme-machine",
      "organization": "acme"
    }
  ]
}
```

Two differences from section 4.2, and the schema enforces both:

- `clientSecretVariable` **replaces** `credentialRef`. It names an environment
  variable; it is not the secret. Upper-case letters, digits and underscores,
  starting with a letter.
- `credentialRef` must **not** appear on a `client-credentials` identity, and
  `clientSecretVariable` must **not** appear on an `oauth-browser` one.

The secret itself never goes in this file, and the file is safe to commit.

`audience` follows the same rule as section 4.2: on Asgardeo it must be the M2M
application's own client ID, not the API resource identifier the example shows.
Under RBAC there is one further difference from a browser login — a
client-credentials grant has no user, so a role granting the scopes must be
assigned to the **application** rather than to a person.

### 7.3 Wire the job

The secret comes from the CI system's own secret store into the named variable.
Nothing else changes; there is no login step.

```yaml
# GitHub Actions
jobs:
  status:
    runs-on: ubuntu-latest
    env:
      WSO2_HOME: ${{ github.workspace }}/.wso2
      WSO2_CONTEXT: acme-ci
      WSO2_ACME_CI_SECRET: ${{ secrets.WSO2_ACME_CI_SECRET }}
    steps:
      - uses: actions/checkout@v4
      - name: Install the context document
        run: |
          mkdir -p "$WSO2_HOME/cli"
          cp ci/contexts.json "$WSO2_HOME/cli/contexts.json"
      - name: Check the shell resolves its context
        run: wso2 version
      - name: Run a product command
        run: wso2 reference status
```

**A caveat about that last step, so it does not surprise you.** `wso2 reference
status` is the example module this repository ships, and `wso2` dispatches any
namespace it does not own itself to an installed module. Module *installation*
commands (`wso2 module install`) are proposed and are not in this release, so
the module has to already be in the managed module store under
`$WSO2_HOME/cli/modules` for that step to resolve; otherwise it exits with
`shell.unknown_command`. The authentication half of this section — the identity,
the secret variable, and the inline grant — is complete and works today, and
`wso2 version` exercises the context resolution without needing a module.

`WSO2_HOME` must be absolute. `WSO2_CONTEXT` selects the context without a flag.

Also set `WSO2_NON_INTERACTIVE=1` on any job where a stray `wso2 login` should
fail loudly rather than sit waiting on a browser that will never open.

Each command exchanges the client secret for an access token narrowed to what
the module asked for. The secret is read into process memory for the length of
one grant, is never written to the state root, and is never passed to a module.

---

## 8. Troubleshooting

Every refusal the shell makes carries a typed code. Find yours here.

### The context document: `contexts.*`

These come from the file you wrote in section 4, and they are the ones a
first-time user meets most often. None of them reaches a browser.

- **`contexts.document_malformed`** — the document was read but is not valid.
  The message names the specific defect, and section 4.3 is the field-by-field
  reference for it. The usual causes are a name that breaks the character rules
  (identity names, context names and `credentialRef` are lower-case letters,
  digits and dashes, starting with a letter), a `type` that is not exactly
  `cloud` or `onprem`, a missing `endpoint` on a product entry, or the
  `credentialRef` / `clientSecretVariable` rule: exactly one of them belongs on
  an identity, and which one is decided by `auth.kind`.
- **`contexts.document_unreadable`** — the file exists but could not be read.
  Check its permissions, or delete it to run without a context.
- **`contexts.schema_unsupported`** — `schemaVersion` is not one this shell
  reads. It must be `2`.
- **`contexts.unknown_context`** — you named a context, with `--context` or
  `WSO2_CONTEXT`, that the document does not declare. The message names the one
  you asked for. Check it against the `contexts` array and `defaultContext`.

### `auth.context_not_selected`

There is no context document at all, or it declares no context to select.
Create `~/.wso2/cli/contexts.json` as in section 4.

### `shell.unknown_command`

The first word was not `help`, `login` or `version`, and no installed module
owns that namespace. See the caveat at the end of section 7.3.

### `auth.discovery_failed`

> the shell could not read the identity provider's OpenID configuration

The issuer in your context document could not be read, or what came back was not
usable. In order of likelihood:

- **The issuer is not exact.** It must equal the `issuer` value in the
  deployment's own discovery document, character for character. Fetch
  `<issuer>/.well-known/openid-configuration` and compare.
- **TLS is not trusted.** Common against a local Identity Server with a
  self-signed certificate. Add the deployment's certificate to the operating
  system trust store. The shell deliberately has no flag to skip verification.
- **The machine cannot reach the issuer.** Proxy, VPN, firewall.
- **The issuer does not advertise `S256`.** Set PKCE to mandatory on the
  application, as in section 2.2.

There is a second, differently worded `auth.discovery_failed`:

> no loopback callback port is available for the browser login

All four of 10425-10428 are in use. Find the holders and free one:

```sh
lsof -nP -iTCP@127.0.0.1:10425-10428 -sTCP:LISTEN   # macOS, Linux
```

The shell will not fall back to an unregistered port, because the deployment
would reject the redirect and the error would name the wrong problem.

### `auth.login_required`

No usable session. Either you have not logged in for this `credentialRef`, or
the deployment has stopped accepting the stored refresh token — it was revoked,
it expired, or it was rotated away by a concurrent run. Run `wso2 login` again.

### `auth.keyring_unavailable`

The operating system's secure store could not be used. On a headless Linux
machine this usually means no Secret Service is running; start a keyring daemon,
or use a `client-credentials` context (section 7), which needs no secure store
at all.

### `auth.narrowing_unavailable`

> the deployment ... the permissions the "reference" module asked for

The shell obtained a token but could not prove it was narrowed to what the
module asked for, so it refused to hand it over. **This refusal is the designed
behavior, not a degraded mode** — a module that silently received the whole
session's authority would hold access nobody decided to give it.

The message tells you which of five things happened:

| The message says | What it means | What to change |
| --- | --- | --- |
| "in a form the shell cannot check" | The access token is opaque. | Set the application to issue JWT access tokens (section 2.5). |
| "did not state which permissions it issued" | The deployment returned no scope, and the token claims none. | Check the API resource is authorized on the application with the scopes selected. |
| "asked for the permissions X and the deployment issued Y" | The deployment ignored the narrower request and issued something else. | The deployment does not narrow on this grant. See below. |
| "is not bound to the ... audience" | The token's `aud` does not carry your audience. | The `audience` in your context document names something the deployment never puts in `aud`. Which value that is differs by product: the **client ID** on Asgardeo (section 2.5), the **API resource identifier** on Identity Server, and there only once it is in the application's audience list (section 3.5). Failing that, the resource is not authorized on the application. |
| "refused to narrow this session" | The token endpoint answered `invalid_scope`. | A scope in your context document is not one the application is authorized for. |

The middle case — a deployment that will not narrow — is a property of the
deployment, not something to work around in the shell. Both supported products
do narrow: measured on 2026-08-06 against a live Asgardeo tenant and against
Identity Server 7.3.0, a session carrying two permissions was refreshed down to
one and answered with exactly that one. Both verdicts are in
[the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md).
So this row should be rare, and where it does appear, login and session
persistence still work while brokered acquisition refuses — and that refusal is
correct.

### `auth.organization_switch_unsupported`

> this release cannot switch the ... identity's session out of its home tenant

Your context's `organization` names something other than the identity's
`auth.tenant`. Make them match, or add a second identity whose home tenant is
the organization you are targeting and log in as it.

### `auth.product_not_configured`

The module asked for something this identity does not register. The message
names both sides. Either the module's namespace is missing from `products`, or
its `audience` differs from the one the module asked for, or a scope it asked
for is not in the `scopes` list. Fix the context document.

### `auth.audience_not_declared` / `auth.scope_not_declared`

The module asked for more than its own installation declared. This is not a
context problem — reinstall the module. The shell grants only what a module
receipt declares, whatever the context document allows.

### `auth.credential_unavailable`

On a `client-credentials` context: the variable named by `clientSecretVariable`
is unset, empty, or holds a secret the deployment rejects. The guidance names
the variable. A variable exported as an empty string is treated as unset.

On a browser login: the flow ended without producing tokens — you closed the
browser, the consent was denied, or the deployment redirected back with an
error.

The browser reached "Login complete" and the code exchange succeeded, but the
identity token that came back was not one the shell would accept. The message
says which kind of failure it was:

| The message says | What it means | What to change |
| --- | --- | --- |
| "was not signed by the identity provider's keys" | The signature did not check out against the key set the issuer publishes. | Usually the `issuer` in your context document names a different deployment than the one that signed you in. |
| "was issued for a different application" | The token's `aud` does not carry your `clientId`. | Confirm `clientId` names the application this issuer signed you in to. |
| "had already expired" | The token was outside its validity window on arrival. | Check this machine's clock. |
| "the shell could not read the signing keys the identity provider publishes" | The key set could not be fetched or parsed. | Confirm the machine can reach the issuer's `jwks_uri`. If it is reachable, see below. |

That last one has a known cause worth naming, because it is not your
configuration. Many WSO2 deployments — Asgardeo tenants and Identity Servers
alike — publish a token-signing certificate whose X.509 serial number is
negative, which RFC 5280 forbids and which Go has rejected since 1.23. The
certificate travels in the `x5c` field of the JWKS, and a library that parses
it eagerly fails the entire key set over it.

**The shell no longer reads that field.** A key's own parameters describe it
completely, so the certificate beside it is discarded before anything tries to
parse it, and such a deployment logs in normally. Nothing needs to be set, and
in particular the `GODEBUG=x509negativeserial=1` workaround that circulated
before this was fixed is no longer required.

If you want to confirm a deployment has such a certificate — a leading minus
sign on the serial is the whole diagnosis:

```sh
curl -s "$(curl -s <issuer>/.well-known/openid-configuration | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwks_uri"])')" \
  | python3 -c 'import base64,json,sys; sys.stdout.buffer.write(base64.b64decode(json.load(sys.stdin)["keys"][0]["x5c"][0]))' \
  | openssl x509 -inform der -noout -serial
```

A serial printed as, for example, `serial=-3A4F8369` is that defect. It no
longer stops a login.

### `auth.login_not_required`

You ran `wso2 login` on a context whose identity carries its own credential.
There is no session to establish; just run the command (section 7).

### `auth.non_interactive`

`wso2 login` was run with `--non-interactive`, or with `WSO2_NON_INTERACTIVE`
set. This is the guard that stops a CI job from waiting on a browser forever.

### `auth.kind_not_implemented`

The context's `auth.kind` is `oauth-device` or `pat`. The schema names them; this
release does not implement them. Use `oauth-browser` or `client-credentials`.

### `auth.session_issuer_mismatch`

The stored session was established against a different issuer than the context
now names. You changed the `issuer` after logging in. Run `wso2 login` again.

---

## 9. Proving it against a real deployment

This repository ships a live smoke run and two one-time experiments, both behind
the `smoke` build tag so they never execute in the default test gate. Neither
touches your own `~/.wso2`: they write a context document into a temporary state
root and store their session under the secure-store reference `wso2-cli-smoke`,
deleted before and after every run.

### 9.1 First, the runs that need no deployment

Nothing below is worth a browser sign-in until these pass. The deterministic
suite already drives the whole chain — login, session, brokered acquisition —
against a fake OIDC issuer that signs real JWTs, so what the live runs add is
evidence about a *deployment*, not about the shell.

```sh
make test          # the default gate, including the acceptance suite
make acceptance    # the architecture-proof gate
make smoke-build   # proves the live runs still compile against the shell
make lint
```

Confirm the live runs skip honestly while you are still unconfigured:

```sh
go test -tags smoke ./test/smoke -run TestLoginSmoke -v
# --- SKIP: TestLoginSmoke — no live deployment is configured: set WSO2_SMOKE_ISSUER, ...
```

### 9.2 Describe the deployment

Put it in a file rather than in your shell. You will end up with more than one
deployment, and the variable that differs between them is not the one you would
guess:

```sh
cp test/smoke/env.example test/smoke/.env
```

```sh
export WSO2_SMOKE_ISSUER='https://api.asgardeo.io/t/<org>/oauth2/token'
export WSO2_SMOKE_CLIENT_ID='<client id>'
export WSO2_SMOKE_AUDIENCE='<client id>'     # on Asgardeo — see section 2.5
export WSO2_SMOKE_SCOPE='reference:status:read reference:status:write'
```

`make smoke-login` and `make empirical-asgardeo` source `test/smoke/.env` when
it exists, and print which file they read. Keep one per deployment and name it
with `SMOKE_ENV=test/smoke/is.env`. Nothing parses these files — Go has no
dotenv convention and the module carries no dependency that would add one — so
`. test/smoke/is.env` in your own shell does exactly what `make` does. Values in
the file overwrite what the shell already exported, which is what keeps a
leftover export from the last deployment from quietly outranking the file you
just edited. `*.env` is ignored by git.

`WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` are different fields that
Asgardeo happens to force to the same value: the first says who is asking, the
second says what the issued token must be bound to. **On Identity Server 7.x
they differ**, and the second is the resource identifier from section 2.4 —
measured on 7.3.0, section 3.5. Copying one deployment's file to another and
changing only the issuer is therefore the mistake to expect; it costs a browser
sign-in and ends in `auth.narrowing_unavailable` naming the audience.

Confirm the issuer against the deployment's own document before spending a
sign-in on a value that is close but not exact:

```sh
curl -s "$WSO2_SMOKE_ISSUER/.well-known/openid-configuration" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["issuer"]); print(d["code_challenge_methods_supported"])'
```

The printed issuer must equal `WSO2_SMOKE_ISSUER` character for character, and
`S256` must appear. Those are the two most common reasons a first login fails
before it reaches a browser.

### 9.3 The live runs

```sh
make smoke-login          # log in, prove the session persisted, broker one acquisition
make empirical-asgardeo   # answer the two open questions about Asgardeo's behavior
```

A passing smoke run ends with the acquisition granted:

```
LOGIN SMOKE: granted — access of 1219 characters bound to "<audience>", expiring 20:07:22Z
```

A run that ends in `auth.narrowing_unavailable` **also passes**, and that is
deliberate: the shell refusing to hand a module more authority than it asked for
is the designed outcome, not a fallback. Section 8 decodes which of the five
narrowing refusals you got.

The experiments print one verdict line each. Their answers belong in section 3
of
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md),
with the date and the `deployment:` line the run printed beneath each verdict —
the verdicts are per-deployment and a second tenant is not covered by the first
one's cells. Both questions were answered against a live Asgardeo tenant on
2026-08-06: any-port loopback **supported**, refresh narrowing **honored**.

Read
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md) before recording
anything. It lists every variable these runs read and, more importantly,
explains which verdicts are catch-all branches that need corroborating — an
`ASGARDEO ANY-PORT LOOPBACK: rejected` is what the experiment prints for *any*
login that did not complete, including one where you simply closed the browser.
