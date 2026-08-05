# Logging in with the WSO2 CLI

**Status:** Working draft
**Last reviewed:** 2026-08-05
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

Whether Asgardeo waives the port for loopback addresses the way Identity Server
6.0.0 and later document is
[not settled from public sources](../research/asgardeo-redirect-uri-and-scope-narrowing.md).
Register all four regardless; it costs nothing and does not depend on the answer.

### 2.4 Add the API resource and its scopes

The audience a module asks for is an API resource identifier, and the
permissions it asks for are that resource's scopes.

1. **API Resources → New API Resource**.
2. Give it an **Identifier**. This exact string is the audience — the shell
   checks the issued token's `aud` claim against it. Record it.
3. Add the scopes the module needs, for example `reference:status:read` and
   `reference:status:write`.
4. Back on the application's **API Authorization** tab, authorize the resource
   and select those scopes.

### 2.5 Issue JWT access tokens

On the application's **Protocol** tab, under **Access Token**, set the token type
to **JWT**. An opaque access token cannot be checked, and the broker refuses
what it cannot check.

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

---

## 3. Register the application in Identity Server 7.x

In the Identity Server console, at `https://localhost:9443/console` by default.

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

Identity Server also waives the port when matching loopback redirect URIs from
6.0.0 onwards, so a single `http://127.0.0.1:10425/callback` entry is documented
to be sufficient there. Registering all four anyway keeps the same
configuration valid on Asgardeo, where that behavior is not documented.

### 3.4 Add the API resource and its scopes

**API Resources → New API Resource**, with an identifier and scopes as in
section 2.4, then authorize it on the application's **API Authorization** tab.

### 3.5 Issue JWT access tokens

Identity Server issues JWT access tokens by default. If the deployment has been
changed to opaque, change it back for this application — see section 2.5 for
why.

### 3.6 Record what you need

- **Client ID**.
- **Issuer**, which for a default Identity Server 7.x deployment takes the shape
  `https://localhost:9443/oauth2/token`. Confirm it from
  `https://localhost:9443/oauth2/token/.well-known/openid-configuration` and use
  the `issuer` value verbatim.
- **Whether this machine trusts the deployment's TLS certificate.** The shell
  uses the process's ordinary HTTP client, so a self-signed certificate that is
  not in the OS trust store fails discovery. See `auth.discovery_failed` in
  section 8.

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
- `audience` — the API resource identifier from section 2.4 or 3.4.
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
| `products.<namespace>.audience` | The API resource identifier. A module asking for any other audience is refused. |
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
| "is not bound to the ... audience" | The token's `aud` does not carry your audience. | The `audience` in your context document is not the API resource identifier, or the resource is not authorized on the application. |
| "refused to narrow this session" | The token endpoint answered `invalid_scope`. | A scope in your context document is not one the application is authorized for. |

The middle case — a deployment that will not narrow — is a property of the
deployment, not something to work around in the shell. Whether Asgardeo narrows
on the refresh grant is
[recorded in the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
once measured against a live tenant. Where it does not narrow, login and session
persistence still work; brokered acquisition refuses, and that refusal is
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
the `smoke` build tag so they never execute in the default test gate.

```sh
make smoke-login          # log in, prove the session persisted, broker one acquisition
make empirical-asgardeo   # answer the two open questions about Asgardeo's behavior
```

Both skip cleanly when no deployment is configured.
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md) lists the environment
variables they read and explains how to read and record their verdicts.
