# ADR 0009: Publishing the SDK, and What Its Version Promises

**Status:** Accepted

The public SDK is published as a Go submodule of this repository, by tag, and
starting at `v0.1.0`. Until now it has not been published at all: the reference
module requires `github.com/wso2/wso2-cli/sdk v0.0.0`, a placeholder that only
resolves because `go.work` replaces it with this checkout. That arrangement was
correct while no version had shipped, and it is what stops a product team from
owning a module: a module that needs a workspace to compile is a module that
cannot leave this repository's build graph, and a scaffolded module in a fresh
directory does not build at all. Publishing the SDK is the precondition for the
whole module-developer flow, not a step in it.

**The tag is the publication.** A Go submodule needs no registry and no upload:
pushing `sdk/vX.Y.Z` makes that version fetchable, and the module proxy then
keeps it forever. That immutability is the reason the tag is gated by a workflow
rather than pushed by hand. The gate asks the questions a bad tag would answer
too late: that `sdk/` builds and tests standalone rather than only inside the
workspace, that it imports nothing under `internal/`, and that the version the
tag names is the one this commit is built against. An accidental `internal/`
import is the failure worth spending a workflow on — it would pull the shell
into every module's build graph, and it cannot be withdrawn from the proxy once
tagged.

Whether a protocol version is one a released shell speaks is deliberately not
asked here. The SDK is not launched by anything; a module built against it is,
and the module release gate already refuses a module whose protocol no released
shell speaks, naming both sides. Asking the same question of an SDK tag would
put the decision in two places, and the one that matters is the one standing
between a user and a module they cannot run.

**The version starts below 1.0 deliberately.** Go's compatibility promise
attaches to `v1`, and the SDK's surface is moving under active work: the Cobra
adapter is landing now, and declared command trees will change how a module
describes itself. Publishing `v1.0.0` would make a promise we would break within
weeks, and the escape from that is a `v2` import path — a worse outcome than
starting where breaking changes are permitted. Pre-1.0 says what is true.

**The SDK version is not the compatibility contract.** What decides whether a
shell can launch a module is the protocol version, which is versioned
separately, declared in `module.json`, checked by the release gate, and
negotiated at every invocation. The SDK version says only which Go API a module
compiled against. Two modules on different SDK versions run on the same shell if
they speak a protocol it speaks; a module on the newest SDK is refused if it
does not. Keeping these apart is what allows the SDK's Go surface to move freely
without touching the guarantee users depend on, and it is why a pre-1.0 SDK is
not a pre-1.0 promise to product teams.

## Considered Options

Keeping the workspace placeholder and having the scaffolder emit a `go.work`
alongside every generated module. It works, and it is why the placeholder has
survived this long. It also makes every module a satellite of this checkout:
the generated module compiles against whatever the developer happens to have
locally, two developers can get different results from identical source, and
the version a release was built against is not recorded anywhere. The `go.work`
comment already anticipated this, saying the replacement "disappears the moment
an SDK version is published."

Publishing `v1.0.0` immediately, on the argument that the module contract is a
public boundary and product teams need stability from it. They do, and they get
it from the protocol version, which already carries that weight. Attaching it
to the Go version as well would mean two stability contracts over one boundary,
with the weaker one blocking work on the SDK's Go surface.

Publishing the SDK from a separate repository, which would let it version
without the monorepo's tags. Rejected for the reasons ADR 0006 already gives:
the cross-repository plumbing costs more than it buys, and the SDK is
generated from the same `sdk/proto` the shell reads.

## Consequences

A generated module's `go.mod` names a concrete SDK version, and the scaffolder
writes the version the checkout it runs from would use rather than a literal in
a template. A literal rots one release after it is written, and the symptom
reaches the developer as a release gate refusing a module they did not
misconfigure.

`go.work` keeps its `use ./sdk` entry so the shell and the SDK still build
together from source, but the placeholder replace for `sdk v0.0.0` goes away
once `v0.1.0` exists. Local SDK changes then reach modules through the workspace
rather than through a version, which is the ordinary Go arrangement.

Every SDK release costs a workflow run and a decision about whether the change
is breaking. Pre-1.0 makes that decision cheap to get wrong in the permissive
direction, so the honest cost is recorded here: consumers on `v0.x` must expect
a minor bump to be able to break them, and the quick-start guide has to say so
rather than implying stability the version does not carry.
