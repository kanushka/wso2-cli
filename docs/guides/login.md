# Logging in with the WSO2 CLI

**Status:** Working draft
**Last reviewed:** 2026-08-10
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

## 2. Write the context document

You do not have to write this file by hand. `wso2 login --url <issuer>
--client-id <id>` creates the identity and the context it authenticates, and
`wso2 context create` adds further contexts over the same identity; section 3
takes that route, and no editor is involved in it. This section stays because
the file is what those commands write, and reading it is how you check what
they wrote — and because a context that names an organization, a project, or
more than one product is still quicker to write than to assemble from flags.

### 2.1 Where it goes

```
~/.wso2/cli/contexts.json
```

Set `WSO2_HOME` to use a different state root; it must be an absolute path, and
the file then lives at `$WSO2_HOME/cli/contexts.json`.

```sh
mkdir -p ~/.wso2/cli
```

### 2.2 What it says

A context document names **identities** and **contexts**. An identity says how
to authenticate and what it may reach. A context selects an identity and the
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

Each walkthrough's last-but-one section shows the whole identity block filled in
for that product, including `type` and any product-specific member.

### 2.3 What each field means

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Must be `2`. |
| `defaultContext` | The context used when no `--context` flag and no `WSO2_CONTEXT` is given. Must name a context declared below. |
| `identities[].name` | Lower-case letters, digits and dashes, starting with a letter, up to 64 characters. |
| `identities[].type` | `cloud` or `onprem`. Nothing else is accepted. |
| `auth.kind` | `oauth-browser` for a person at a browser. `oauth-device` for an identity that can only be established without one, covered in section 3.1. `client-credentials` for CI, covered in section 5. `pat` is named by the schema but not implemented in this release. |
| `auth.issuer` | The issuer, verbatim from its discovery document. |
| `auth.clientId` | The registered public client. |
| `auth.tenant` | The identity's home organization. |
| `auth.provider` | Names the product when the shell must ask it for tokens in a product-specific shape. Required for Thunder; see [its walkthrough](login-thunder.md#9-write-the-context-document). |
| `auth.credentialRef` | The name the session is stored under in the OS secure store. **Required** for `oauth-browser` and `oauth-device`; **not allowed** for `client-credentials`. Same character rules as an identity name. |
| `products.<namespace>` | What this identity may reach for one module. The namespace is the module's own name, and follows the same character rules as an identity name. |
| `products.<namespace>.endpoint` | The product's base URL. **Required** on every product entry, and must be an absolute `http` or `https` URL with a host. |
| `products.<namespace>.audience` | What the issued token's `aud` claim must carry. A grant whose `aud` does not carry it is refused. It is **not** compared against the audience the module asks for: a module names its API by a logical name compiled into it, while this is the concrete string *this* deployment stamps into `aud`. Which value that is differs by product; section 2.2 has the rule. |
| `products.<namespace>.scopes` | The permissions this identity carries. A module asking for one that is not listed is refused. |
| `contexts[].organization` | The organization to act within. Either leave it out, or set it to the identity's `auth.tenant`. This release cannot switch a session out of its home tenant, and any other value is refused. See `auth.organization_switch_unsupported` in section 6. |

### 2.4 Check it

```sh
wso2 login --context acme-dev
```

If the document is malformed, the shell says so before opening any browser.

---

## 3. Log in

```sh
wso2 login
```

or, to name a context other than the default:

```sh
wso2 login --context acme-dev
```

On a machine with nothing configured yet, name the issuer and the application
you registered in section 1, and login creates what it authenticated:

```sh
wso2 login --url https://idp.customer.example --client-id wso2-cli
```

It reports the names it assigned. Without `--context` the identity and the
context are both named after the issuer host with each dot replaced by a hyphen
— `idp.customer.example` becomes `idp-customer-example` — and `--context
<name>` names them both directly. The context name is what you type on every
`--context` and every `wso2 context use` afterwards, so pass a short one if the
derived name is longer than you want to live with; `wso2 context create <name>
--identity <identity>` adds a shorter handle to the same identity later.

An issuer with no host to name, such as one at a bare IP address, is refused
rather than given a name you could not have predicted; `--context` is the way
through. A `--url` that is not an absolute `http` or `https` URL is refused
where you typed it, so a missing `https://` is reported as the typo it is.

Nothing is written unless the login succeeded, so an issuer you mistyped costs
you the corrected command and nothing else. Nor is a session: a document this
shell may not overwrite, such as a schema version 1 one, is refused before the
browser opens rather than after a login it could not record.

Running the same login again reuses the identity it created; a login that would
change the issuer or the client ID of an identity already configured is refused
rather than allowed to replace it.

The created identity reaches no product yet. A self-hosted deployment publishes
no catalogue of what it serves, so `wso2 identity add-product` records each
product's endpoint, audience and scopes, and the login output names it.

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

**When to use it.** Set `"kind": "oauth-device"` on the identity when that
identity can *only* be established this way: a deployment where the loopback
callback URLs cannot be registered, or one whose users are never at a machine
with a reachable browser. It is a property of the identity, not of where you
happen to be sitting today.

If you are usually at a laptop and occasionally on a build box, that is the case
`wso2 login --device-code` is meant for, and **that flag is not in this
release**. Until it arrives, the way to have both is two identities, one
`oauth-browser` and one `oauth-device`, with different `credentialRef` values
and a context for each.

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
      "auth": {
        "kind": "oauth-device",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
        "tenant": "acme",
        "credentialRef": "acme-cloud-device"
      }
```

Every other field means exactly what it means for `oauth-browser`, and
`credentialRef` is required in the same way. Give it a different value from your
browser identity's if you keep both, so the two sessions do not share a slot.

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
  service `wso2-cli` and the name you put in `credentialRef`. That store is
  Keychain on macOS, Secret Service on Linux, and Credential Manager on
  Windows.
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

**Two things logout does not do.** It does not end the browser single-sign-on
session at the identity provider, so a later `wso2 login` may complete without
prompting you for credentials; sections 2.3 and 3.3 describe that session.
And because a session is keyed by `credentialRef`, which belongs to the
identity, ending it ends it for every context naming that identity; the command
names them.

A `client-credentials` identity has no session to end and is refused with
`auth.logout_not_required` (section 5).

---

## 5. CI: authenticate without a login

A CI job has no browser and no secure store, so it does not use a session at
all. It uses a machine-to-machine identity that carries its own credential and
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

Two differences from section 2.2, and the schema enforces both:

- `clientSecretVariable` **replaces** `credentialRef`. It names an environment
  variable; it is not the secret. Upper-case letters, digits and underscores,
  starting with a letter.
- `credentialRef` must **not** appear on a `client-credentials` identity, and
  `clientSecretVariable` must **not** appear on an `oauth-browser` one.

The secret itself never goes in this file, and the file is safe to commit.

**The example above is an Asgardeo identity, and two of its members are
product-specific.** Substitute both before using it against another deployment:

- `audience` follows the same per-product rule as section 2.2, applied to *this*
  application. On Asgardeo it must be the M2M application's own client ID, not
  the API resource identifier the example shows.
- `auth.provider` carries into a CI identity exactly as it does a browser one.
  A Thunder deployment needs `"provider": "thunder"` here, because that is what
  makes the shell name the protected resource on the client-credentials
  request, and Thunder refuses a grant that names none. The document parses
  either way, so leaving it out fails at the first command rather than at the
  first
  read. [The Thunder walkthrough](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one)
  shows the whole identity, including the two further rules a resource-bound
  identity must satisfy.

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
`shell.unknown_command`. The identity, the secret variable, and the inline
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
  (identity names, context names and `credentialRef` are lower-case letters,
  digits and dashes, starting with a letter), a `type` that is not exactly
  `cloud` or `onprem`, a missing `endpoint` on a product entry, or the
  `credentialRef` / `clientSecretVariable` rule: exactly one of them belongs on
  an identity, and which one is decided by `auth.kind`.
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
- **`contexts.unknown_identity`.** `wso2 context create --identity` named an
  identity the document does not declare. Logging in is the only thing that
  creates one, so run `wso2 login` first, or check the name against the
  `identities` array.
- **`contexts.identity_exists`.** `wso2 login --url` named a context whose
  identity is already configured against a different issuer or a different
  client ID. The message names both the value on file and the one you asked
  for. Logging in never replaces an identity, because the issuer and client it
  records are not written down anywhere else; log in under another name with
  `--context`, or correct the flag you mistyped.
- **`contexts.identity_name_underivable`.** `wso2 login --url` was given an
  issuer with no host a name can be made from — a bare IP address, or a host
  whose first label starts with a digit — and no `--context` to name the
  identity instead. A name is lower-case letters, digits and hyphens, starting
  with a letter. Pass `--context <name>`; nothing was written.
- **`contexts.context_exists`.** `wso2 context create` was given a name the
  document already declares. Creating a context never replaces one, because the
  organization, project and identity it recorded are not written down anywhere
  else. Choose another name.
- **`contexts.product_exists`.** `wso2 identity add-product` named a product
  namespace the identity already records. Recording one never overwrites
  another on its own, because the endpoint, audience and scopes it held are not
  written down anywhere else; the ordinary way to reach this is a second run
  from shell history with one flag corrected. `wso2 identity list` shows what
  is recorded, and `--replace` overwrites it, replacing the whole record rather
  than merging with it.

### The context commands: `shell.*`

These are about what you typed, not about the file. Nothing is written when one
of them is reported.

- **`shell.missing_required_flag`.** A flag the command cannot proceed without
  was not given. `wso2 context create` reports it for `--identity`: a context
  authenticates as an identity, and `wso2 login` is what creates one. `wso2
  login --url` reports it for `--client-id`, which it asks for at a terminal
  and refuses to guess anywhere else — there is no WSO2-published client for a
  self-hosted deployment, so the value can only come from the application you
  registered. `wso2 identity add-product` reports it for `--endpoint`, which
  nothing can discover: a self-hosted deployment publishes no catalogue of what
  it serves. The message says why nothing was asked: `--no-input`,
  `WSO2_NO_INPUT`, or standard input that is not a terminal. Not to be confused
  with `shell.missing_flag_value`, which means a flag was given without the
  value it needs.
- **`shell.invalid_argument`.** A value you typed is not one the command can
  use. For `wso2 context create`, and for `wso2 login --context`, it is a name a
  context may not have: names are lower-case letters, digits and hyphens,
  starting with a letter, at most 64 characters. For `wso2 login --url` it is a
  value that is not an issuer URL, and a missing `https://` is the usual cause.
  `wso2 identity add-product` reports it for a product namespace, which follows
  the same name rule, and for a product the context document will not hold: an
  endpoint that embeds user information, an endpoint no URL parser reads, and a
  product an identity bound to one protected resource cannot carry. The
  endpoint is never repeated back, because a rejected one is the likeliest
  place for a credential to have been typed by mistake. Nothing was written, so
  retyping the command is the whole fix.
- **`shell.missing_argument`, `shell.unexpected_argument`.** The command was
  given too few or too many arguments. The recovery shows the shape it expects.

### `auth.context_not_selected`

There is no context document at all, or it declares no context to select.
Create `~/.wso2/cli/contexts.json` as in section 2.

### `shell.unknown_command`

The first word was not `help`, `login` or `version`, and no installed module
owns that namespace. See the caveat at the end of section 5.2.

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
- **TLS is not trusted.** Common against a local Identity Server or Thunder
  deployment with a self-signed certificate. Add the deployment's certificate to
  the operating system trust store. Each walkthrough has the commands. The
  shell deliberately has no flag to skip verification.
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

### `auth.login_required`

No usable session. Either you have not logged in for this `credentialRef`, or
the deployment has stopped accepting the stored refresh token: it was revoked,
it expired, or a concurrent run rotated it away. Run `wso2 login` again.

### `auth.logout_not_required`

You ran `wso2 logout` against a context whose identity acquires access inline
and never holds a session, which in practice means a `client-credentials`
identity.
Nothing is stored for it, so there is nothing to end. Remove the credential from
the environment to stop the shell acquiring access with it.

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

> this release cannot switch the ... identity's session out of its home tenant

Your context's `organization` names something other than the identity's
`auth.tenant`. Make them match, or add a second identity whose home tenant is
the organization you are targeting and log in as it.

### `auth.product_not_configured`

The module asked for something this identity does not register. The message
names both sides. Either the module's namespace is missing from `products`, or
its entry sets no `audience`, or a scope it asked for is not in the `scopes`
list. Fix the context document.

An `audience` that differs from the one the module asks for is *not* this
refusal, and is normal: the values come from different vocabularies. What must
hold is that the deployment binds the token to the `audience` recorded here,
which is proved when the token arrives and reported as
`auth.narrowing_unavailable` when it fails.

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

You ran `wso2 login` on a context whose identity carries its own credential.
There is no session to establish; just run the command (section 5).

### `auth.non_interactive`

`wso2 login` was run with `--no-input`, or with `WSO2_NO_INPUT` set. This is
the guard that stops a CI job from waiting forever on a browser, or, on a
device context, from waiting forever on an approval no one is there to give.
The message names which of the two it refused.

### `auth.kind_not_implemented`

The context's `auth.kind` is `pat`. The schema names it; this release does not
implement it. Use `oauth-browser`, `oauth-device`, or `client-credentials`.

### `auth.session_issuer_mismatch`

The stored session was established against a different issuer than the context
now names. You changed the `issuer` after logging in. Run `wso2 login` again.

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
