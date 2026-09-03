# ADR 0008: Cobra for the Shell Command UI

**Status:** Accepted

The shell routes its own commands with Cobra, and declares the flags common to
all of them once on the root command. Every shell-owned command is a
`cobra.Command`, help is rendered from that tree rather than from a
hand-maintained table, and flag spellings follow POSIX conventions because
`pflag` implements them. The shell links `github.com/spf13/cobra` and
`github.com/spf13/pflag` and nothing else from that family: Cobra's
documentation generator pulls a Markdown renderer and a YAML parser into the
module graph, and neither belongs in a binary whose premise is verified
execution. Man page or Markdown generation, if it is ever wanted, belongs in a
separate developer tool. A boundaries test asserts the linked set, so the
constraint is enforced rather than remembered.

Three properties are load-bearing and are not obvious from the code.

**A product namespace is not a Cobra command.** An argument the shell does not
recognize as a built-in never enters the command tree; it is resolved against
the managed module store as before. This keeps built-in precedence a property of
dispatch order rather than an interaction between Cobra's command lookup and a
command set discovered at runtime, and it keeps `wso2 help` and `wso2 version`
free of any module store read. The cost is that command suggestions cover
built-in commands only.

**Cobra's allowlist for unknown flags must never be used at that boundary.**
`FParseErrWhitelist{UnknownFlags: true}` does not forward an unknown flag. It
discards the flag together with its value, with no error and no diagnostic, so
`wso2 api gateway list --env prod` would reach the module as `[gateway list]`
and the command would run against the wrong environment. Flag parsing is
disabled on the passthrough path instead, and the shell interprets its own flags
there itself. The evidence is recorded in
[shell command framework research](../research/shell-command-framework.md).

**When modules declare their command trees, parsing reads the declaration from
the module receipt and never from the catalog.** The receipt is local, written
at install time, and covered by the integrity check performed before launch. The
catalog is fetched remotely and is not signed. A remote file must not be able to
change how the shell interprets a user's command line, so the catalog's copy of
a command tree is for discovery and suggestions only. This is written down now,
before that work starts, because the wrong wiring would be invisible in review.

## Considered Options

- **Keeping the hand-written dispatcher.** This was the standing recommendation
  and was correct while the shell owned two commands and one flag. It stops
  being correct at the scale the command reference proposes: roughly thirty root
  commands with five flags described as common to all of them, where keeping the
  flags consistent and the help honest depends on every reviewer noticing an
  omission. The failure mode is silent — a flag never wired to a command is
  simply not accepted, and a reader cannot distinguish that from a decision.
- **Requiring an explicit terminator before product arguments**, so that
  `wso2 api gateway list -- --env prod` splits cleanly and the shell could parse
  its own flags with full knowledge. `pflag` records the split point, so this
  works in both directions and needs no protocol change. It was rejected as a
  user-facing tax paid forever on every product command, to buy a
  shell-implementation simplification, and it is a worse experience than the
  CLIs being migrated from.
- **Registering each resolved namespace as a Cobra command**, which would give
  suggestions across product commands immediately. It was deferred rather than
  rejected: without a declared command tree it buys suggestions at the price of
  reading the module store on every invocation, including `wso2 --help`, and it
  moves built-in precedence into Cobra's lookup. It becomes the better option
  once a receipt carries a declared tree, because the receipt read is then
  happening anyway.

## Consequences

Flag semantics did not change when the framework arrived. The built-in command
bodies still parse their own arguments, reached through a shim that re-attaches
the shell flags Cobra parsed, so the existing tests remained a regression suite
for the routing change. That shim is scaffolding and is removed as each command
declares its flags directly.

The output flag has two parsers for as long as flag parsing is disabled on the
passthrough path: `pflag`'s, and the shell's own on that path. A test feeds one
argument list through both and asserts they agree, because two parsers of one
flag drift and the drift would be invisible. The duplication ends with the
declared command tree, not before.

Flag parsing stops at the first argument that is not a flag. A product
namespace and the module's own flags follow that argument, and they must reach
the module verbatim, so the shell's parser has to stop before them. Without
this, `wso2 --context prod api list --env stage` fails on `--env`, which is the
module's flag and none of the shell's business. The cost is that a shell flag
written after a positional argument is not the shell's: on a built-in that is
handled by parsing the parent's flags during command lookup, and on the
namespace path by the hand-written parser.

A shell flag is declared once on the root, but each built-in names which of
them it can act on, and one it cannot act on is refused rather than accepted
and dropped. `version` renders fixed output and the module lifecycle commands
select no context, so today only `login` acts on `--context` and only the
namespace path acts on `--output`. A value silently ignored is worse than a
refusal, because the user believes it took effect. This is why the flags are
declared centrally and honored selectively rather than declared per command:
the refusal is then one rule with one message, and each command that grows the
ability to act on a flag names it instead of re-declaring it.

Help output is generated, so its exact shape is now a function of the tree. The
published wording — the usage line, the command table, and the note that product
commands come from installed modules — is preserved by a template rather than
left to Cobra's default layout, because that wording is a user-facing contract
and two tests assert it.

Shell completion is not delivered by this decision and should not be added until
modules declare their command trees. A completion that knows every built-in and
no product command reads to a user as "that command does not exist" rather than
as missing information, which is worse than offering none.

On Windows the graph also carries `github.com/inconshreveable/mousetrap`, which
Cobra uses to detect a binary started from Explorer. It is build-tagged, so it
is not linked on other platforms.

## Amendment: modules now declare their command trees

Two of the costs recorded above were stated as standing, and are not any more.
[ADR 0013](0013-a-command-tree-is-parsed-only-from-the-local-receipt.md)
records how a module declares its command tree and where the shell is allowed
to read one from.

**Command suggestions no longer cover built-in commands only.** For a module
that declares a tree, the shell names a mistyped product command and offers the
declared command nearest to it. A module that declares none, and a module that
is not installed, are both still outside what the shell can suggest.

**Parsing at the namespace boundary is no longer limited to the flags the shell
recognises.** Where a declaration exists the shell reads the whole line against
it, so a shell flag written after a product flag is the shell's, and a flag the
command does not declare is named rather than forwarded. The passthrough
described above remains exactly as written for a module with no declaration.

Shell completion is still not delivered, and the reasoning above still holds
for it: the declaration reaches only installed modules, so a completion built on
it today would still be silent about every product command a user has not
installed.
