# Module catalog

**Status:** Accepted
**Related:** [Release artifacts](release-artifacts.md),
[architecture](../architecture.md)
**Last reviewed:** 2026-08-20

This document is the contract between the tags a product module is released
under and the two files a shell reads to discover, select, and verify a module
version. Both files are generated from the tags that exist. There is no
curation step and no reviewed metadata, so nothing can be forgotten and the
catalog cannot disagree with what shipped.

The generator is `internal/catalog` and the command that runs it is
`cmd/wso2-catalog`.

## Module tags

```text
<namespace>/v<version>
```

A product module's tags are prefixed by the namespace it owns, which separates
them from the shell's plain `v*` tags and from the SDK's `sdk/v*` tags. The
version is otherwise the product's own: a module is free to carry the version
its users already know, and the shell never compares a module's version against
its own. The launch gate is the protocol range intersected with the platform,
and nothing else.

The one constraint is that the version is a semantic version, which is the
constraint the module receipt already imposes on an installed module: a version
the shell could not record could not be installed either. A product scheme
carrying a fourth component, such as `4.2.0.1`, does not satisfy it and has to
be mapped onto three components and a prerelease or patch level before it can
be published. Build metadata is refused rather than dropped, because the
shell's version parse discards it and two tags differing only there would
otherwise collapse into one entry.

A tag naming a namespace no module in this repository declares fails
generation. Publishing an entry for it would advertise an artifact that was
never built.

A module declares the namespace it owns in a `module.json` beside its
`go.mod`:

```json
{
  "schemaVersion": 1,
  "namespace": "reference",
  "compatibility": {
    "shell": ">=0.1.0 <2.0.0",
    "protocolVersions": [2]
  },
  "capabilities": {
    "authAudiences": ["reference-status"],
    "authScopes": ["reference:status:read"]
  }
}
```

The namespace is declared rather than taken from the directory name, so a
module directory and the command word it owns are free to differ.

`compatibility` is what the module claims about the shells that can launch it,
and it is what the [release gate](release-artifacts.md#the-release-gate)
decides over. The claim is not taken on faith: the conformance job builds the
module against the published SDK for the previous protocol and launches it
under the current shell, which is what makes declaring the older half of the
window mean something.

`capabilities` are the access requests the module is permitted to make. The
authentication broker intersects a runtime request with what the installed
receipt records, so a catalog entry that carried none would leave a module
installed from the catalog denied every brokered request it makes. What the
module declares is what the catalog publishes and what the receipt records.

A tag publishes the declaration as it stood at that tag, not as it stands on
the default branch: a module that widened its protocol range last week did not
widen the entry it published last year.

## Where the catalog is published

The same origin that already serves the install scripts, at fixed paths:

| Path                       | Contents                                    |
| -------------------------- | ------------------------------------------- |
| `index.json`               | One entry per namespace, latest per channel |
| `modules/<namespace>.json` | The full version history for one namespace  |

## `index.json`

Every update check reads this file and nothing else. Its size is bounded by
the number of namespaces and channels rather than by release history, so the
cost of an update check does not grow as products accumulate releases.

```json
{
  "schemaVersion": 1,
  "modules": [
    {
      "namespace": "reference",
      "path": "modules/reference.json",
      "channels": [
        { "channel": "prerelease", "version": "4.6.0-rc.1" },
        { "channel": "stable", "version": "4.5.0" }
      ]
    }
  ]
}
```

`path` is where that namespace's history is published. A shell fetches it only
when it must select a specific version, which is why a normal update check
costs one request.

A channel is derived from the version rather than declared: a version carrying
a prerelease identifier is on `prerelease` and every other version is on
`stable`. Nothing can therefore land on a channel by mistake.

## `modules/<namespace>.json`

The full history for one namespace, newest version first.

```json
{
  "schemaVersion": 1,
  "namespace": "reference",
  "versions": [
    {
      "version": "4.5.0",
      "channel": "stable",
      "compatibility": {
        "shell": ">=0.1.0 <2.0.0",
        "protocolVersions": [1]
      },
      "capabilities": {
        "authAudiences": ["reference-status"],
        "authScopes": ["reference:status:read"]
      },
      "artifacts": [
        {
          "os": "linux",
          "arch": "amd64",
          "url": "https://downloads.example.invalid/reference-4.5.0-linux-amd64.tar.gz",
          "size": 5242880,
          "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        }
      ]
    }
  ]
}
```

Ordering the file newest first is a presentation choice and not a selection
one. Among the versions its channel and pin policy permit, a shell selects the
newest whose protocol range intersects what it speaks and which publishes an
artifact for its platform. The numerically newest release is not assumed
usable.

## Installing from the catalog

```sh
wso2 module install reference
wso2 module install reference@4.5.0
wso2 module install reference --channel prerelease
```

The shell reads `index.json` to find the namespace and where its history is
published, reads that history because a specific version has to be selected,
downloads the selected artifact, checks it against the digest the entry
records, extracts it into the managed module store, writes a module receipt,
and activates it. Those are the only two catalog requests an install makes, and
no other operation makes any: a command that uses an already-installed module
reads the local receipt and never asks the catalog anything.

Among the versions the channel or the pin permits, the newest whose protocol
versions intersect what the shell speaks and which publishes an artifact for
the shell's platform is selected. A pin overrides the channel, so a pipeline
can hold an exact version and a prerelease can be installed without putting the
module on that channel. The numerically newest release is not assumed usable,
and a module's version is never compared against the shell's.

The archive is a gzipped tarball carrying the module executable at its root,
named `wso2-module-<namespace>`, with `.exe` on Windows. The name is a
convention rather than a catalog field, so an archive not following it is
refused rather than searched.

Every refusal is distinguishable from every other, because each is a different
thing for a user to do about it:

- `modules.incompatible_protocol`, exit 69: no published version speaks a
  protocol this shell speaks.
- `modules.unsupported_platform`, exit 69: the module publishes no artifact
  for this platform.
- `modules.artifact_digest_mismatch`, exit 69: the download did not match the
  digest the entry publishes.
- `modules.artifact_malformed`, exit 69: the archive is not the shape a module
  archive has.
- `catalog.origin_unreachable`, exit 70: the catalog origin could not be read.
- `catalog.unknown_module`, exit 64: the catalog was read and publishes no
  such module.
- `catalog.unknown_channel`, exit 64: the requested channel is not a release
  channel, so no module will ever publish on it.
- `catalog.empty_channel`, exit 64: the channel exists and this module has
  published nothing on it, or has published nothing at all.

The protocol refusal names the protocol versions on both sides, and the
platform refusal names the platform, so neither reads as a broken download. An
unreachable origin and an unknown module are deliberately not the same problem:
one is an outage and the other is a mistake. The two channel refusals are
separated for the same reason: an unknown channel is a typo, while an empty one
is a release that has not happened yet, and only the second is worth waiting
for. Both name the channels the module does publish on, and the flag that
chooses one.

Any failure leaves no executable and no receipt. The download is checked before
anything is written into the store, extraction happens in a staging directory
inside the store, and the active-version pointer is written last, so a failed
install leaves nothing to clean up.

The origin the shell reads is `https://wso2.github.io/wso2-cli`. It can be
overridden with `WSO2_CLI_CATALOG_ORIGIN`, which exists so the acceptance suite
can drive the shell against a local origin serving a generated catalog.

## Discovering what can be installed

```sh
wso2 module available
```

One request, the index, lists every namespace the catalog publishes with the
latest version on each of its channels. What exists is therefore discoverable
from the shell rather than from this document.

## Update checks, channels, and pins

```sh
wso2 module list
wso2 module update reference
wso2 module update --all
```

`wso2 module list` reports the installed modules and which of them have an
update available. It costs one request whatever is installed, because
`index.json` already carries the latest version per channel and no version
history is fetched: a check selects nothing, and selecting is what a history is
for. Extending a module's release history therefore does not make a check cost
more, which is the property the index exists for.

Channel and pin are recorded per module, in a `policy.json` beside that
module's installations:

```json
{
  "schemaVersion": 1,
  "namespace": "reference",
  "channel": "prerelease",
  "pinnedVersion": ""
}
```

An install records what it was asked for, and an update reads it back. That is
what makes a channel a property of the module rather than of the shell, so a
user takes a prerelease of one product without taking prereleases of all of
them, and what makes a pin survive an update run rather than being a one-off
argument. A pinned module is passed over by `wso2 module update --all` rather
than moved, so updating everything else cannot silently take a module off the
version it is held at. Re-running `wso2 module install` is how a module's
channel or pin is changed, because what is recorded is what the last install
asked for.

An update selects under exactly the rules an install does: among the versions
that module's own channel permits, the newest whose protocol versions intersect
what the shell speaks and which publishes an artifact for the platform. The
newest release on a channel is not assumed usable, so a shell too old for the
newest release updates to the newest it can launch rather than to something it
could not run.

An update that fails partway leaves the previous version active and usable.
Nothing is deactivated until a replacement has been downloaded, verified, and
unpacked, so a failed update can only fail to add: the version that was working
is still the version that runs, and the module's own channel and pin are left
as they were. A run over several modules attempts every one of them, reports
every refusal, and exits on the first, so a partial run neither stops at the
first problem nor reports itself as a success. `modules.not_installed`, exit
64, is naming a module that is not installed, which is a mistake rather than a
run over nothing.

## What the digest proves

`sha256` proves that a downloaded archive is the one this entry describes. It
does not prove that this entry is authentic. Artifacts are unsigned, as
[release artifacts](release-artifacts.md) already records, and integrity rests
on the digest together with HTTPS. Whoever can publish to the catalog origin
controls the update channel; that exposure is mitigated by branch protection
and required review on the workflows that publish, not by cryptography.
Manifest signing is a tracked follow-up, recorded in
[architecture](../architecture.md) section 15.

The manifest carries no `publisher`, `signature`, `provenance`, `sbom`, or
revocation field. With one repository and one CODEOWNERS file, the question
those fields existed to answer is not asked, and carrying empty or fabricated
values would suggest a trust chain that does not exist.

## Generation

```sh
go run ./cmd/wso2-catalog-input -repo . -out releases.json
go run ./cmd/wso2-catalog -input releases.json -out site -repo .
```

The input document names the module tags that exist and, for each one, what
that tag published: the compatibility that build declared and the platform
archives uploaded for it.

```json
{
  "tags": ["reference/v4.5.0"],
  "published": {
    "reference/v4.5.0": {
      "compatibility": {
        "shell": ">=0.1.0 <2.0.0",
        "protocolVersions": [1]
      },
      "artifacts": [
        {
          "platform": { "os": "linux", "arch": "amd64" },
          "url": "https://downloads.example.invalid/reference-4.5.0-linux-amd64.tar.gz",
          "size": 5242880,
          "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        }
      ]
    }
  }
}
```

What each tag published is recorded per tag rather than read from the
checkout, because the checkout describes the version being released and not
the versions already released. The buildable modules are read from the
checkout, because whether a namespace exists is a fact about this repository
now.

Nobody writes that document by hand. `cmd/wso2-catalog-input` assembles it:
the module tags come from git, the compatibility and capabilities each tag
declared come from the module declaration as it stood at that tag, and each
artifact's URL and size are read back from the release that published it, with
its digest read from the checksum file published beside it. A release missing
a supported platform, or an archive the checksum file does not cover, fails
assembly rather than publishing an entry pointing at something unverifiable.

## Publishing the catalog

`scripts/assemble-site.sh` assembles everything the origin serves: the install
and uninstall scripts, the landing page, and the regenerated catalog. Both
workflows that deploy there run it. A deployment replaces the whole
site, so a deployment that assembled only half of it would take the other half
down.

`.github/workflows/module-release.yml` runs on a module tag: it gates, builds
and publishes the module's artifacts, then regenerates and deploys the
catalog. `.github/workflows/pages.yml` runs on a change to the scripts or the
generator on `main` and deploys the same assembled site. Each job holds only
the permissions it needs: the gate and the catalog jobs read repository
contents, the publish job alone writes them, and only the deploying job holds
Pages access.

Whoever can publish to that origin controls the update channel for the shell
and for every module. That exposure already existed for the install scripts,
and serving the catalog there grows its blast radius rather than creating it.
The mitigation is branch protection and required review on these two
workflows. Manifest signing is a tracked follow-up.

Generation is deterministic. Every namespace, channel, version, and artifact
is emitted in a fixed order and the documents are rendered with fixed
formatting, so regenerating over an unchanged tag set produces byte-identical
files and a release with no new tags changes nothing.

Generation fails, rather than publishing an entry, when a tag names no
buildable module, when a tag published no release or no artifact, when a tag
is listed twice, when two modules claim one namespace, or when an artifact
carries no URL, no size, or an unreadable digest.
