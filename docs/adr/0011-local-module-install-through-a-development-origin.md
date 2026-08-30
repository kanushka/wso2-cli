# ADR 0011: Installing an Unpublished Module Through a Development Origin

**Status:** Accepted

A product team building a module cannot run it. Scaffolding creates a module
that builds and passes its own test, and the SDK's test kit drives it through
the module contract in process, but neither launches it under `wso2`. The test
kit says so about itself: it is a conforming peer rather than the shell, it
performs no receipt resolution, no integrity check, and no rendering, and a
module that satisfies it is not thereby proven to satisfy the shell. So the
first time a developer sees their own module answer through the shell, they
have already published a tag. This decision closes that gap.

**A local install goes through the real catalog, not around it.** A contributor
command builds the module with the same `release.Build` a release uses,
packages the archive, and runs the real catalog generator over it. A server on
loopback answers the catalog and archive paths for the length of the run, and
the ordinary installer resolves, downloads, verifies, and activates from it.

What the developer gets is the installation a user gets, produced by the same
code. Two things are worth stating precisely, because a reader will otherwise
assume more than is true. The command drives `internal/install` directly with
the origin set on its catalog client, rather than executing the `wso2` binary
with `WSO2_CLI_CATALOG_ORIGIN` in its environment; the installer and everything
below it are the real ones, but the shell's own argument parsing and policy
defaulting are not exercised. And the generated documents and the archive are
held in memory and served from there, rather than written to a directory on
disk. Neither changes what lands in the module store.

**The obvious alternative is a trap.** Writing the store entry directly is far
simpler: build the executable, write `receipt.json` and `active.json`, and the
shell resolves it. The test-only fixture installer already does exactly this and
would have been reusable. What it does not do is what makes the shortcut wrong.
It never calls `receipt.Validate()`, writes no `policy.json`, is not atomic and
has no rollback, and omits the `.exe` suffix that a real installation applies on
Windows. A module installed that way passes weaker checks than a published one,
which inverts the purpose: the local install exists so a developer meets a
problem before their users do, and a path that skips the checks hides exactly
the problems worth meeting early. Every infidelity would also have to be found
and fixed again each time the real installer changed, by someone who had no
reason to look.

**Going through the catalog makes the rest of the lifecycle real for free.**
`wso2 module list`, `update`, and `remove` operate on a locally installed module
correctly, because there is nothing local about it beyond where its catalog was
read from. Under a direct store write each of those would have been a case to
reason about separately, and `update` in particular would have been a hazard: a
module installed with no version policy is unpinned and eligible, so a published
release would silently replace a developer's own build. The real installer
records a policy, so the pin that prevents this is written by the code that
already knows to write it.

**The cost is a running HTTP server, and it is bounded.** The catalog client
uses the default HTTP transport, so a `file://` origin does not work and
something must serve the directory. The server runs only for the length of the
install. Once the module is in the managed module store the shell resolves it
locally and makes no catalog request, so the developer sets no environment
variable for ordinary use and the iteration loop is a rebuild followed by an
ordinary command.

This also settles a question the code did not. `internal/modules/fixture`
documents itself as unreachable from any shell command because "the architecture
proof has no public unverified local-install command," while the test enforcing
that computes reachability from `./cmd/wso2` alone and would not have noticed a
sibling contributor binary importing it. Rather than decide whether the letter
or the intent governs, this design needs no fixture at all, and the sentence
stays true as written.

## Considered Options

A `wso2 module install --from <path>` flag. It is the shortest path to the
feature and keeps one program. It also puts an install path into the shipped
shell that verifies nothing, which is the capability the fixture boundary exists
to keep out of the product, and a flag cannot be un-shipped once users find it.

A contributor command writing the store entry directly, reusing the test-only
fixture installer. Rejected for the infidelities above.

Extending the fixture boundary test to cover every `cmd/` binary. It would have
settled the letter-versus-intent question by force, but it would break the
catalog and release binaries, which import `internal/catalog` for good reason,
and it answers a question this design no longer asks.

## Consequences

The contributor command depends on the catalog generator and the release
tooling's packaging, so a change to either reaches the developer loop. That is
intended: the loop is meant to break when the real path breaks.

A developer's module is installed into their ordinary state root and is
indistinguishable from a published module apart from its version. `wso2 module
remove` is how it comes off, which is the same answer a user gets.
