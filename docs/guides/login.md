# Logging in with the WSO2 CLI

**Status:** Working draft
**Last reviewed:** 2026-08-10
**Related:** [Architecture](../architecture.md),
[product requirements](../product-requirements.md),
[shell commands](../reference/commands.md),
[authentication context examples](../examples/authentication-contexts.md)

How to log in, what login stores, and how a CI job authenticates without one.
This works the same whichever product backs your deployment.

## Before you start

You need an OAuth application registered in your deployment. That part is
product-specific, so start with your product's walkthrough and come back here:

| Deployment | Walkthrough |
| --- | --- |
| Asgardeo | [Registering in Asgardeo](login-asgardeo.md) |
| WSO2 Identity Server 7.x | [Registering in Identity Server](login-identity-server.md) |
| ThunderID | [Registering in ThunderID](login-thunder.md) |

Each one ends by handing you four values: **issuer**, **client ID**,
**audience**, and **scopes**. Section 5 lists what the shell needs from an
application, if you would rather see the requirements first.

---

## 1. Log in

On a machine with nothing configured, name the issuer and the application you
registered:

```sh
wso2 login --url https://idp.customer.example --client-id wso2-cli
```

This creates an identity, creates a context that authenticates with it, and
signs you in. Afterwards, `wso2 login` on its own re-authenticates the default
context, and `wso2 login --context acme-dev` names another one.

**Naming.** Without `--context`, the identity and the context are both named
after the issuer host, with each dot replaced by a hyphen, so
`idp.customer.example` becomes `idp-customer-example`. `--context <name>` names
them both directly. You type that name on every `--context` flag and every
`wso2 context use`, so pass a short one. To add a shorter handle later:

```sh
wso2 context create <name> --identity <identity>
```

**What login refuses.** An issuer whose host cannot become an identity name,
unless you pass `--context`. A name must start with a lower-case letter, so a
bare IP address, or a host whose first label starts with a digit, is refused
rather than turned into a name you could not have predicted. A `--url` that is
not an absolute
`http` or `https` URL, so a missing `https://` is reported where you typed it. A
login that would change the issuer or client ID of an existing identity. A
context document this shell may not overwrite, which is refused before the
browser opens. Nothing is written unless the login succeeds.

**Reaching a product.** A new identity reaches no product yet, because a
self-hosted deployment publishes no catalogue of what it serves. Record each
one:

```sh
wso2 identity add-product <identity> <namespace> \
  --endpoint <url> --audience <aud> --scopes <scope1,scope2>
```

`--scopes` takes a comma-separated list. `wso2 identity list` shows what is
recorded, and `--replace` overwrites a namespace's existing record.

### What happens during login

1. The shell reads the issuer's discovery document and confirms it advertises
   `S256`.
2. It binds the first free port of 10425-10428 on `127.0.0.1`.
3. It prints the authorization URL to standard error and opens your browser
   there.
4. You sign in and consent.
5. The browser is redirected back to the loopback listener, and the shell
   exchanges the code, with the PKCE verifier, for tokens.
6. It verifies the identity token, including the nonce it sent.
7. It writes the refresh token to the operating system's secure store and
   reports who you are.

The command waits up to five minutes. Set `WSO2_NO_BROWSER=1` to print the URL
without opening anything; you still have to finish the sign-in in a browser that
can reach `127.0.0.1` **on this machine**.

### 1.1 Logging in without a browser

Over SSH or inside a container, the browser login cannot finish: it waits for a
redirect to `127.0.0.1` on *this* machine, and your browser's `127.0.0.1` is
somewhere else. The device authorization grant solves that, and you approve on
any other device.

Set `"kind": "oauth-device"` on the identity when it can only be established
this way, either because the deployment cannot register loopback callback URLs
or because its users are never at a machine with a reachable browser. It is a
property of the identity, not of where you happen to be sitting today.

> `wso2 login --device-code`, for switching per invocation, is not in this
> release. To have both, keep two identities, one `oauth-browser` and one
> `oauth-device`, with different `credentialRef` values and a context for each.

**Registration.** Add the **Device Code** grant to the application's allowed
grant types. Nothing else changes; the loopback callback URLs are unused by this
flow. Asgardeo and Identity Server 7.x both support it. Thunder does not
register a device grant handler at all, so its deployments cannot use this flow.

**The context document** is the section 2 document with one word changed:

```json
      "auth": {
        "kind": "oauth-device",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
        "tenant": "acme",
        "credentialRef": "acme-cloud-device"
      }
```

**What you see:**

```console
$ wso2 login

To log in, visit:

    https://api.asgardeo.io/t/acme/authenticationendpoint/device.do

and enter the code:

    WDJB-MJHT

Or open this link, which carries the code:

    https://api.asgardeo.io/t/acme/authenticationendpoint/device.do?user_code=WDJB-MJHT

Waiting for you to approve this login...
```

Open the first URL on your phone or laptop, type the code, and sign in. The
terminal finishes on its own. The shell polls at the rate the deployment asks
for and stops when the code expires, never later than fifteen minutes.

A browser login always reports a `Subject`. A device login reports one only if
the deployment returned an identity token, which RFC 8628 does not require. The
session works either way.

---

## 2. The context document

`wso2 login` and `wso2 context create` write this file for you. Read it to check
what they wrote, or edit it directly when a context names an organization, a
project, or more than one product.

```
~/.wso2/cli/contexts.json
```

Set `WSO2_HOME` to use a different state root; it must be an absolute path, and
the file then lives at `$WSO2_HOME/cli/contexts.json`. If you are writing the
file by hand, create the directory first:

```sh
mkdir -p ~/.wso2/cli
```

A context document names **identities** and **contexts**. An identity says how
to authenticate and what it may reach. A context picks an identity and the
organization to act within.

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

Fill in the four values from your walkthrough: `issuer`, `clientId`, `audience`,
and `scopes`.

> **`audience` is the one field that is not portable between products.** It is
> the client ID on [Asgardeo](login-asgardeo.md#1-what-is-different-about-asgardeo),
> the API resource identifier on
> [Identity Server](login-identity-server.md#1-what-is-different-about-identity-server),
> and an absolute resource-server URI on
> [Thunder](login-thunder.md#1-what-is-different-about-thunder). The example
> above shows the resource-identifier form, so against Asgardeo it needs the
> client ID here.

### 2.1 Field reference

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Must be `2`. |
| `defaultContext` | The context used when no `--context` flag and no `WSO2_CONTEXT` is given. Must name a context declared below. |
| `identities[].name` | Lower-case letters, digits and dashes, starting with a letter, up to 64 characters. |
| `identities[].type` | `cloud` or `onprem`. Nothing else is accepted. |
| `auth.kind` | `oauth-browser` for a person at a browser. `oauth-device` for an identity that can only be established without one (section 1.1). `client-credentials` for CI (section 4). `pat` is named by the schema but not implemented. |
| `auth.issuer` | The issuer, verbatim from its discovery document. |
| `auth.clientId` | The registered public client. |
| `auth.tenant` | The identity's home organization. |
| `auth.provider` | Names the product when the shell must ask it for tokens in a product-specific shape. Required for Thunder; see [its walkthrough](login-thunder.md#9-log-in-and-check-what-it-wrote). |
| `auth.credentialRef` | The name the session is stored under in the OS secure store. Required for `oauth-browser` and `oauth-device`, and not allowed for `client-credentials`. Same character rules as an identity name. |
| `products.<namespace>` | What this identity may reach for one module. The namespace is the module's own name, with the same character rules as an identity name. |
| `products.<namespace>.endpoint` | The product's base URL. Required on every product entry, and must be an absolute `http` or `https` URL with a host. |
| `products.<namespace>.audience` | What the issued token's `aud` claim must carry. A grant whose `aud` does not carry it is refused. It is not compared against the audience the module asks for: a module names its API by a logical name compiled into it, while this is the concrete string this deployment stamps into `aud`. |
| `products.<namespace>.scopes` | The permissions this identity carries. A module asking for one that is not listed is refused. |
| `contexts[].organization` | The organization to act within. Either leave it out, or set it to the identity's `auth.tenant`. This release cannot switch a session out of its home tenant. |

Check the document parses before it opens a browser:

```sh
wso2 login --context acme-dev
```

---

## 3. Sessions and logout

- **The refresh token** goes to the operating system's secure store, under the
  service `wso2-cli` and the name in `credentialRef`. That is Keychain on macOS,
  Secret Service on Linux, and Credential Manager on Windows.
- **Nothing under `~/.wso2` holds a credential.** The state root holds the
  context document, the managed module store, and the lock files that keep
  refresh-token rotation single-writer.
- **Modules never receive the session.** When a module needs access, the shell
  exchanges the refresh token for a short-lived access token narrowed to what
  that module declared, proves the result carries it, and hands over only that.

`wso2 logout` asks the deployment to revoke the refresh token and removes the
secure-store entry. It reports one of three outcomes:

| Outcome | What it means |
| --- | --- |
| `confirmed` | The deployment accepted the request. RFC 7009 requires the same answer for an unknown token as for a live one, so this means it was told, not that anything was found to retract. |
| `not-attempted` | The deployment publishes no `revocation_endpoint`, so it was never asked. Its own copy of the session stands until it expires. |
| `failed` | The deployment was asked and did not accept, or could not be reached. Most likely it requires a confidential client on that endpoint. |

The secure-store entry goes, and the command succeeds, under all three; only
what the shell claims changes. See
[ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md).

Two things logout does not do. It does not end the browser single-sign-on
session at the identity provider, so a later `wso2 login` may complete without
prompting you. And because a session is keyed by `credentialRef`, ending it ends
it for every context naming that identity; the command lists them.

A `client-credentials` identity has no session to end and is refused with
`auth.logout_not_required`.

---

## 4. CI: authenticate without a login

A CI job has no browser and no secure store, so it uses a machine-to-machine
identity that carries its own credential and exchanges it on every command.
**There is no login step in CI**, and a job that runs `wso2 login` is refused
with `auth.login_not_required`.

Register the machine-to-machine application first:
[Asgardeo](login-asgardeo.md#8-a-machine-to-machine-client-for-ci-if-you-need-one),
[Identity Server](login-identity-server.md#10-a-confidential-client-for-ci-if-you-need-one),
[Thunder](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one). All
three come down to the same thing: the Client Credentials grant and nothing
else, no redirect URLs, no PKCE, the same API resource and scopes as the browser
application, JWT access tokens, and a recorded client ID and secret.

### 4.1 Write the CI context

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

Two differences from section 2, both enforced by the schema:

- `clientSecretVariable` replaces `credentialRef`. It names an environment
  variable and is not the secret itself. Upper-case letters, digits and
  underscores, starting with a letter.
- `credentialRef` must not appear on a `client-credentials` identity, and
  `clientSecretVariable` must not appear on an `oauth-browser` one.

The secret never goes in this file, so the file is safe to commit.

This example is an Asgardeo identity. Against another deployment, substitute
`audience` per the rule in section 2, and add `"provider": "thunder"` for a
Thunder deployment. Thunder refuses a client-credentials grant that names no
protected resource, and the document parses either way, so leaving `provider`
out fails at the first command rather than at the first read. The
[Thunder walkthrough](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one)
shows the whole identity.

### 4.2 Wire the job

The secret comes from the CI system's secret store into the named variable.
There is no login step.

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

`WSO2_HOME` must be absolute. `WSO2_CONTEXT` selects the context without a flag.

Set `WSO2_NO_INPUT=1` so a stray `wso2 login` fails loudly instead of waiting on
a browser that will never open; `--no-input` says the same for one invocation.
Either way a browser or device login is refused with `auth.non_interactive`. See
[Non-interactive use](../reference/commands.md#non-interactive-use).

Each command exchanges the client secret for an access token narrowed to what
the module asked for. The secret is read into process memory for one grant, is
never written to the state root, and is never passed to a module.

---

## 5. What the shell needs from a deployment

The shell signs a person in with the browser Authorization Code flow and PKCE,
keeps the refresh token in the operating system's secure store, and derives a
separate short-lived access token for each module that asks for one. That puts
five requirements on the application you register:

| Requirement | Why |
| --- | --- |
| **A public client**, no client secret | The shell is installed on people's machines and cannot hold one. PKCE replaces it. |
| **PKCE mandatory, `S256`** | The shell refuses to start a login against an issuer that does not advertise `S256`. |
| **Four loopback callback URLs** | The shell listens on `127.0.0.1` and takes the first free port of four, so all four must be registered. |
| **The refresh token grant** | The stored session *is* the refresh token. Without this grant, login succeeds and every later command fails. |
| **An API resource with scopes, and JWT access tokens** | The shell proves the token a module receives carries exactly what that module asked for. It cannot prove that about an opaque token, so it refuses. See `auth.narrowing_unavailable` below. |

The four callback URLs:

```text
http://127.0.0.1:10425/callback
http://127.0.0.1:10426/callback
http://127.0.0.1:10427/callback
http://127.0.0.1:10428/callback
```

Four rather than one because another process on the machine may already hold
the first choice, and the shell then falls back to the next.

For a browserless login, add the device code grant as well (section 1.1).

The products differ on the fifth requirement, and that difference decides what
you write as `audience`. Asgardeo binds `aud` to the client ID. Identity Server
binds it to the API resource identifier, once that is in the application's
audience list. Thunder names a resource server per request. Each walkthrough
states its product's answer.

---

## 6. Troubleshooting

Every refusal carries a typed code. Failures specific to one product are in that
product's walkthrough; [Thunder's](login-thunder.md#10-troubleshooting) is the
longest.

### The context document: `contexts.*`

None of these reaches a browser, and nothing is written when one is reported.

| Code | What to do |
| --- | --- |
| `contexts.document_malformed` | The document is not valid, and the message names the defect. Usual causes: a name breaking the character rules, a `type` that is not `cloud` or `onprem`, a missing `endpoint` on a product entry, or both or neither of `credentialRef` and `clientSecretVariable` (`auth.kind` decides which one belongs). Section 2.1 is the field reference. |
| `contexts.document_unreadable` | The file exists but could not be read. Check permissions, or delete it to run without a context. |
| `contexts.document_frozen` | The document declares a schema version this shell does not write, so a command that would have replaced it refused. Either it is a version 1 document, or a newer WSO2 CLI on this machine manages it. Move it aside, or run the CLI version that manages it. |
| `contexts.document_unwritable` | The state root is not writable by you. Check `~/.wso2/cli` or `$WSO2_HOME/cli`. |
| `contexts.document_busy` | Another `wso2` invocation held the update lock too long. Writing takes no network call, so a holder that slow is stuck. Retry, then look for a stalled `wso2` process. |
| `contexts.schema_unsupported` | `schemaVersion` must be `2`. |
| `contexts.unknown_context` | You named a context the document does not declare. Compare it against the `contexts` array and `defaultContext`. |
| `contexts.unknown_identity` | `wso2 context create --identity` named an identity that does not exist. Only logging in creates one. |
| `contexts.identity_exists` | `wso2 login --url` named a context whose identity is already configured against a different issuer or client ID. Logging in never replaces an identity. Use another `--context`, or correct the flag. |
| `contexts.identity_name_underivable` | The issuer's host cannot become a name (a bare IP address, or a first label starting with a digit), and no `--context` was given. Pass `--context <name>`. |
| `contexts.context_exists` | `wso2 context create` was given a name that already exists. Creating never replaces. Choose another name. |
| `contexts.product_exists` | `wso2 identity add-product` named a product the identity already records. `wso2 identity list` shows what is recorded, and `--replace` overwrites the whole record. |

`contexts.document_malformed` is about content, not version. A file refused over
its `schemaVersion` reports `contexts.document_frozen` instead.

### What you typed: `shell.*`

- **`shell.missing_required_flag`.** A required flag was not given, and the
  message says why nothing was prompted for (`--no-input`, `WSO2_NO_INPUT`, or
  standard input that is not a terminal). `wso2 context create` needs
  `--identity`; `wso2 login --url` needs `--client-id`, which it will not guess,
  because only the application you registered has that value; `wso2 identity
  add-product` needs `--endpoint`, which nothing can discover. Not the same as
  `shell.missing_flag_value`, which means a flag was given without its value.
- **`shell.invalid_argument`.** A value the command cannot use. Names are
  lower-case letters, digits and hyphens, starting with a letter, at most 64
  characters. For `--url` it is usually a missing `https://`. Retyping the
  command is normally the whole fix. The exception is a product an identity
  bound to one protected resource cannot carry, where the constraint is on the
  identity: the recovery names `--replace` and a second `wso2 login --context`.
- **`shell.missing_argument`, `shell.unexpected_argument`.** Too few or too many
  arguments. The recovery shows the expected shape.
- **`shell.unknown_command`.** The first word is neither a shell command
  (`context`, `help`, `identity`, `login`, `logout`, `module`, `version`) nor an
  installed module's namespace. `wso2 help` lists the shell's commands, and
  `wso2 module list` shows what is installed.

### `auth.context_not_selected`

There is no context document, or it declares no context. Run `wso2 login --url
<issuer> --client-id <id>`, or `wso2 context use <name>` to select an existing
one. `wso2 context list` shows what is configured.

### `auth.discovery_failed`

> the shell could not read the identity provider's OpenID configuration

Likeliest first:

- **The issuer is not exact.** It must equal the `issuer` value in the
  deployment's discovery document, character for character. Fetch
  `<issuer>/.well-known/openid-configuration` and compare. The three products do
  not share a shape: Asgardeo's carries `/oauth2/token` under a tenant, Identity
  Server's carries `/oauth2/token` under a host and port, and Thunder's is the
  bare origin.
- **TLS is not trusted.** Common against a local Identity Server or Thunder with
  a self-signed certificate. Add its certificate to the OS trust store; each
  walkthrough has the commands. The shell has no flag to skip verification.
- **The machine cannot reach the issuer.** Proxy, VPN, firewall.
- **The issuer does not advertise `S256`.** Set PKCE to mandatory.

Two variants carry the same code:

> the identity provider does not advertise the device authorization grant

Enable the Device Code grant (section 1.1), or use an `oauth-browser` context.
Thunder has no device grant at all.

> no loopback callback port is available for the browser login

All four of 10425-10428 are in use. Find the holders and free one:

```sh
lsof -nP -iTCP@127.0.0.1:10425-10428 -sTCP:LISTEN   # macOS, Linux
```

The shell will not fall back to an unregistered port, because the deployment
would reject the redirect and the error would name the wrong problem.

### `auth.narrowing_unavailable`

The shell got a token but could not prove it was narrowed to what the module
asked for, so it refused to hand it over. This is the designed behavior: a
module that silently received the whole session's authority would hold access
nobody decided to give it.

| The message says | What it means | What to change |
| --- | --- | --- |
| "in a form the shell cannot check" | The access token is opaque. | Set the application to issue JWT access tokens. |
| "did not state which permissions it issued" | The deployment returned no scope, and the token claims none. | Check the API resource is authorized on the application with the scopes selected. |
| "asked for the permissions X and the deployment issued Y" | The deployment ignored the narrower request. | The deployment does not narrow on this grant. See below. |
| "is not bound to the ... audience" | The token's `aud` does not carry your audience. | Your `audience` names something this deployment never puts in `aud`. It is the client ID on [Asgardeo](login-asgardeo.md#1-what-is-different-about-asgardeo), the API resource identifier on [Identity Server](login-identity-server.md#1-what-is-different-about-identity-server) and only once it is in the application's audience list, and the resource server URI on [Thunder](login-thunder.md#1-what-is-different-about-thunder). Failing that, the resource is not authorized on the application. |
| "refused to narrow this session" | The token endpoint answered `invalid_scope`. | A scope in your context document is not one the application is authorized for. |

A deployment that will not narrow is a property of that deployment. Both
products measured so far do narrow: on 2026-08-06, against a live Asgardeo
tenant and Identity Server 7.3.0, a session carrying two permissions was
refreshed down to one and answered with exactly that one
([research](../research/asgardeo-redirect-uri-and-scope-narrowing.md)).

### Other `auth.*` codes

| Code | Cause and fix |
| --- | --- |
| `auth.login_required` | No usable session, or the deployment stopped accepting the stored refresh token because it was revoked, expired, or rotated away by a concurrent run. Run `wso2 login` again. |
| `auth.logout_not_required` | The identity acquires access inline and holds no session. Remove the credential from the environment instead. |
| `auth.keyring_unavailable` | The OS secure store could not be used. On headless Linux this usually means no Secret Service is running. Start a keyring daemon, or use a `client-credentials` context. |
| `auth.organization_switch_unsupported` | Your context's `organization` is not the identity's `auth.tenant`. Make them match, or add a second identity whose home tenant is the target. |
| `auth.product_not_configured` | The module asked for something this identity does not register. Either the namespace is missing from `products`, its entry sets no `audience`, or a requested scope is not listed. An `audience` that differs from the one the module asks for is normal and is not this refusal. |
| `auth.audience_not_declared`, `auth.scope_not_declared` | The module asked for more than its own installation declares. Reinstall the module. |
| `auth.login_not_required` | The identity carries its own credential. There is no session to establish; just run the command. |
| `auth.non_interactive` | `wso2 login` ran with `--no-input` or `WSO2_NO_INPUT`. This guard stops CI waiting forever on a browser or an approval. |
| `auth.kind_not_implemented` | `auth.kind` is `pat`, which this release does not implement. Use `oauth-browser`, `oauth-device`, or `client-credentials`. |
| `auth.session_issuer_mismatch` | The stored session was established against a different issuer than the context now names. Run `wso2 login` again. |

### `auth.credential_unavailable`

On a `client-credentials` context, the variable named by `clientSecretVariable`
is unset, empty, or holds a secret the deployment rejects. A variable exported
as an empty string counts as unset.

On a browser login, the flow ended without producing tokens: you closed the
browser, someone denied consent, or the deployment redirected back with an
error. If the browser reached "Login complete" and the exchange succeeded, the
identity token was not one the shell would accept:

| The message says | What to change |
| --- | --- |
| "was not signed by the identity provider's keys" | Usually the `issuer` names a different deployment than the one that signed you in. |
| "was issued for a different application" | Confirm `clientId` names the application this issuer signed you in to. |
| "had already expired" | Check this machine's clock. |
| "the shell could not read the signing keys the identity provider publishes" | Confirm the machine can reach the issuer's `jwks_uri`. |

On a device login, the message says which of four endings it was:

| The message says | What to do |
| --- | --- |
| "the login was declined at the identity provider" | Run `wso2 login` again and approve it. Check the code on screen matches your terminal. |
| "the approval window closed before this login was approved" | The device code expired. Run it again and approve promptly. |
| "this login was not approved in time" | The same, reached by the shell's own deadline. |
| "would not start a device authorization" | Confirm `clientId`, and that the application is registered for the device grant. |

> **A note on negative certificate serials.** Many WSO2 deployments publish a
> token-signing certificate whose X.509 serial number is negative, which RFC
> 5280 forbids and Go has rejected since 1.23. The shell no longer reads the
> `x5c` field where that certificate travels, so such a deployment logs in
> normally and the old `GODEBUG=x509negativeserial=1` workaround is no longer
> needed.

---

## 7. Testing against a real deployment

This repository ships a live smoke run and two one-time experiments behind the
`smoke` build tag, so they never run in the default gate. They write to a
temporary state root and store their session under `wso2-cli-smoke`, so your own
`~/.wso2` is untouched.

Run the deterministic suites first. They already drive login, session, and
brokered acquisition against a fake OIDC issuer that signs real JWTs, so the
live runs only add evidence about a deployment:

```sh
make test          # the default gate, including the acceptance suite
make acceptance    # the architecture-proof gate
make smoke-build   # proves the live runs still compile
make lint
```

Confirm the live runs skip honestly while you are still unconfigured:

```sh
go test -tags smoke ./test/smoke -run TestLoginSmoke -v
# --- SKIP: TestLoginSmoke — no live deployment is configured: set WSO2_SMOKE_ISSUER, ...
```

Describe the deployment in a file rather than in your shell:

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
it exists and print which file they read. Keep one per deployment and name it
with `SMOKE_ENV=test/smoke/is.env`. Values in the file overwrite what the shell
exported, so a leftover export cannot outrank the file you just edited. `*.env`
is git-ignored.

> **`WSO2_SMOKE_CLIENT_ID` and `WSO2_SMOKE_AUDIENCE` are different fields that
> Asgardeo happens to force to the same value.** On Identity Server and Thunder
> they differ. Copying one deployment's file and changing only the issuer is the
> mistake to expect; it costs a browser sign-in and ends in
> `auth.narrowing_unavailable`.

Confirm the issuer before spending a sign-in on a value that is close but not
exact:

```sh
curl -s "$WSO2_SMOKE_ISSUER/.well-known/openid-configuration" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["issuer"]); print(d["code_challenge_methods_supported"])'
```

The printed issuer must equal `WSO2_SMOKE_ISSUER` character for character, and
`S256` must appear.

```sh
make smoke-login          # log in, prove the session persisted, broker one acquisition
make smoke-login-device   # the same, approved on another device (section 1.1)
make empirical-asgardeo   # measure both behaviors against your own tenant
```

A passing run ends with the acquisition granted:

```text
LOGIN SMOKE: granted — access of 1219 characters bound to "<audience>", expiring 20:07:22Z
```

A run ending in `auth.narrowing_unavailable` also passes, on purpose: the shell
refusing to hand a module more authority than it asked for is the designed
outcome.

Read [`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md) before recording any
verdict. It lists every variable these runs read and says which verdicts are
catch-all branches that need corroborating.
