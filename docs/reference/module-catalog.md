# Module Catalog

**Status:** Accepted
**Related:** [Release artifacts](release-artifacts.md),
[architecture](../architecture.md)
**Last reviewed:** 2026-08-19

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
  "namespace": "reference"
}
```

The namespace is declared rather than taken from the directory name, so a
module directory and the command word it owns are free to differ.

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

## What the digest proves

`sha256` proves that a downloaded archive is the one this entry describes. It
does not prove that this entry is authentic. Artifacts are unsigned, as
[release artifacts](release-artifacts.md) already records, and integrity rests
on the digest together with HTTPS. Whoever can publish to the catalog origin
controls the update channel; that exposure is mitigated by branch protection
and required review on the workflows that publish, not by cryptography.
Manifest signing is a tracked follow-up.

The manifest carries no `publisher`, `signature`, `provenance`, `sbom`, or
revocation field. With one repository and one CODEOWNERS file, the question
those fields existed to answer is not asked, and carrying empty or fabricated
values would suggest a trust chain that does not exist.

## Generation

```sh
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

Generation is deterministic. Every namespace, channel, version, and artifact
is emitted in a fixed order and the documents are rendered with fixed
formatting, so regenerating over an unchanged tag set produces byte-identical
files and a release with no new tags changes nothing.

Generation fails, rather than publishing an entry, when a tag names no
buildable module, when a tag published no release or no artifact, when a tag
is listed twice, when two modules claim one namespace, or when an artifact
carries no URL, no size, or an unreadable digest.
