# Context file

**Status:** Proposed reference
**Related:** [Logging in](../guides/login.md),
[shell commands](commands.md),
[authentication context examples](../examples/authentication-contexts.md)
**Last reviewed:** 2026-09-16

A context file is the short, shareable description of one or more contexts. A
platform team writes it once, commits it, and every developer applies it:

```sh
wso2 context apply -f team-context.json --use local
wso2 login
```

It holds URLs, not secrets, and nothing that belongs to one machine. Everything
else — issuer, client id, audience, scopes, grant — comes from the installed
products' descriptors when the file is applied.

## The file

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

That is a complete file. `name`, one way to log in, and a `url` per product are
all that is required.

## Two ways to log in

Every context has to say how it logs in. Pick one:

**Through a product** — the usual way, and what the file above does:

```json
"login": { "product": "identity" }
```

The shell takes the issuer and the client id from that product's descriptor, so
the namespace you name must also be a key under `products`. It is the one whose
`url` the issuer is derived from.

**Against a bare issuer** — for a context with no login product:

```json
"login": { "issuer": "https://id.example.com", "clientId": "wso2-cli" }
```

Nothing is derived here, so you state both members yourself.

You may also state `issuer` or `clientId` next to `login.product`. What you
state wins over what the descriptor would fill in.

## What each member means

| Member | Required | Meaning |
|---|---|---|
| `contexts[].name` | yes | the context's name on every machine that applies the file |
| `login.product` | one way to log in | the namespace the context logs in through, also a key under `products` |
| `login.issuer` + `login.clientId` | the other way | log in against a bare issuer, with no login product |
| `login.kind` | no | `oauth-browser` by default, or `client-credentials` when `clientSecretVariable` is set. `oauth-device` and `pat` are accepted too |
| `login.clientSecretVariable` | no | the **name** of an environment variable holding the secret, never the secret |
| `login.tenant` | no | derived from an Asgardeo issuer when absent |
| `login.provider` | no | taken from the login product's descriptor; a bare-issuer context has none unless you state it |
| `products.<namespace>.url` | yes | where that product runs |
| `products.<namespace>.gateway.url` | no | the product's gateway, when it has one |
| `products.<namespace>.version` | no | the module version to install; it is not stored in the context |
| `type`, `organization`, `project` | no | `type` is derived from the issuer when absent |
| `schemaVersion` | no | must be `4` when present |

This table lists what a team normally writes. A product may also state
`audience`, `scopes`, `grant`, `clientIdVariable` and `clientSecretVariable`,
and a login may state `narrowing`. State them only when the deployment differs
from what the installed product declares; otherwise let apply fill them in.

## What apply does

1. Reads the whole file and refuses it as a whole before it installs anything.
2. Installs any product a context names and this machine does not have.
   `--no-install` installs nothing, and then every product must be installed
   already or stated in full. `--update-products` installs a pinned version
   that does not match the one installed.
3. Fills in the frozen defaults from each installed product's descriptor:
   issuer, client id, provider, audience, scopes, grant, gateway audience and
   gateway scopes. `type` and `login.tenant` come from the issuer instead. A
   fault only a descriptor can reveal — a product that declares no grant, or no
   way for a machine context to reach it — is raised here, still before
   anything is written.
4. Ends the sessions that no longer match.
5. Writes **complete** context records to `contexts.json`, selecting the one
   named by `--use`. Nothing else changes the selection.

A context in the file replaces the context of the same name **whole**, keeping
its credential reference; a client-credentials context holds none. Contexts the
file does not name are left alone. A session ends when its binding changed —
its issuer, client, strategy, scopes or resource — so a new URL ends a session
when it moves the issuer or the resource the session was bound to.

Because the records are complete, a later `wso2 product update` changes no
login until the file is applied again. `wso2 doctor` says when a record differs
from what the installed product would write now.

## Review before you write

```sh
wso2 context apply -f team-context.json --dry-run
```

`--dry-run` prints what would be installed, created or replaced, a
field-by-field diff for each replaced context, and the sessions that would end.
It writes nothing. When a product still has to be installed, the defaults
cannot be resolved yet, and the plan says `resolved after install`.

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

`wso2 context export [<name>]` prints your contexts in exactly this form, with
nothing machine-specific, ready to commit:

```sh
wso2 context export > team-context.json
```

Export writes the complete records, so the file is longer than the one above,
and `wso2 context apply -f <file> --no-install` writes it unchanged on a
machine that has none of the products installed. Export does not write
`version` pins; add those by hand if your team wants them.

## A machine context

For CI, log in with a client and a secret the job holds in an environment
variable. The file names the variable; the value never appears in it. A product
accepts such a context only when its descriptor says how a machine reaches it,
so check the product before you write one:

```json
{
  "contexts": [
    {
      "name": "ci",
      "login": {
        "kind": "client-credentials",
        "issuer": "https://id.example.com",
        "clientId": "ci-runner",
        "clientSecretVariable": "WSO2_CLIENT_SECRET"
      },
      "products": { "identity": { "url": "https://id.example.com" } }
    }
  ]
}
```

Such a context stores no session: each command reads the variable and gets its
own token.
