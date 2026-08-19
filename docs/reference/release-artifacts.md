# Release Artifacts

**Status:** Accepted
**Related:** [Distribution research](../research/root-cli-installation-distribution.md),
[module catalog](module-catalog.md), [architecture](../architecture.md)
**Last reviewed:** 2026-08-20

This document is the naming contract between a published release and the
programs that download from it. An install script derives every URL it needs
from a resolved tag and this convention alone: no manifest, no index, and no API
call beyond resolving the tag. Changing anything here breaks installers that are
already in users' hands, so it changes only with a deliberate migration.

The configuration that implements this is `.goreleaser.yaml`, and the workflow
that publishes it is `.github/workflows/release.yml`.

## Where artifacts are published

GitHub Releases on `wso2/wso2-cli`. A pushed tag matching `v*` publishes one
release named for that tag.

This is the interim distribution channel. The signed, per-platform channels in
[the distribution research](../research/root-cli-installation-distribution.md)
remain the destination, and the archives described here are the inputs those
channels package.

## Archive names

```text
wso2-cli-<tag>-<os>-<arch>.<extension>
```

| Component     | Values                                          |
| ------------- | ----------------------------------------------- |
| `<tag>`       | The Git tag verbatim, including its leading `v`  |
| `<os>`        | `linux`, `darwin`, `windows`                     |
| `<arch>`      | `amd64`, `arm64`, `arm`, `386`                   |
| `<extension>` | `tar.gz` for Linux, `zip` for macOS and Windows  |

The tag appears verbatim so that a script which resolved a tag can build the
name without transforming it.

The operating system and architecture tokens are not what platform detection
reports, and both have to be normalized before a name is built.

On macOS and Linux, `uname -s` answers `Linux` and `Darwin`, which lower-case to
the tokens above, and `uname -m` answers a wider set, so `x86_64` maps to
`amd64`, `aarch64` to `arm64`, `armv6l` and `armv7l` to `arm`, and `i686` to
`386`.

Windows does not go through `uname` at all. The operating system token is fixed:
anything installing from Windows is on `windows`, so nothing has to be detected
to choose it. The architecture comes from the environment instead, reading
`PROCESSOR_ARCHITEW6432` before `PROCESSOR_ARCHITECTURE` — a 32-bit process on
a 64-bit machine reports `x86` in the second and the real architecture only in
the first — and mapping `AMD64` to `amd64` and `ARM64` to `arm64`.

Normalizing is not enough on its own: the result has to name a target the
release actually carries, and the table below is not the product of every
operating system with every architecture. There is no `windows` `386` archive
and no `darwin` `386` or `arm` archive, so an installer that normalized its way
to one of those has found a platform with nothing to install rather than a name
to fetch. That is a refusal, and it belongs before the download rather than as a
confusing 404 from it. The same holds for a machine reporting an architecture
absent from the list entirely.

The `arm` archive is built for ARMv6, which the more common ARMv7 hardware also
runs. There is one `arm` archive rather than one per variant, so a script that
detected `arm` has a single name to build.

## Supported targets

A release carries exactly these eight archives:

| Operating system | Architectures                  |
| ---------------- | ------------------------------ |
| Linux            | `amd64`, `arm64`, `arm`, `386`  |
| macOS            | `amd64`, `arm64`               |
| Windows          | `amd64`, `arm64`               |

The pull-request cross-build check compiles this same list. The two are kept
equal deliberately: a target that could be released without being compiled on
every pull request would break in a tag rather than in the change that broke it.

## Archive contents

Each archive contains, at its root and in no subdirectory:

- `wso2` — the shell binary, named `wso2.exe` on Windows
- `LICENSE`
- `NOTICE`

An installer extracts the archive and moves one known path. The license and
notice travel with the binary because Apache-2.0 requires it.

## Checksums

Every release carries `checksums.txt`: one SHA-256 line per archive, in the
format `sha256sum` reads and writes.

```text
https://github.com/wso2/wso2-cli/releases/download/<tag>/checksums.txt
```

The install scripts fetch this file and verify the archive they downloaded
before extracting it. Verification failure is fatal: nothing is extracted and
nothing is installed.

Artifacts are not signed or notarized. Code signing belongs to the per-platform
channels, and until then integrity rests on this checksum file and on HTTPS.

## Version reporting

A released binary reports the version it was built as. The release injects the
shell version — the tag with its leading `v` removed, because the version
package prefixes one for display — through the build-time variables in
`internal/version`. It injects no protocol versions: the shell speaks the
window declared in `sdk/protocol`, the current protocol version and its
predecessor, and a release that narrowed it would cut off module releases for
users a protocol generation behind.

The release workflow proves this rather than assuming it. It downloads the
published assets back from the release page, checks that the published checksum
file is the one that was built and that it lists every archive beside it, then
extracts the Linux archive and runs `wso2 version`. The release fails if the
binary reports the development placeholder, reports a version unrelated to the
tag, or reports a protocol window that disagrees with the one the shell's own
source declares.

## Module releases

A product module is released by pushing a tag in its own namespace, which is a
separate release from the shell's and runs
`.github/workflows/module-release.yml`. What that workflow publishes follows
the same conventions as a shell release, with the module's own names.

```text
wso2-module-<namespace>-v<version>-<os>-<arch>.<extension>
```

The target list is the eight above: a module that published for fewer would
leave a user who can install the shell unable to install the module. The
extension is `tar.gz` on every platform, including the two where a shell
release publishes a zip, because the shell extracts a module archive as a
gzipped tarball and refuses anything else. Each archive carries, at its root and in no subdirectory, the module executable
named `wso2-module-<namespace>` — `.exe` on Windows — beside `LICENSE` and
`NOTICE`. That name is a convention rather than a catalog field: the shell
extracts a module archive expecting exactly it and refuses an archive that does
not carry it rather than searching for something executable. Both halves of
that convention read it from `internal/release`, so it is written down once.

A module release publishes a `checksums.txt` covering every archive, in the
format `sha256sum` reads. It is what the catalog's digests are read back from:
the release API reports no digest, so a release whose checksum file does not
cover an archive publishes nothing that archive could be verified against, and
catalog generation fails rather than publishing an entry with no digest.

## The release gate

Before anything is built or uploaded, the release decides whether the module
can run on a shell that exists at all:

```sh
go run ./cmd/wso2-module-release -tag reference/v4.5.0 -gate-only
```

A module is admitted when at least one module-contract protocol version it
declares is one the released shell speaks. A module requiring a protocol newer
than the released shell speaks is refused, which enforces that the shell ships
first; so is a module speaking only a protocol the window has already left
behind, because no shell in existence could launch it either.

The refusal names both sides — the protocol the module requires and the set the
released shell speaks — so a product team can tell whether to wait for a shell
release or to change the module.

The released shell's supported set is read from `sdk/protocol`, which is the
same single declaration the shell itself reads when it announces what it
speaks, so the gate and the shell cannot come to disagree. The shell release
holds the other end of that equality: it asks the published binary what it
speaks and fails the release when the answer is not what the source declares.

This is not the gate a pull request runs. That one, in
`scripts/previous-protocol.sh`, asks whether a change to the shell or the SDK
broke the older half of the protocol window. This one asks whether a module
being released can run anywhere. They catch different failures and neither
stands in for the other.

The decision is a pure function of what the module declares and what the
released shell supports, so it is proven by tests over fixture data in
`internal/release` rather than by pushing a tag.

## Prereleases

A tag carrying a prerelease identifier, such as `v0.2.0-rc.1`, publishes as a
GitHub prerelease. Resolving "the latest release" skips prereleases, so a
release candidate never becomes the default for users who ask for the newest
version; the install scripts reach it only through their prerelease channel.

## Download URLs

```text
https://github.com/wso2/wso2-cli/releases/download/<tag>/wso2-cli-<tag>-<os>-<arch>.<extension>
https://github.com/wso2/wso2-cli/releases/download/<tag>/checksums.txt
```

The newest stable tag can be resolved without an API token by following the
redirect on `https://github.com/wso2/wso2-cli/releases/latest` and reading the
tag from the resulting URL.

## Reproducing a release without publishing

```sh
make release-snapshot
```

This builds every artifact into `dist/`, including `checksums.txt`, and
publishes nothing. It is how a change to the release configuration is checked
before a tag exists.

A snapshot differs from a real release in two ways worth knowing before reading
its output: the archive names carry the most recent tag in the checkout, which
is `v0.0.0` when there is none, while the binary inside reports the next patch
version suffixed with `-snapshot`. Everything else — the target list, the
archive formats, the archive contents, and the checksum file — is what a tag
produces.

`make release-check` validates the configuration without building.
