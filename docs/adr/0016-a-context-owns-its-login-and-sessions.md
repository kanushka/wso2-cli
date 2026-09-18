# ADR 0016: A Context Owns Its Login and Its Sessions

**Status:** Accepted

The context document is one list of contexts. A context holds its own login
block, its products, and a credential reference its sessions live under, and no
two contexts share a credential reference. The account, the separate record
several contexts could name, is gone from the file and from the commands, and
`wso2 context` is where every command that shapes the file lives: `create`,
`product add`, `product remove`, `apply`, `edit`, `export`, `rename`, `delete`,
`use`, `list`, `current` and `show`.

Two things had drifted apart. On-premises setup took four commands under three
families — `wso2 <namespace> connect` per product, the `account` family, and
`context` — and `connect` sat under each product's namespace where
`wso2 --help` never listed it. And users read "account" as a person's account
at a provider, which it was not: the file held `accounts[]` and `contexts[]`,
and each context was only a pointer into the account list. A context file is
also the natural unit a platform team shares with every developer on a
deployment, and a shared unit has to be self-contained.

ADR 0015 rejected folding the account under the context because several
contexts could name one account, so the account was part of none of them. This
ADR answers that by giving up the sharing rather than the containment. Sharing
one login between contexts meant sharing its sessions, and a shared session is
where the model broke: two contexts naming one account could change a
product's URL or grant independently, and each would then present the other's
stored session for something it was never authorized for, while ending one
context's sessions ended the other's. With one reference per context, a change
to a context can end exactly the sessions it rebinds, logout ends only the
selected context's sessions, and nothing another context holds is touched.
Several organizations reached through one login stay one context, switched
with `wso2 org use`; two targets that must be selectable by name are two
contexts that each log in, which the provider's sign-on usually answers
without a prompt.

**Two files, two roles.** The input file is short and shareable: it names
contexts and the URLs their products answer at, and leaves out whatever an
installed product's descriptor knows. `wso2 context apply -f` validates the
whole file, plans, installs missing products, resolves the defaults from the
now-installed descriptors, ends the sessions whose bindings change, and writes
**complete** records to `contexts.json` under its lock; nothing is written
until everything before the write has succeeded. The shell then reads only
`contexts.json` at command time and never consults a descriptor to fill a gap,
so a later `wso2 product update` changes no authentication behaviour until the
context is applied again ("frozen defaults"). `wso2 doctor` and
`wso2 context show` report a record whose values differ from what the
installed descriptor would produce, and name the command that adopts them. An
input file never names a credential reference or a selection, because both
belong to one machine; `apply` keeps a replaced context's reference, assigns
one to a new context, and changes the selection only with `--use`.

**A session records its binding.** A stored session now records the resource
and audience it was authorized for beside the issuer, client, scopes and
strategy it already recorded, and is presented only for a record that asks for
exactly that. An entry written before this is reused only for a record that
asks for no resource. Every command that changes a context's records ends the
sessions whose binding no longer matches — best effort at the issuer, per ADR
0010, and certainly in the secure store — before it writes, so no entry is left
that no document names and none is presented for a record it does not match.
Sessions are never copied between references: an issuer that detects refresh
token reuse revokes both copies.

**Migration.** A schema version 2 or 3 document is read, migrated in memory,
and written as version 4 by the first command after an upgrade. Each context
takes its account's login and products, `endpoint` becomes `url`, and the
effective login product is written out. An account's reference, and every
session under it, goes to one of the contexts that named it — the selected one
if it is among them, else the first by name — and the others get references of
their own and log in again; the shell says which. An account no context named
becomes a context of its own so that no configuration is lost. A version 1
document stays read-only, as before. An older shell refuses to overwrite a
version 4 document (`contexts.document_frozen`).

**Redirects.** `wso2 <namespace> connect` and the `wso2 account` family are
removed, and typing one answers with the exact replacement built from the line
typed, in the usage exit class, as ADR 0015's moved identity verbs did. A
redirect is reached only after dispatch has declined the words, so no
namespace is shadowed and a module that declares one of them is reached as
usual.

Consequences: a person with several contexts on one deployment signs in once
per context rather than once for all of them, and after upgrading, contexts
that shared an account's session sign in again once. The internal
authentication view keeps the name `contexts.Account` for now; it is built
from a context and has no presence in the file or the commands. `wso2 login`
and `wso2 context create` stay the one-command paths for a user with no team
file, and neither needs the file to be edited.

## Considered Options

- Keeping accounts and adding `wso2 context account add` beside
  `wso2 context product add` keeps the sharing and moves the words, and keeps
  both problems: the word users misread stays, and so do the shared sessions.
- Inlining the account but letting contexts keep a shared reference when their
  login blocks are equal was rejected: equal login blocks say nothing about
  the products, whose URLs and grants decide what a stored session was
  authorized for.
- Resolving descriptor defaults when a command runs, rather than freezing them
  into the record, keeps the file short and makes a product update silently
  change what a login asks for; a reproducible local configuration is the
  point of the file.
- Discovering a deployment's products from a well-known document served by the
  login product would make a bare URL enough. It depends on other products'
  teams and is tracked in
  [#196](https://github.com/wso2/wso2-cli/issues/196); it
  produces an input file, so it goes through `apply` unchanged.
