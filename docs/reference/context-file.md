# Context file

**Status:** Reference
**Related:** [shell commands](commands.md), [module catalog](module-catalog.md)
**Last reviewed:** 2026-09-17

A **context** is one named target: how the shell logs in (its `login`), every
product it can reach from that login (its `products`), the secure-store
entries its sessions live under (its `credentialRef`), and optionally the
organization and project to act within. Each context owns its own sessions —
no two contexts share a `credentialRef` — so two targets that need the same
login are two contexts that each log in, or one context whose organization
`wso2 org use` switches.

Two files hold this shape, for two different jobs:

- The **local document** (`contexts.json`) is complete: every value a login
  and a command need is written out, and the shell reads nothing else at
  command time.
- The **input file** is short and shareable: a platform team writes it once,
  commits it, and every developer applies it. It leaves out whatever the
  installed products' descriptors already know.

```sh
wso2 context apply -f team-context.json --use local
wso2 login
```

`apply` fills in the input file's gaps from each installed product's
descriptor and writes complete records to the local document. Reading the
descriptor at apply time rather than at login time is deliberate ("frozen
defaults"): a later `wso2 product update` changes no login until the file is
applied again. `wso2 doctor` and `wso2 context show` say when a record
differs from what the installed product would write now.

## Where it lives

```
~/.wso2/cli/contexts.json
```

Set `WSO2_HOME` to use a different state root; it must be an absolute path,
and the file then lives at `$WSO2_HOME/cli/contexts.json`. `wso2 context show`
reports the path whether or not a document has been written there yet.

## The local document

```json
{
  "schemaVersion": 4,
  "defaultContext": "local",
  "contexts": [
    {
      "name": "local",
      "type": "onprem",
      "credentialRef": "local",
      "login": {
        "kind": "oauth-browser",
        "issuer": "http://localhost:8501",
        "clientId": "wso2-cli",
        "provider": "thunder",
        "product": "identity"
      },
      "products": {
        "identity": {
          "url": "http://localhost:8501",
          "audience": "https://localhost:8090/mcp",
          "scopes": ["system"]
        },
        "api": {
          "url": "http://localhost:9251",
          "audience": "http://localhost:9251",
          "grant": { "kind": "exchange" },
          "gateway": { "url": "http://localhost:9091", "audience": "http://localhost:9091" }
        }
      }
    }
  ]
}
```

This is what `wso2 context apply` writes for the short `team-context.json`
below it — a login product (`identity`, direct) and a second product (`api`)
reached by exchanging the login session's token, each with its own gateway
record. Two things this asserts, and either can be wrong at runtime: every
listed product accepts access derived from that session (a product that
validates only its own resident issuer does not belong here), and the user is
*authorized* for each — one login authenticates for all of them, it does not
authorize.

### Field reference

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Must be `4`. Versions 2 and 3 are read and upgraded in place the first time this shell runs; version 1 is read and never rewritten. |
| `defaultContext` | The context used when no `--context` flag and no `WSO2_CONTEXT` is given. May be absent, which selects nothing. |
| `contexts[].name` | Lower-case letters, digits and dashes, starting with a letter, up to 64 characters. |
| `contexts[].type` | `cloud` or `onprem`. Selects defaults and wording, never structure. Required here; `context apply` derives it from the issuer when an input file omits it. |
| `contexts[].credentialRef` | The name this context's sessions are stored under in the OS secure store: the login session under the reference, and each product's own under `<ref>.<product>`. **Required** for `oauth-browser`, `oauth-device` and `pat`; **not allowed** for `client-credentials`. Unique across the document; it stays when the context is renamed, so no session moves. |
| `login.kind` | `oauth-browser` for a person at a browser, `oauth-device` for a context that can only be established without one, `client-credentials` for CI. `pat` is named by the schema but not implemented in this release. |
| `login.issuer` | The issuer, verbatim from its discovery document. Must be `https`; plain `http` is accepted only on a loopback host (`localhost`, `127.0.0.0/8`, `::1`), because the shell sends credentials to it. The same rule applies to a grant's own issuer and to every endpoint the issuer's discovery document names — a plaintext one is refused as `auth.discovery_failed`. |
| `login.clientId` | The registered public client. |
| `login.tenant` | The home tenant the login belongs to at the issuer; derived from an Asgardeo issuer when absent. Not the same as `organization`, which is what commands target. |
| `login.provider` | Names the product when the shell must ask it for tokens in a product-specific shape: `asgardeo`, `identity-server` or `thunder`. Required for Thunder. |
| `login.narrowing` | `scoped-refresh` or `token-resource`; wins over `provider` when given. Optional. |
| `login.product` | The product the login authorization runs for. **Required** once the context reaches a direct product, so recording another product can never move the login from under the sessions already stored. |
| `login.clientSecretVariable` | `client-credentials` only. The **name** of an environment variable holding the secret, never the secret. |
| `products.<namespace>` | What this context may reach for one module. The namespace follows the same character rules as a context name. |
| `products.<namespace>.url` | The product's base URL. **Required**, and must be an absolute `http` or `https` URL with a host. |
| `products.<namespace>.audience` | What the issued token's `aud` claim must carry. Not compared against the audience a module asks for by its own logical name — this is the concrete string *this* deployment stamps into `aud`. |
| `products.<namespace>.scopes` | The permissions this context carries. A module asking for one that is not listed is refused. |
| `products.<namespace>.grant` | How a product is reached when the login session does not already cover it: `exchange` (the login session's token exchanged per command, RFC 8693), `jwt-bearer` (an identity token from the login session presented at the product's own issuer), or `federated` (a public client at the product's own issuer, through the same browser sign-on). Absent for a product the login session covers directly. |
| `products.<namespace>.clientIdVariable` / `clientSecretVariable` | A credential of the product's own, for a `client-credentials` context whose machine client the product cannot map to its roles. Names, never values. |
| `products.<namespace>.gateway` | The product's gateway, when it has one: its own `url`, `audience` and `scopes`. |
| `contexts[].organization` | The organization to act within. Either leave it out, or set it to `login.tenant` — this release cannot switch a session out of its home tenant. |
| `contexts[].project` | The project inside the organization to narrow the target to. |

## The input file

```json
{
  "contexts": [
    {
      "name": "local",
      "login": { "product": "identity" },
      "products": {
        "identity": { "url": "http://localhost:8501" },
        "api": {
          "url": "http://localhost:9251",
          "gateway": { "url": "http://localhost:9091" }
        }
      }
    }
  ]
}
```

`name`, one way to log in, and a `url` per product are all that is required.
Every member of the local document may also appear here except
`credentialRef` and `defaultContext`, which apply refuses; what you do state is
taken as written — state `audience`, `scopes`, `grant`, a `clientIdVariable` or
`clientSecretVariable` only when the deployment differs from what the
installed product declares; otherwise let `apply` fill them in from the
descriptor. `products.<namespace>.version` pins the module version to
install; it is not stored in the local document.

### Two ways to log in

Every context has to say how it logs in. Pick one:

**Through a product** — the usual way, and what the file above does:

```json
"login": { "product": "identity" }
```

The shell takes the issuer and the client id from that product's descriptor,
so the namespace named must also be a key under `products` — it is the one
whose `url` the issuer is derived from.

**Against a bare issuer** — for a context with no login product:

```json
"login": { "issuer": "https://id.example.com", "clientId": "wso2-cli" }
```

Nothing is derived here, so both members are stated directly. You may also
state `issuer` or `clientId` next to `login.product`; what you state wins over
what the descriptor would fill in.

## What apply does

1. Reads the whole file and refuses it as a whole before it installs anything.
2. Installs any product a context names and this machine does not have.
   `--no-install` installs nothing, and then every product must be installed
   already or stated in full. `--update-products` installs a pinned version
   that does not match the one installed.
3. Fills in the frozen defaults from each installed product's descriptor:
   issuer, client id, provider, audience, scopes, grant, gateway audience and
   gateway scopes. `type` and `login.tenant` come from the issuer instead. A
   fault only a descriptor can reveal — a product that declares no grant, or
   no way for a machine context to reach it — is raised here, still before
   anything is written.
4. Ends the sessions that no longer match.
5. Writes **complete** context records to `contexts.json`, selecting the one
   named by `--use`. Nothing else changes the selection.

A context in the file replaces the context of the same name **whole**,
keeping its credential reference; a client-credentials context holds none.
Contexts the file does not name are left alone. A session ends when its
binding changed — its issuer, client, strategy, scopes or resource — so a new
URL ends a session when it moves the issuer or the resource the session was
bound to.

```sh
wso2 context apply -f team-context.json --dry-run
```

`--dry-run` prints what would be installed, created or replaced, a
field-by-field diff for each replaced context, and the sessions that would
end. It writes nothing. When a product still has to be installed, the
defaults cannot be resolved yet, and the plan says `resolved after install`.

## What the file must not carry

| Not allowed | Why |
|---|---|
| `credentialRef` | a credential reference belongs to one machine; apply assigns it |
| `defaultContext` | the selection belongs to one machine; use `--use` |
| a client secret, a password, a token | the file is meant to be committed; name an environment variable instead |
| `accounts` (a schema 2 or 3 document) | refused, never migrated: export a fresh file from a machine that has already migrated |
| any other unknown member | refused, so a typo never becomes a silent default |

## When it is refused

Apply refuses the whole file and writes nothing. The message names the cause:

| Message says | Cause |
|---|---|
| `declares no contexts` | `contexts` is empty or absent |
| `declares the context "x" more than once` | two contexts share a name |
| `contains more than one JSON document` | two objects in one file |
| `declares schema version 3, and this shell reads 4` | wrong `schemaVersion` |
| `names a credentialRef, which belongs to one machine's secure store` | a credential reference in the file |
| `selects a context (defaultContext), and a shared file never does` | a selection in the file |
| `logs the context "x" in through the "y" product, which it does not list under products` | `login.product` names a namespace with no `products` entry |
| `gives the context "x" neither a login product nor an issuer and client id` | no way to log in |
| `json: unknown field "logn"` | a misspelled or unsupported member |

With `--no-install`, a login product that is not installed must also state
`login.issuer` and `login.clientId`, because no descriptor can fill them in.

## Share one

`wso2 context export [<name>]` prints your contexts in the input-file form,
with the credential references and the selection removed, ready to commit:

```sh
wso2 context export > team-context.json
```

Export writes complete records, so the file is longer than an input file, and
`wso2 context apply -f <file> --no-install` writes it unchanged on a machine
that has none of the products installed. Export does not write `version`
pins; add those by hand if your team wants them.

## Signing in without a browser

`"kind": "oauth-device"` (or `wso2 context create --device`) is for a context
that can *only* be established without a browser — a deployment whose
loopback callback URLs cannot be registered, or one whose users are never at a
machine that can reach one. It is a property of the context, not of where you
happen to be sitting today:

```json
{
  "contexts": [
    {
      "name": "remote",
      "login": {
        "kind": "oauth-device",
        "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
        "clientId": "wso2-cli",
        "tenant": "acme",
        "product": "reference"
      },
      "products": {
        "reference": { "url": "https://reference.example.test" }
      }
    }
  ]
}
```

Every other field means exactly what it means for `oauth-browser`. Apply
writes the `credentialRef` the sessions are stored under, as it does for
`oauth-browser`. Thunder-backed products advertise no
device grant and refuse this kind outright.

## CI: authenticate without a login

A CI job has no browser and no secure store, so it uses a machine-to-machine
context that carries its own credential and exchanges it inline, on every
command — there is **no login step**. A job that runs `wso2 login` against
such a context is refused with `auth.login_not_required`.

```json
{
  "contexts": [
    {
      "name": "ci",
      "type": "onprem",
      "login": {
        "kind": "client-credentials",
        "issuer": "https://id.example.com",
        "clientId": "ci-runner",
        "clientSecretVariable": "WSO2_CLIENT_SECRET"
      },
      "products": {
        "identity": { "url": "https://id.example.com" }
      }
    }
  ]
}
```

`clientSecretVariable` **replaces** `credentialRef` on a `client-credentials`
context; the two must not appear together. The variable name is upper-case
letters, digits and underscores, starting with a letter. The secret itself
never goes in this file, so the file is safe to commit — the CI system's own
secret store injects the value into that variable at run time:

```yaml
env:
  WSO2_CLIENT_SECRET: ${{ secrets.WSO2_CLIENT_SECRET }}
steps:
  - run: wso2 context apply -f ci/context.json --use ci --no-input
  - run: wso2 identity status
```

The shell reads the variable into process memory for the length of one grant,
performs the token exchange itself, and hands the module only the resulting
short-lived access token — the secret never reaches the module, the
filesystem, or the OS secure store. Set `--no-input` or `WSO2_NO_INPUT=1` on
any job where a stray `wso2 login` should fail loudly rather than wait on a
browser that will never open; see [non-interactive
use](commands.md#non-interactive-use).

A product accepts a machine identity only when its descriptor says how one
reaches it (`capabilities.product.machine` in the [module
manifest](module-manifest.md#capabilitiesproduct)): `inline` reuses the
context's own client, minted per product, and `clientIdVariable` /
`clientSecretVariable` on the product entry name a credential of the
product's own when it does not.

A product reached by `jwt-bearer` instead of directly presents an identity
token from the CI session at its own issuer:

```json
"integration": {
  "url": "https://integration.acme.example",
  "grant": { "kind": "jwt-bearer" }
}
```
