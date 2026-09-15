# Logging in with the WSO2 CLI

**Status:** Working draft
**Last reviewed:** 2026-09-15
**Related:** [Architecture](../architecture.md),
[product requirements](../product-requirements.md),
[shell commands](../reference/commands.md),
[authentication context examples](../examples/authentication-contexts.md)

This guide takes you from a registered application to a working `wso2 login` and
a CI job that authenticates without one. Everything here is the same whichever
product backs your deployment.

**Registering the application is product-specific, and each product has its own
walkthrough.** Read one of these first, then come back here at section 2:

| Deployment | Walkthrough |
| --- | --- |
| **Asgardeo** | [Registering in Asgardeo](login-asgardeo.md) |
| **WSO2 Identity Server 7.x** | [Registering in Identity Server](login-identity-server.md) |
| **ThunderID** | [Registering in ThunderID](login-thunder.md) |

They are alternatives. You need exactly one. Each is written to be read on its
own, and each ends by handing you the four values section 2 asks for.

**If your platform team shares a context file**, you do not assemble those
values at all: `wso2 context apply -f team-context.json --use <name>` writes
the context, installing the products it needs, and one `wso2 login` then
serves every product on it. **If the product you are reaching has an installed
module**, `wso2 context create <name> --login-product <product> --url <url>`
and `wso2 context product add <product> --url <url>` fill every value in from
the module's descriptor. This guide is the one for the context document itself
and for a deployment recorded by hand.

---

## 1. What the shell needs from a deployment

The shell signs a person in with the browser Authorization Code flow and PKCE,
keeps the resulting refresh token in the operating system's secure store, and
derives a separate short-lived access token for each module that asks for one.
Nothing else is stored, and no module ever sees the session.

That design imposes five requirements on the application you register. This list
is here so you know what the clicking in your product's walkthrough is for.

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

   Four rather than one because these ports are in the IANA dynamic range and
   something else may already hold the first choice. A developer then lands on
   a registered redirect instead of a mismatch error.
4. **The refresh token grant.** The session the shell stores *is* the refresh
   token. Without this grant, login succeeds and every later command fails.
5. **An API resource with scopes, and JWT access tokens.** The shell proves that
   the token a module receives carries exactly the permissions that module asked
   for and is bound to the audience it asked for. It cannot prove that about an
   opaque token, and it refuses rather than hand over a grant it could not
   check. See `auth.narrowing_unavailable` in section 6.

A sixth is optional and needed only for logging in from a machine with no
browser: **the device code grant**. Section 3.1 covers it, and nothing else in
the registration changes.

**Where the products differ is the fifth requirement**, and the difference
decides what you write as `audience` in section 2. Asgardeo binds an access
token's `aud` to the client ID; Identity Server binds it to the API resource
identifier, once that is in the application's audience list; Thunder names a
*resource server* per request and calls the object something else again. Each
walkthrough states its product's answer and shows the measurement behind it.

---

## 2. The context document

You do not have to write this file by hand. Three commands write it:

- `wso2 context apply -f <file>` writes the contexts a shared input file
  describes, filling in everything the installed products' descriptors know
  (section 2.5).
- `wso2 context create <name> --login-product <product> --url <url>` and
  `wso2 context product add <product> --url <url>` build one context from the
  command line.
- `wso2 login --url <issuer> --client-id <id>` creates a context as it logs in
  (section 3).

This section stays because the file is what those commands write, and reading
it is how you check what they wrote. `wso2 context show` prints it whole, and
`wso2 context edit` opens it in your editor and refuses an edit that leaves it
invalid.

### 2.1 Where it goes

```
~/.wso2/cli/contexts.json
```

Set `WSO2_HOME` to use a different state root; it must be an absolute path, and
the file then lives at `$WSO2_HOME/cli/contexts.json`.

### 2.2 What it says

A context document is one list of **contexts**. Each context says how to log
in (its `login` block), what it reaches (its `products`), where its sessions
are stored (its `credentialRef`), and optionally the organization and project
to act within. Each context owns its own sessions: no two contexts share a
`credentialRef`.

```json
{
  "schemaVersion": 4,
  "defaultContext": "acme-dev",
  "contexts": [
    {
      "name": "acme-dev",
      "type": "cloud",
      "credentialRef": "acme-dev",
      "login": {
        "kind": "oauth-browser",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
        "tenant": "acme",
        "product": "reference"
      },
      "organization": "acme",
      "products": {
        "reference": {
          "url": "https://api.asgardeo.io",
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ]
}
```

Replace:

- `issuer`: the value your walkthrough had you confirm against the deployment's
  own discovery document.
- `clientId`: the client ID you recorded.
- `audience`: **the value your product's walkthrough told you to record**, and
  the one field where copying another product's document goes wrong. It is the
  client ID on [Asgardeo](login-asgardeo.md#1-what-is-different-about-asgardeo),
  the API resource identifier on
  [Identity Server](login-identity-server.md#1-what-is-different-about-identity-server),
  and an absolute resource-server URI on
  [Thunder](login-thunder.md#1-what-is-different-about-thunder). The example
  above shows the resource-identifier form, so against Asgardeo it needs the
  client ID substituted here.
- `scopes`: the scopes you authorized on the application.

Each walkthrough shows the whole context filled in for that product, including
`type` and any product-specific member.

### 2.3 What each field means

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Must be `4`. Versions 2 and 3 are read and upgraded in place the first time this shell runs (section 2.6); version 1 is read and never rewritten. |
| `defaultContext` | The context used when no `--context` flag and no `WSO2_CONTEXT` is given. May be absent, which selects nothing. |
| `contexts[].name` | Lower-case letters, digits and dashes, starting with a letter, up to 64 characters. |
| `contexts[].type` | `cloud` or `onprem`. Nothing else is accepted. |
| `contexts[].credentialRef` | The name this context's sessions are stored under in the OS secure store: the login session under the reference, and each product's own under `<ref>.<product>`. **Required** for `oauth-browser`, `oauth-device` and `pat`; **not allowed** for `client-credentials`. Unique across the document. It stays when the context is renamed, so no session moves. |
| `login.kind` | `oauth-browser` for a person at a browser. `oauth-device` for a context that can only be established without one, covered in section 3.1. `client-credentials` for CI, covered in section 5. `pat` is named by the schema but not implemented in this release. |
| `login.issuer` | The issuer, verbatim from its discovery document. |
| `login.clientId` | The registered public client. |
| `login.tenant` | The context's home organization. |
| `login.provider` | Names the product when the shell must ask it for tokens in a product-specific shape. Required for Thunder; see [its walkthrough](login-thunder.md#9-declare-the-context-then-log-in). |
| `login.product` | The product the login authorization runs for. **Required** once the context reaches a direct product, so recording another product can never move the login from under the stored sessions. The writing commands set it for you. |
| `products.<namespace>` | What this context may reach for one module. The namespace is the module's own name, and follows the same character rules as a context name. |
| `products.<namespace>.url` | The product's base URL. **Required** on every product entry, and must be an absolute `http` or `https` URL with a host. |
| `products.<namespace>.audience` | What the issued token's `aud` claim must carry. A grant whose `aud` does not carry it is refused. It is **not** compared against the audience the module asks for: a module names its API by a logical name compiled into it, while this is the concrete string *this* deployment stamps into `aud`. Which value that is differs by product; section 2.2 has the rule. |
| `products.<namespace>.scopes` | The permissions this context carries. A module asking for one that is not listed is refused. |
| `products.<namespace>.grant` | How a product whose issuer is not the login's is reached: `exchange`, `jwt-bearer` or `federated`. Absent for a product the login session covers directly. |
| `products.<namespace>.gateway` | The product's gateway, when it has one: its own `url`, `audience` and `scopes`. |
| `contexts[].organization` | The organization to act within. Either leave it out, or set it to the context's `login.tenant`. This release cannot switch a session out of its home tenant, and any other value is refused. See `auth.organization_switch_unsupported` in section 6. |

### 2.4 Check it

```sh
wso2 context show
wso2 login --context acme-dev
```

If the document is malformed, the shell says so before opening any browser.

### 2.5 A shared context file

A platform team that runs the deployment writes the short form once, and every
developer applies it. The **input file** leaves out everything an installed
product's descriptor already knows, and never names a `credentialRef` or a
`defaultContext`, which belong to one machine:

```json
{
  "contexts": [
    {
      "name": "local",
      "login": { "product": "identity" },
      "products": {
        "identity": { "url": "http://localhost:8501" },
        "api": { "url": "http://localhost:9251", "gateway": { "url": "http://localhost:9091" } }
      }
    }
  ]
}
```

```sh
wso2 context apply -f team-context.json --use local
wso2 login
```

`apply` validates the whole file, reports what it will install and change,
installs any missing product, fills in the defaults from the installed
descriptors, and writes **complete** records to `contexts.json`. It then reads
nothing from a descriptor at command time, so a later `wso2 product update`
changes no login until the file is applied again; `wso2 doctor` and
`wso2 context show` say when a record differs from what the installed product
would write now.

- A context in the file replaces the context of the same name **whole**.
  Contexts the file does not name are kept.
- The selection changes only with `--use <name>`.
- `--dry-run` prints the plan, with a field-by-field diff for each replaced
  context and the sessions that would end, and writes nothing.
- A product entry may pin a version (`"version": "1.4.2"`). A product already
  installed at another version is left as it is unless `--update-products` is
  given. `--no-install` installs nothing, and then needs each product installed
  already or stated in full in the file.
- A replaced context whose product changed URL, audience or grant has the
  sessions that no longer match ended before the file is written (ADR 0010's
  best-effort revocation).

`wso2 context export [<name>]` prints contexts in this form, complete and with
nothing machine-specific, ready to share.

### 2.6 Documents written by an earlier CLI

A schema version 2 or 3 document kept accounts and contexts in two lists. The
first command this shell runs rewrites it as version 4: each context takes its
account's login and products, and `endpoint` becomes `url`. When several
contexts shared one account, the account's sessions stay with one of them (the
selected one if it is among them, else the first by name) and the shell prints
which others need `wso2 login --context <name>`. Sessions are never copied: a
refresh token held twice is revoked by an issuer that detects reuse.

---

## 3. Log in

```sh
wso2 login
```

In a terminal, without `--context`, login asks which context to log in to. It
skips straight to a new one when none exist:

```text
Log in to:
  1. An existing context
  2. A new context
Choose [1]: 2
Deployment:
  1. WSO2 Cloud (coming soon)
  2. Local (Identity Server / Thunder)
Choose [2]:
Issuer URL: https://localhost:9443/oauth2/token
Client ID of the registered OAuth application: wso2-cli
Context name [context-1]: local-is
```

Picking an existing context lists them, with the selected one as the default,
and logs in to the one you pick. WSO2 Cloud cannot be picked yet; choosing it
says so and asks again. Under `--no-input`, `WSO2_NO_INPUT`, or a standard
input that is not a terminal, nothing is asked and login uses the selected
context, as it did before.

To name a context other than the default:

```sh
wso2 login --context acme-dev
```

A `--context` naming no context yet asks for the deployment, issuer URL and
client ID in a terminal. Under `--no-input` it is refused with
`shell.missing_required_flag` unless `--url` and `--client-id` are given to
create it.

On a machine with nothing configured yet, name the issuer and the application
you registered in section 1, and login creates what it authenticated:

```sh
wso2 login --url https://idp.customer.example --client-id wso2-cli
```

It reports the name it assigned, and `--context <name>` sets it. Without
`--context`, login asks:

```text
Context name [context-1]:
```

Enter accepts the next free `context-N`, and a name that is not legal or is
already taken is asked again. When standard input is not a terminal the default
is taken without asking. The context name is what you type on every
`--context` and every `wso2 context use` afterwards, so pick a short one;
`wso2 context rename <name> <new-name>` renames it later.

A `--url` that is not an absolute `http` or `https` URL is refused where you
typed it, so a missing `https://` is reported as the typo it is.

Nothing is written unless the login succeeded, so an issuer you mistyped costs
you the corrected command and nothing else. Nor is a session: a document this
shell may not overwrite, such as a schema version 1 one, is refused before the
browser opens rather than after a login it could not record.

Running the same login again reuses the context it created, found by its
issuer and client ID. A `--context` naming a context already configured
against another issuer or client ID is refused rather than allowed to replace
it.

The created context reaches no product yet. A self-hosted deployment publishes
no catalogue of what it serves, so `wso2 context product add <product> --url
<url>` records each one, and the login output names it. Against ThunderID,
which binds every login to a product, create the context with
`wso2 context create <name> --login-product identity --url <url>` instead.

What happens, in order:

1. The shell reads the issuer's discovery document and confirms it advertises
   `S256`.
2. It binds the first free port of 10425-10428 on `127.0.0.1`.
3. It prints the authorization URL to standard error and opens your browser at
   it. If no browser can be opened, the printed URL is the whole fallback: open
   it yourself, on this machine.
4. You sign in and consent.
5. The browser is redirected back to the loopback listener, and the shell
   exchanges the code, with the PKCE verifier, for tokens.
6. It verifies the identity token, including the nonce it sent.
7. It writes the refresh token to the operating system's secure store and
   reports who you are.

On a machine with no browser at all, a remote shell or a container, set
`WSO2_NO_BROWSER=1`. The shell then prints the URL and does not attempt to open
anything. You still have to complete the sign-in in a browser that can reach
`127.0.0.1` **on this machine**, so this helps with a missing browser, not with
a missing desktop.

The command waits up to five minutes for you.

## 3.1 Logging in without a browser

If the machine you are typing on has no browser that can reach it, because you
are over SSH or inside a container, the login above cannot finish. It waits for
the identity provider to redirect back to `127.0.0.1` on *this* machine, and
your browser's `127.0.0.1` is somewhere else.

The device authorization grant solves that. Nothing is bound to loopback, and
the approval happens on any other device you like.

**When to use it.** Set `"kind": "oauth-device"` on the context (or pass
`--device` to `wso2 context create`) when it can *only* be established this
way: a deployment where the loopback callback URLs cannot be registered, or one
whose users are never at a machine with a reachable browser. It is a property
of the context, not of where you happen to be sitting today.

If you are usually at a laptop and occasionally on a build box, that is the case
`wso2 login --device-code` is meant for, and **that flag is not in this
release**. Until it arrives, the way to have both is two contexts, one
`oauth-browser` and one `oauth-device`.

**What to register.** Everything in your product's walkthrough applies
unchanged, with two differences:

- Add the **Device Code** grant to the application's allowed grant types.
  Asgardeo and Identity Server 7.x both support it; on Asgardeo it appears in
  the same **Allowed grant types** list as Code and Refresh Token.
- The four loopback callback URLs are not used by this flow. Leave them
  registered anyway if the same application also serves browser logins.

Thunder-backed products cannot use this flow at all. Thunder registers no
device grant handler, so its deployments advertise none and the shell refuses
before printing anything.

**The context document** is the section 2.2 document with one word changed:

```json
      "login": {
        "kind": "oauth-device",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
        "tenant": "acme",
        "product": "reference"
      }
```

Every other field means exactly what it means for `oauth-browser`, and
`credentialRef` is required in the same way.

**What you see:**

```
$ wso2 login

To log in, visit:

    https://api.asgardeo.io/t/acme/authenticationendpoint/device.do

and enter the code:

    WDJB-MJHT

Or open this link, which carries the code:

    https://api.asgardeo.io/t/acme/authenticationendpoint/device.do?user_code=WDJB-MJHT

Waiting for you to approve this login...
```

Open the first URL on your phone or your laptop, type the code, and sign in. The
terminal finishes on its own. The third line is a shortcut for a device you can
paste a link into; the code is deliberately printed on its own line so it
survives being read aloud.

The shell polls at the rate the deployment asks for and stops when the code
expires, usually after ten to fifteen minutes and never later than fifteen.
Nothing is opened on this machine.

**One difference from browser login worth knowing.** A browser login always
reports a `Subject`. A device login reports one only if the deployment returned
an identity token from this grant, which not every deployment does; RFC 8628
does not require it. The session is established either way, and every product
command afterwards behaves identically.

---

## 4. What login stored, and where

- **The refresh token** goes to the operating system's secure store, under the
  service `wso2-cli` and a name made of the `credentialRef` you chose, `@`, and
  a short digest of the state root (`~/.wso2`, or `WSO2_HOME`) the login ran
  under. That store is Keychain on macOS, Secret Service on Linux, and
  Credential Manager on Windows. The digest is what keeps two state roots
  apart: a second `WSO2_HOME` whose context document also names a context
  `thunder` starts with no session, rather than the first one's, and each
  deployment keeps a refresh token of its own. `wso2 whoami` and every product
  command also check that the stored session was established against the
  `issuer`, client, scopes and resource the context names now, and treat one
  that was not as no session.
  A session stored by a shell older than this rule was written under the bare
  `credentialRef` and is not read: after upgrading, run `wso2 login` once per
  context, and `wso2 logout` retires the old entry when it finds one. A
  session stored before sessions recorded their resource is not presented for
  a resource-bound product (ThunderID) either; log in again once.
- **Nothing under `~/.wso2` holds a credential.** The state root holds the
  context document you wrote, the managed module store, and the advisory lock
  files that keep refresh-token rotation single-writer. No session material is
  ever written there: not the refresh token, not an access token, not a client
  secret.
- **Modules never receive the session.** When a module needs access, the shell
  exchanges the refresh token for a fresh, short-lived access token narrowed to
  exactly the permissions that module declared, proves the result carries what
  was asked for, and hands over only that.

To sign out, run `wso2 logout`. It asks the deployment to revoke the session's
refresh token and removes the secure-store entry named by `credentialRef`. What
it can promise about the first of those depends on the deployment, and it tells
you which of three things happened:

- **`confirmed`.** The deployment accepted the request. That means it was told,
  not that anything was found to retract: RFC 7009 requires a server to answer
  an unknown token exactly as it answers a live one, so revocation cannot be
  used to probe for valid tokens.
- **`not-attempted`.** The deployment publishes no `revocation_endpoint` in its
  OpenID configuration, so it was never asked, and its own copy of the session
  stands until it expires.
- **`failed`.** The deployment was asked and did not accept, or could not be
  reached. Most likely it requires a confidential client on that endpoint, and
  the shell is a public client with no secret.

**The secure-store entry goes under all three, and the command succeeds under
all three.** You asked to end a session; you do not keep one because the
deployment was unreachable. What changes between the outcomes is only what the
shell claims, which is the decision recorded in
[ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md).

**Logout ends only the selected context's sessions**, because each context owns
its own. By default it also opens the identity provider's sign-out page so the
next login prompts for credentials; `--keep-browser-session` leaves that
sign-on alone. Some providers end the user's other refresh tokens when the
browser sign-on ends, which is outside the shell's control, and logout says
so rather than claiming either way.

A `client-credentials` context has no session to end; logout reports that
nothing was stored and exits 0 (section 5).

---

## 5. CI: authenticate without a login

A CI job has no browser and no secure store, so it does not use a session at
all. It uses a machine-to-machine context that carries its own credential and
exchanges it inline, on every command. **There is no login step in CI.** A job
that runs `wso2 login` is refused with `auth.login_not_required`.

**Register the machine-to-machine application first.** That is product-specific,
and each walkthrough has a section for it:
[Asgardeo](login-asgardeo.md#8-a-machine-to-machine-client-for-ci-if-you-need-one),
[Identity Server](login-identity-server.md#10-a-confidential-client-for-ci-if-you-need-one),
[Thunder](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one). All
three come down to the same thing: the **Client Credentials** grant and nothing
else, no redirect URLs, no PKCE, the same API resource and scopes as the browser
application, JWT access tokens, and a recorded client ID and secret.

### 5.1 Write the CI context

```sh
wso2 context create acme-ci --issuer https://api.asgardeo.io/t/acme/oauth2/token \
    --client-id REPLACE_WITH_YOUR_M2M_CLIENT_ID --client-secret-variable WSO2_ACME_CI_SECRET
wso2 context product add reference --url https://api.asgardeo.io --context acme-ci
```

or, as the record `wso2 context export acme-ci` prints and `wso2 context apply`
reads:

```json
{
  "contexts": [
    {
      "name": "acme-ci",
      "type": "cloud",
      "login": {
        "kind": "client-credentials",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_M2M_CLIENT_ID",
        "tenant": "acme",
        "clientSecretVariable": "WSO2_ACME_CI_SECRET"
      },
      "organization": "acme",
      "products": {
        "reference": {
          "url": "https://api.asgardeo.io",
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ]
}
```

Two differences from section 2.2, and the schema enforces both:

- `clientSecretVariable` **replaces** `credentialRef`. It names an environment
  variable; it is not the secret. Upper-case letters, digits and underscores,
  starting with a letter.
- `credentialRef` must **not** appear on a `client-credentials` context, and
  `clientSecretVariable` must **not** appear on an `oauth-browser` one.

The secret itself never goes in this file, and the file is safe to commit.

**The example above is an Asgardeo context, and two of its members are
product-specific.** Substitute both before using it against another deployment:

- `audience` follows the same per-product rule as section 2.2, applied to *this*
  application. On Asgardeo it must be the M2M application's own client ID, not
  the API resource identifier the example shows.
- `login.provider` carries into a CI context exactly as it does a browser one.
  A Thunder deployment needs `"provider": "thunder"` here, because that is what
  makes the shell name the protected resource on the client-credentials
  request, and Thunder refuses a grant that names none.
  [The Thunder walkthrough](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one)
  shows the whole context.

### 5.2 Wire the job

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
      - name: Apply the context file
        run: wso2 context apply -f ci/context.json --use acme-ci --no-input
      - name: Check the shell resolves its context
        run: wso2 version
      - name: Run a product command
        run: wso2 reference status
```

**A caveat about that last step, so it does not surprise you.** `wso2 reference
status` is the example module this repository ships, and `wso2` dispatches any
namespace it does not own itself to an installed module. Module *installation*
commands (`wso2 product install`) are proposed and are not in this release, so
the module has to already be in the managed module store under
`$WSO2_HOME/cli/modules` for that step to resolve; otherwise it exits with
`shell.unknown_command`. The context, the secret variable, and the inline
grant are complete and work today, and `wso2 version` exercises the context
resolution without needing a module.

`WSO2_HOME` must be absolute. `WSO2_CONTEXT` selects the context without a flag.

Also set `WSO2_NO_INPUT=1` on any job where a stray `wso2 login` should fail
loudly rather than sit waiting on a browser that will never open. The
`--no-input` flag says the same thing for one invocation. Either way nothing
prompts, opens a browser, or waits for a human, and a browser or device login
is refused with `auth.non_interactive`. See [Non-interactive
use](../reference/commands.md#non-interactive-use) in the command reference.

Each command exchanges the client secret for an access token narrowed to what
the module asked for. The secret is read into process memory for the length of
one grant, is never written to the state root, and is never passed to a module.

---

## 6. Troubleshooting

Every refusal the shell makes carries a typed code. Find yours here. This table
covers all three products; failures that can only happen on one of them are in
that product's walkthrough. [Thunder's](login-thunder.md#10-troubleshooting) is
the longest, because its registration model differs the most.

### The context document: `contexts.*`

These come from the file you wrote in section 2, and they are the ones a
first-time user meets most often. None of them reaches a browser.

- **`contexts.document_malformed`.** The document was read but is not valid.
  The message names the specific defect, and section 2.3 is the field-by-field
  reference for it. The usual causes are a name that breaks the character rules
  (context names and `credentialRef` are lower-case letters, digits and dashes,
  starting with a letter), a `type` that is not exactly `cloud` or `onprem`, a
  missing `url` on a product entry, two contexts naming the same
  `credentialRef`, an interactive context that reaches a direct product without
  naming its `login.product`, or the `credentialRef` / `clientSecretVariable`
  rule: exactly one of them belongs on a context, and which one is decided by
  `login.kind`. A URL that embeds user information is refused with a recovery
  of its own naming what to take out; the rejected URL is never repeated back,
  because it is the likeliest place for a credential to have been typed by
  mistake. Nothing is written when a command is refused this way.
- **`contexts.document_malformed` is about content, not about version.** If the
  shell declined to overwrite your file because of its `schemaVersion`, the code
  is `contexts.document_frozen` below, and the field reference will not help.
- **`contexts.document_unreadable`.** The file exists but could not be read.
  Check its permissions, or delete it to run without a context.
- **`contexts.document_frozen`.** The document on disk declares a schema
  version this shell does not write, so a command that would have replaced it
  refused instead. The message names the file and the version it found. Either
  it is a version 1 document, which this shell still reads but will not rewrite
  in place, or it is a version a newer WSO2 CLI on this machine wrote and still
  manages, which this shell cannot read at all. Nothing is wrong with the file.
  Move it aside to start a new one, or run the CLI version that manages it.
- **`contexts.document_unwritable`.** The shell had something to write to the
  document and could not — the file itself is fine. Check that the state root,
  `~/.wso2/cli` or `$WSO2_HOME/cli`, is writable by you, then retry.
- **`contexts.document_busy`.** Another `wso2` invocation held the document's
  update lock for longer than the shell waits. Writing the document takes no
  network call, so a holder that slow is stuck rather than working; retry, and
  if it repeats, check for a `wso2` process that is not making progress.
- **`contexts.schema_unsupported`.** `schemaVersion` is not one this shell
  reads. It must be `2`.
- **`contexts.unknown_context`.** You named a context, with `--context` or
  `WSO2_CONTEXT`, that the document does not declare. The message names the one
  you asked for. Check it against the `contexts` array and `defaultContext`.
- **`contexts.no_context_selected`.** Contexts exist and none is selected, which
  `wso2 context apply` leaves behind unless given `--use`. Run
  `wso2 context use <name>`, or pass `--context <name>`.
- **`contexts.context_exists`.** `wso2 context create` was given a name the
  document already declares, or `wso2 login --url` named a context already
  configured against a different issuer or client ID; the message names both
  values. Neither command replaces a context, because what it recorded is not
  written down anywhere else. Choose another name, or replace it deliberately
  with `wso2 context apply -f <file>`.
- **`contexts.product_exists`.** `wso2 context product add` named a product the
  context already records. `wso2 context show` shows what is recorded, and
  `--replace` overwrites it, replacing the whole record and ending the sessions
  it no longer matches.
- **`contexts.unknown_product`.** `wso2 context product remove` named a record
  the context does not hold. The refusal lists every record it does, product
  namespaces and `<namespace>/gateway` keys alike, and nothing was removed or
  ended.
- **`contexts.login_product`.** `wso2 context product remove` named the product
  the context logs in through. Its login session was authorized for that
  product, so nothing was removed or ended. To log in through another product,
  create a context that logs in through it with `wso2 context create <name>
  --login-product <product> --url <url>`. The login product's gateway record is
  separate and can be removed on its own.
- **`shell.command_moved`.** You typed a command this shell removed, such as
  `wso2 <product> connect` or `wso2 account …`. The recovery is the exact
  replacement, built from what you typed.

### The context commands: `shell.*`

These are about what you typed, not about the file. Nothing is written when one
of them is reported.

- **`shell.missing_required_flag`.** A flag the command cannot proceed without
  was not given. `wso2 context create` reports it for a line that names neither
  `--login-product <product> --url <url>` nor `--issuer <url> --client-id <id>`,
  and for a `--url` without `--login-product`: a URL alone does not say which
  product's defaults apply, so the shell does not guess. `wso2 login --url`
  reports it for `--client-id`, which it asks for at a terminal and refuses to
  guess anywhere else. `wso2 context product add` reports it for `--url`. The
  message says why nothing was asked: `--no-input`, `WSO2_NO_INPUT`, or
  standard input that is not a terminal.
- **`shell.invalid_argument`.** A value you typed is not one the command can
  use: a name a context may not have (lower-case letters, digits and hyphens,
  starting with a letter, at most 64 characters), a value that is not a URL, a
  product that is not a login provider named as `--login-product`, or a secret
  where a variable name belongs. Nothing was written, so retyping the command
  is usually the whole fix.
- **`shell.input_malformed`.** `wso2 context apply` read a file that is not a
  valid input file: unknown members, a `defaultContext` or `credentialRef`
  (which belong to one machine), a context with no way to log in, or a URL that
  does not parse. Nothing was installed or written.
- **`shell.missing_argument`, `shell.unexpected_argument`.** The command was
  given too few or too many arguments. The recovery shows the shape it expects.

### `auth.context_not_selected`

There is no context document at all, or no context is selected. Run
`wso2 context use <name>` to select one that already exists, `wso2 context
apply -f <file>` to set one up from a shared file, or `wso2 login --url
<issuer> --client-id <id>`, which creates a context as it logs in.
`wso2 context list` shows what is configured.

### `shell.unknown_command`

The first word was not a shell command — `context`, `help`, `login`,
`logout`, `product`, `org`, `whoami`, `doctor`, `config` or `version` — and no
installed module owns that
namespace. `wso2 help` lists the commands the shell owns. See the caveat at
the end of section 5.2.

### `auth.discovery_failed`

> the shell could not read the identity provider's OpenID configuration

The issuer in your context document could not be read, or what came back was not
usable. In order of likelihood:

- **The issuer is not exact.** It must equal the `issuer` value in the
  deployment's own discovery document, character for character. Fetch
  `<issuer>/.well-known/openid-configuration` and compare. The three products do
  not share an issuer shape: Asgardeo's carries a `/oauth2/token` path under a
  tenant, Identity Server's carries `/oauth2/token` under a host and port, and
  Thunder's is the bare origin.
- **The machine cannot reach the issuer.** Proxy, VPN, firewall.
- **The issuer does not advertise `S256`.** Set PKCE to mandatory on the
  application, as your walkthrough's public-client section describes.

There is a third, on a device login only:

> the identity provider does not advertise the device authorization grant

The deployment does not offer the grant, so there is no point printing a code
nobody could approve. Either enable the **Device Code** grant on the
application (section 3.1), or use an `oauth-browser` context. Thunder-backed
deployments have no device grant at all and cannot be made to.

There is a second, differently worded `auth.discovery_failed`:

> no loopback callback port is available for the browser login

All four of 10425-10428 are in use. Find the holders and free one:

```sh
lsof -nP -iTCP@127.0.0.1:10425-10428 -sTCP:LISTEN   # macOS, Linux
```

The shell will not fall back to an unregistered port, because the deployment
would reject the redirect and the error would name the wrong problem.

A certificate this machine does not trust is not reported under this code; it
gets its own, below.

### `auth.certificate_untrusted`

> the certificate the identity provider at localhost:9443 presents is not trusted by this machine

The issuer answered, but its TLS certificate is not signed by an authority
this machine knows, so the shell would not read anything from it. Every
self-hosted product — API Manager, Identity Server, Thunder — serves a
self-signed certificate on a fresh install, so this is the ordinary first
failure against one. The host and port in the message are the ones the shell
dialled, and the recovery carries the same two commands with them filled in:

```sh
openssl s_client -connect localhost:9443 -showcerts </dev/null 2>/dev/null \
  | awk '/BEGIN CERT/,/END CERT/' > localhost-9443.pem
export WSO2_CA_FILE=$PWD/localhost-9443.pem
```

`WSO2_CA_FILE` is read by the shell that runs `wso2`, so export it there, not
in a shell that only produced the file. It widens trust beside the operating
system's roots and never narrows it; a file that cannot be read is refused
with `shell.ca_file_unreadable`. Adding the certificate to the operating
system's trust store instead works too, and needs no variable. The shell
deliberately has no flag to skip verification. `wso2 doctor --online` reports
the same refusal from its issuer check, so a machine can be checked before
any product command is run; see the
[command reference](../reference/commands.md#trusting-a-deployments-certificate).

### `auth.login_required`

No usable session. Either you have not logged in for this `credentialRef`, or
the deployment has stopped accepting the stored refresh token: it was revoked,
it expired, or a concurrent run rotated it away. Run `wso2 login` again.

### `auth.logout_not_required`

No longer raised. `wso2 logout` against a context that acquires access
inline, which in practice means a `client-credentials` context, now reports
that no session was stored and exits 0, so a pipeline that ends with it does
not fail. Nothing is stored for such a context; remove the
credential from the environment to stop the shell acquiring access with it.

### `auth.keyring_unavailable`

The operating system's secure store could not be used. On a headless Linux
machine this usually means no Secret Service is running; start a keyring daemon,
or use a `client-credentials` context (section 5), which needs no secure store
at all.

### `auth.narrowing_unavailable`

> the deployment ... the permissions the "reference" module asked for

The shell obtained a token but could not prove it was narrowed to what the
module asked for, so it refused to hand it over. **This refusal is the designed
behavior, not a degraded mode.** A module that silently received the whole
session's authority would hold access nobody decided to give it.

The message tells you which of five things happened:

| The message says | What it means | What to change |
| --- | --- | --- |
| "in a form the shell cannot check" | The access token is opaque. | Set the application to issue JWT access tokens. |
| "did not state which permissions it issued" | The deployment returned no scope, and the token claims none. | Check the API resource is authorized on the application with the scopes selected. |
| "asked for the permissions X and the deployment issued Y" | The deployment ignored the narrower request and issued something else. | The deployment does not narrow on this grant. See below. |
| "is not bound to the ... audience" | The token's `aud` does not carry your audience. | The `audience` in your context document names something the deployment never puts in `aud`. Which value that is differs by product: the **client ID** on [Asgardeo](login-asgardeo.md#1-what-is-different-about-asgardeo), the **API resource identifier** on [Identity Server](login-identity-server.md#1-what-is-different-about-identity-server) and there only once it is in the application's audience list, and the **resource server URI** on [Thunder](login-thunder.md#1-what-is-different-about-thunder). Failing that, the resource is not authorized on the application. |
| "refused to narrow this session" | The token endpoint answered `invalid_scope`. | A scope in your context document is not one the application is authorized for. |

The middle case, a deployment that will not narrow, is a property of the
deployment and not something to work around in the shell. Both products this was
measured on do narrow: on 2026-08-06, against a live Asgardeo tenant and against
Identity Server 7.3.0, a session carrying two permissions was refreshed down to
one and answered with exactly that one. Both verdicts are in
[the research document](../research/asgardeo-redirect-uri-and-scope-narrowing.md).
So this row should be rare, and where it does appear, login and session
persistence still work while brokered acquisition refuses, and that refusal is
correct.

### `auth.organization_switch_unsupported`

> this release cannot switch its session out of its home tenant

Your context's `organization` names something other than its `login.tenant`.
Make them match, or create a context whose home tenant is the organization you
are targeting and log in to it.

### `auth.product_not_configured`

The module asked for something this context does not register. The message
names both sides. Either the module's namespace is missing from `products`, or
its entry sets no `audience`, or a scope it asked for is not in the `scopes`
list. `wso2 context product add <product> --url <url> [--replace]` records it.

An `audience` that differs from the one the module asks for is *not* this
refusal, and is normal: the values come from different vocabularies. What must
hold is that the deployment binds the token to the `audience` recorded here,
which is proved when the token arrives and reported as
`auth.narrowing_unavailable` when it fails.

The same code reports a login the identity provider refused with RFC 8707's
`invalid_target`, which no retry gets past. When the login named no resource
server, the context records no product its login binds to: ThunderID binds
every login to one, so a `wso2 login --url` that creates its context is
refused this way, and the recovery names `wso2 context create <name>
--login-product identity --url <issuer-url>`, which creates the context with
that product recorded. When the login named one, the
deployment has not registered it: register it as a resource server identifier
at the identity provider, or record the product with the audience the
deployment did register.

### `auth.audience_not_declared` / `auth.scope_not_declared`

The module asked for more than its own installation declared. This is not a
context problem. Reinstall the module. The shell grants only what a module
receipt declares, whatever the context document allows.

### `auth.credential_unavailable`

On a `client-credentials` context: the variable named by `clientSecretVariable`
is unset, empty, or holds a secret the deployment rejects. The guidance names
the variable. A variable exported as an empty string is treated as unset.

On a browser login: the flow ended without producing tokens. You closed the
browser, someone denied the consent, or the deployment redirected back with an
error.

The browser reached "You are signed in" and the code exchange succeeded, but
the identity token that came back was not one the shell would accept. The message
says which kind of failure it was:

| The message says | What it means | What to change |
| --- | --- | --- |
| "was not signed by the identity provider's keys" | The signature did not check out against the key set the issuer publishes. | Usually the `issuer` in your context document names a different deployment than the one that signed you in. |
| "was issued for a different application" | The token's `aud` does not carry your `clientId`. | Confirm `clientId` names the application this issuer signed you in to. |
| "had already expired" | The token was outside its validity window on arrival. | Check this machine's clock. |
| "the shell could not read the signing keys the identity provider publishes" | The key set could not be fetched or parsed. | Confirm the machine can reach the issuer's `jwks_uri`. If it is reachable, see below. |

That last one has a known cause worth naming, because it is not your
configuration. Many WSO2 deployments, Asgardeo tenants and Identity Servers
alike, publish a token-signing certificate whose X.509 serial number is
negative, which RFC 5280 forbids and which Go has rejected since 1.23. The
certificate travels in the `x5c` field of the JWKS, and a library that parses
it eagerly fails the entire key set over it.

**The shell no longer reads that field.** A key's own parameters describe it
completely, so the certificate beside it is discarded before anything tries to
parse it, and such a deployment logs in normally. Nothing needs to be set, and
in particular the `GODEBUG=x509negativeserial=1` workaround that circulated
before this was fixed is no longer required.

If you want to confirm a deployment has such a certificate, a leading minus
sign on the serial is the whole diagnosis:

```sh
curl -s "$(curl -s <issuer>/.well-known/openid-configuration | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwks_uri"])')" \
  | python3 -c 'import base64,json,sys; sys.stdout.buffer.write(base64.b64decode(json.load(sys.stdin)["keys"][0]["x5c"][0]))' \
  | openssl x509 -inform der -noout -serial
```

A serial printed as, for example, `serial=-3A4F8369` is that defect. It no
longer stops a login.

On a device login (section 3.1), the message says which of four endings it was:

| The message says | What it means | What to do |
| --- | --- | --- |
| "the login was declined at the identity provider" | You, or someone at the approval screen, refused the request. | Run `wso2 login` again and approve it. Check the code on screen matches the one in your terminal. |
| "the approval window closed before this login was approved" | The device code expired before anyone approved it. | Run `wso2 login` again and approve it promptly. |
| "this login was not approved in time" | The same, reached by the shell's own deadline rather than the deployment's answer. | As above. |
| "would not start a device authorization" | The deployment refused the request before any code was issued. | Confirm `clientId`, and that the application is registered for the device grant. |

All four leave you with no session, which is why they share one
code. Only the sentence differs, because only the sentence can.

### `auth.login_not_required`

You ran `wso2 login` on a context that carries its own credential.
There is no session to establish; just run the command (section 5).

### `auth.non_interactive`

`wso2 login` was run with `--no-input`, or with `WSO2_NO_INPUT` set. This is
the guard that stops a CI job from waiting forever on a browser, or, on a
device context, from waiting forever on an approval no one is there to give.
The message names which of the two it refused.

### `auth.kind_not_implemented`

The context's `login.kind` is `pat`. The schema names it; this release does not
implement it. Use `oauth-browser`, `oauth-device`, or `client-credentials`.

### `auth.session_issuer_mismatch`

The stored session was established against a different issuer than the context
now names. You changed the `issuer` after logging in. Run `wso2 login` again.

### `auth.session_required`

> the "apim" product has no session under this context yet

Nothing is stored for that product, and the invocation forbade the browser that
would authorize it: you passed `--no-input`, or `WSO2_NO_INPUT` is set. The
refusal names whichever of the two said so. Run `wso2 login --only <namespace>`
where a browser can open, or `wso2 login` to authorize every product.

### `auth.reauthorization_required`

> the "apim" product has a session under this context, but the identity
> provider would not renew it to the permissions the module asked for

A session for the product **is** stored — `wso2 whoami` shows it present — and
the deployment would not renew it to what this command needs. Authorizing the
product again is the step that wanted a browser, and the invocation forbade
one.

It is a different refusal from `auth.session_required` because the remedy is
different, and the shell cannot tell two causes apart from here. Another
login, run where a browser can open, may fix it: the issuer is asked afresh and
may grant what a refresh would not, which is what a federated product does
every time its access token expires. If it does not, this user's groups map to
no role carrying the permissions, and no number of logins will change that —
an administrator has to grant one. The recovery names both, in that order.

---

## 7. Proving it against a real deployment

This repository ships a live smoke run and two one-time experiments, both behind
the `smoke` build tag so they never execute in the default test gate. Neither
touches your own `~/.wso2`: they write a context document into a temporary state
root and store their session under the secure-store reference `wso2-cli-smoke`,
deleted before and after every run.

### 7.1 First, the runs that need no deployment

Nothing below is worth a browser sign-in until these pass. The deterministic
suite already drives login, session, and brokered acquisition
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

### 7.2 Describe the deployment

Put it in a file rather than in your shell. You will end up with more than one
deployment, and the variable that differs between them is not the one you would
guess:

```sh
cp test/smoke/env.example test/smoke/.env
```

```sh
export WSO2_SMOKE_ISSUER='https://api.asgardeo.io/t/<org>/oauth2/token'
export WSO2_SMOKE_CLIENT_ID='<client id>'
export WSO2_SMOKE_AUDIENCE='<client id>'     # on Asgardeo, see its walkthrough
export WSO2_SMOKE_SCOPE='reference:status:read reference:status:write'
```

`make smoke-login` and `make empirical-asgardeo` source `test/smoke/.env` when
it exists, and print which file they read. Keep one per deployment and name it
with `SMOKE_ENV=test/smoke/is.env`. Nothing parses these files. Go has no
dotenv convention and the module carries no dependency that would add one, so
`. test/smoke/is.env` in your own shell does exactly what `make` does. Values in
the file overwrite what the shell already exported, which is what keeps a
leftover export from the last deployment from quietly outranking the file you
just edited. `*.env` is ignored by git.

`WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` are different fields that
Asgardeo happens to force to the same value: the first says who is asking, the
second says what the issued token must be bound to. **On Identity Server and
Thunder they differ**: it is the API resource identifier on Identity Server and
an absolute resource-server URI on Thunder, which refuses a bare identifier.
See
[the Identity Server walkthrough](login-identity-server.md#1-what-is-different-about-identity-server)
and [the Thunder walkthrough](login-thunder.md#1-what-is-different-about-thunder).
Copying one deployment's file to another and changing only the issuer is
therefore the mistake to expect; it costs a browser sign-in and ends in
`auth.narrowing_unavailable` naming the audience.

Confirm the issuer against the deployment's own document before spending a
sign-in on a value that is close but not exact:

```sh
curl -s "$WSO2_SMOKE_ISSUER/.well-known/openid-configuration" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["issuer"]); print(d["code_challenge_methods_supported"])'
```

The printed issuer must equal `WSO2_SMOKE_ISSUER` character for character, and
`S256` must appear. Those are the two most common reasons a first login fails
before it reaches a browser.

### 7.3 The live runs

```sh
make smoke-login          # log in, prove the session persisted, broker one acquisition
make smoke-login-device   # the same, approved on another device (section 3.1)
make empirical-asgardeo   # answer the two open questions about Asgardeo's behavior
```

`make smoke-login-device` reads the same variables and needs no new ones. The
only thing it wants from the deployment is the device grant enabled on the same
application. It also reports whether that deployment's device grant returned an
identity token, which is a per-deployment fact this repository has not yet
measured on either product; the answer belongs in the research document beside
the other verdicts.

A passing smoke run ends with the acquisition granted:

```
LOGIN SMOKE: granted — access of 1219 characters bound to "<audience>", expiring 20:07:22Z
```

A run that ends in `auth.narrowing_unavailable` **also passes**, and that is
deliberate: the shell refusing to hand a module more authority than it asked for
is the designed outcome, not a fallback. Section 6 decodes which of the five
narrowing refusals you got.

The experiments print one verdict line each. Their answers belong in section 3
of
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md),
with the date and the `deployment:` line the run printed beneath each verdict.
The verdicts are per-deployment, and the first tenant's cells say nothing about
a second. Both questions were answered against a live Asgardeo tenant on
2026-08-06: any-port loopback **supported**, refresh narrowing **honored**.

Read
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md) before recording
anything. It lists every variable these runs read and, more importantly,
explains which verdicts are catch-all branches that need corroborating. An
`ASGARDEO ANY-PORT LOOPBACK: rejected` is what the experiment prints for *any*
login that did not complete, including one where you simply closed the browser.
