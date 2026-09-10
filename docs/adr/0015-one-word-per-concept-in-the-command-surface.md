# ADR 0015: One Word per Concept in the Command Surface

**Status:** Accepted

The shell names four products as top-level namespaces, `identity`, `api`,
`agent` and `integration`, and every word in the command surface means one
thing. Two renames follow from that rule. The shell's `identity` command
becomes `account`, and the domain term moves with it, so an account is one
login provider and one person. The `module` command group becomes `product`,
and `module` stays the word contributors use for the executable they build.

Naming the namespaces after products rather than after deployments is what
makes the identity one workable at all. The identity namespace answers for
ThunderID and for Identity Server, which share no API, so a namespace named
after either would have to be migrated the first time a deployment ran the
other. It also settles nothing that the deployment record does not already
settle: the identity's provider is recorded, and the module reads it.

`identity` could not be taken without moving the shell's own command, because
`isShellCommand` resolves a built-in before the module store is opened, so a
module in a shadowed namespace would build, release, install, and never run.
Renaming the command alone was rejected. The word would then name both the
login record and the product, in the same paragraph and in the same refusal
text, which is the ambiguity the rename exists to remove. The concept moves
too, at a measured cost of 877 mentions across `docs/` and `CONTEXT.md`, 73
`Identity*` Go identifiers, 54 user-visible `wso2 identity ...` strings, and a
schema bump for the `identities` key in `contexts.json`, migrated through the
machinery `internal/contexts/legacy.go` already carries.

`product` beats `platform` for the command group on the same rule. The domain
already says product in five `CONTEXT.md` terms, 1215 mentions across the
documentation, and 427 user-visible strings, against 264 mentions of platform.
Choosing platform for commands while every refusal says product would recreate
the split this ADR closes, and closing it properly would cost a second
migration larger than the first. Users install a product; contributors build a
module; each word keeps one job.

`wso2 module` stays as a deprecated alias, which `wso2 identity` cannot. No
product is named `module`, so the alias collides with nothing and keeps the
name reserved against a namespace claiming it later. The identity command has
no such room, because the word becomes a namespace the shell must dispatch.

An alias is impossible; a redirect is not, and the two were conflated. Every
`wso2 identity <verb>` that shipped keeps being typed after the rename, out of
habit and out of scripts and pages written before it, and both paths it can
take end nowhere: an uninstalled namespace refuses with `shell.unknown_command`
and an installed one refuses with `shell.unknown_product_command`, which reads
as a command that exists nowhere rather than one that moved. So the account
verbs the shell used to own — `create`, `add-product`, `list` — are recognized
by name at both refusals and answered by naming `wso2 account`. That shadows no
namespace and reserves no word: the module still owns `identity`, and a module
command sharing one of those three names is reached exactly as before, because
the redirect is only ever reached after dispatch has already failed.

The identity namespace answers for two products whose command trees are not the
same, and ADR 0013 parses a tree only from the local receipt, so the tree cannot
vary by the deployment a command is pointed at. It is the union, and each
command declares which providers it serves. Help marks the ones a provider does
not answer for, and a command run against a provider it does not serve is
refused by the module before any request is made, naming the provider it
belongs to. The intersection was rejected: ThunderID resource servers have no
Identity Server counterpart, and a namespace that could not manage them would
leave the product's own setup outside the shell.

The help page states what a machine can reach before anything is installed. It
carries two sections, one for shell commands and one for product commands, and
the product section names every product the release knows about, marking those
this machine has not installed. The list is a snapshot of the catalog index
baked in at release, which keeps ADR 0006 intact: the catalog is still a build
output, and the shell only carries a copy of one. It also keeps help offline,
which a help page has to be. `scripts/released-shell-protocols.sh` already
reads the published release rather than the checkout for the same reason.

The product section names only namespaces the release knows a published
version for, so help never advertises a product `wso2 product install` would
then fail to find; a namespace that exists in the monorepo without a release is
absent rather than marked. Each product declares a short title in its
`module.json`, and `cmd/wso2-catalog` carries the title into `index.json`. A
title is rendered into a terminal and, per ADR 0006, nothing attests to the
authenticity of a catalog entry, so it is bounded in length and stripped of
control characters before it is printed. A table of titles held
in the shell was rejected: it would make the shell curate what exists, and a
product released later would need a shell release before its name could be
printed.

`available` and `list` become one `list`. Once help answers what exists and
what is installed, the two commands answer one question between them, and the
answer worth paying a network round trip for is the update column that neither
help nor `wso2 version` can give offline. One table names every known product,
the installed version or none, and the available update or, when the catalog
cannot be reached, that it is unknown.

Dated documents keep the words they were written with. The decision records in
`docs/adr` and the findings in `docs/research` say what was decided and what was
measured on a given day, and a measurement rewritten to use vocabulary that did
not exist when it was taken is no longer a record of anything. This ADR is where
the vocabulary changed, so it is the one place a reader needs in order to read
the earlier ones. Everything a reader is meant to act on today — the guides, the
reference, the examples, and `CONTEXT.md` — moves.

Consequences: `wso2 product list` is the only thing that reports an available
update, so a machine that never runs it never learns of one. Reusing the
catalog answer that `list`, `install` and `update` already fetch, and stating
its age in `wso2 version`, would close that without a new network call;
checking on ordinary commands would not, because it makes the shell reach the
network when nobody asked it to. Both renames break commands that shipped, and
shipping them in one release costs users one migration rather than two.

## Considered Options

- Naming the identity namespace `iam` keeps the shell's `identity` command and
  costs no migration at all. It was rejected for consistency: `api`, `agent`
  and `integration` name their products plainly, and one abbreviation among
  them is the odd word a reader has to learn.
- Naming it `thunder` or `is` names a deployment rather than a product, and
  the namespace would have to move the first time a user ran the other one.
- Leaving the moved account verbs to refuse in the ordinary way keeps the
  dispatch path free of special cases. It was rejected because the ordinary
  refusal is wrong here rather than merely unhelpful: it tells a user the
  command does not exist, at the one moment the shell knows both that it did
  and where it went.
- Folding the account command under `context` would free the word without
  inventing a synonym. It states a containment the model does not have:
  several contexts may name one account, so the account is not part of any of
  them.
- Listing only installed products in help, with a line pointing at the
  catalog, keeps every word of ADR 0006 and tells a new user nothing. The
  moment discovery matters most is the first run, which is exactly when
  nothing is installed and nothing is cached.
- Rendering the help list from a cached catalog index, refreshed whenever a
  command reaches the network, is self-updating and empty on a fresh machine,
  which is the same failure as the option above arriving later.

## Amendment: the shell commands are split around the products

The help page carries three sections rather than two. The shell commands a user
starts with — `account`, `context`, `login`, `logout`, `org`, `product` and
`whoami` — come first, the product section follows, and every other shell
command comes last under its own heading. A product is what a user came for,
and listing it after `config`, `doctor` and `version` buried it below commands
most users never run. A shell command not named as core is listed under the last
heading, so adding one cannot drop it from the page.

The release's copy of the catalog is injected into the binary at build time
rather than committed, because a committed copy would either be edited by the
release job, which leaves the tree dirty for the release build, or go stale
between releases. A development build carries none. The product section also
names every installed product the copy does not know, such as one installed
from a development origin, by the summary its declared command tree gives, so
the page never omits a namespace the shell would dispatch. A product the copy
knows only as a prerelease is left out, because install selects the stable
channel. When the installed products cannot be read, the copy is listed
unmarked and the page says it could not tell.
