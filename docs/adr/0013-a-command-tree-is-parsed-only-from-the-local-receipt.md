# ADR 0013: A Command Tree Is Parsed Only from the Local Receipt

**Status:** Accepted

A product module declares its command tree — its commands, their flags, and
which of those flags carry a value — and the shell reads that declaration
before it parses a user's command line. That is what lets `wso2 <namespace>
status --since 1h --output json` mean the same thing as `wso2 <namespace>
status --output json --since 1h`. Without a declaration the shell could only
consume the flags it recognised and hand everything from the first flag it did
not to the module, so the second spelling rendered JSON and the first silently
rendered a table.

The declaration therefore decides how the shell interprets what a user typed,
and that makes where it comes from a security question rather than a
convenience one. **The shell parses only from the module receipt, which is
local, and which is read out of the installed executable at install time.**
The catalog carries a copy of the tree so that a command belonging to a module
nobody has installed can still be recognised and suggested, and that copy is
never parsed from. The catalog is fetched over the network and carries no
signature, no publisher, and no provenance field; letting it reach a parser
would let whoever served it change the meaning of a command already on screen,
and would make catalog signing a prerequisite for parsing product flags at
all.

The receipt's copy earns its position by being read from the binary rather
than copied from the entry that pointed at it. Installation runs the
just-unpacked executable, which reports its own tree, and the receipt records
what that executable said. The receipt is then pinned to that executable by a
SHA-256 digest the shell recomputes before every launch, so the tree the shell
parses with is the tree of the code about to run. This is the one receipt field
not taken from what the catalog published, and the exception is the point:
every other field describes what was promised, and this one has to describe
what is actually there.

The split is enforced rather than documented. `internal/parsetree.Tree` keeps
its declaration in an unexported field and `FromReceipt` is its only
constructor, so a tree obtained anywhere else cannot be handed to anything that
parses — the call does not compile. `internal/boundaries` states both halves as
tests: that the parse package's whole dependency closure excludes the catalog
package, and that the single constructor still takes a receipt.

## Considered Options

- **Copying the tree from the catalog into the receipt at install**, the way
  `capabilities` already is, needs no new pipeline step and no execution at
  install time. But the receipt's copy would then only be the catalog's copy
  cached locally: pinned against later tampering, never verified against the
  module. The shell would be parsing from an unsigned remote file one hop
  removed, and the guarantee this ADR states would be a description of a
  hop rather than of a source. Rejected.
- **Reading the tree from the module at launch**, over the protocol handshake
  the module already performs, would need no receipt field at all. But the
  shell has to parse the command line before it knows what to launch, help and
  completion would each require starting a process, and a module that needs a
  context or a session could not answer for its own flags. Rejected.
- **Signing the catalog** and parsing from it directly would collapse the two
  copies into one. It is a larger piece of work with its own key handling and
  revocation questions, and this design deliberately does not depend on it: the
  split above is what keeps catalog signing off the critical path for parsing
  product flags. Not rejected, but not a prerequisite.
- **Refusing an install whose executable declares a different namespace** than
  the entry it was published under was considered and narrowed. The tree from
  such an executable is discarded, so nothing is parsed against a module that
  is not there, but the install proceeds: dropping the tree is exactly what the
  property needs, and the launch gate — receipt, digest, and the capabilities
  the broker intersects — is unchanged either way.

## Consequences

Installation now executes the module once, before writing the receipt. The
executable's archive digest has already been verified against the catalog entry
at that point, and the process is given the null device for all three streams,
a ten-second ceiling, and a bounded wait, so a module that ignores the request,
fails, hangs, or answers unreadably leaves the module installed with no
declared tree.

A module with no declared tree is parsed the way every module was before:
leading plain words are the command, and everything from the first unrecognised
flag is the module's to interpret or refuse. That is what a module built
against an older SDK, or one not built on Cobra, gets, and it is why declaring
is optional rather than required.

The receipt schema moved to version 2 and version 1 is refused, because a
version 1 receipt carries no tree and would leave one module parsing by a rule
every other module had stopped using. The recovery is to reinstall. The shell
is pre-release, so no installation this breaks is one anybody was promised
would keep working.
