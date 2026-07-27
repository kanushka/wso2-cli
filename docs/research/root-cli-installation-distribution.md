# WSO2 root CLI installation and distribution

**Status:** Recommended plan  
**Date:** 2026-07-27  
**Scope:** Publishing, installing, updating, pinning, and distributing the Go
`wso2` root shell

## Recommendation

Publish one immutable shell release containing platform-specific artifacts, a
checksum manifest, signatures, and provenance. Make native package managers the
preferred interactive installation path, while retaining signed standalone
archives as the canonical artifacts used by package managers, CI, containers,
and offline workflows.

The recommended user-facing matrix is:

| Environment | Recommended channel | Canonical artifact |
| --- | --- | --- |
| macOS | Homebrew; signed and notarized `.pkg` as an alternative | `tar.gz` for each architecture |
| Windows | WinGet; signed MSI as an alternative | Authenticode-signed MSI and ZIP |
| Debian/Ubuntu | WSO2 APT repository | signed `.deb` and `tar.gz` |
| RHEL/Fedora/Amazon Linux | WSO2 RPM repository | signed `.rpm` and `tar.gz` |
| Other Linux and CI | versioned standalone archive | `tar.gz` for each architecture |
| Containers | version-pinned official image and/or archive install | digest-pinned image/archive |
| Air-gapped systems | root-only installer or self-installing bundle | signed, self-contained package |

The root shell and product modules remain separate releases. Installing the
root shell should not silently install every product module. Online module
discovery and the offline bundle flows remain the responsibility of the module
manager described in the [architecture](../architecture.md).

## What established CLIs do

### Direct installers and archives

- AWS CLI publishes a macOS `.pkg`, Windows machine-wide and per-user MSI
  installers, and Linux ZIP installers. Its Linux ZIP has a detached PGP
  signature; updates reuse the installer, and Windows updates download a new
  MSI. AWS warns that third-party repositories may not contain the latest
  version
  ([AWS CLI installation](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)).
- Google Cloud CLI supports versioned archives with published SHA-256 values,
  Windows installation, and signed APT packages. Its Linux APT instructions use
  a repository-specific keyring through `signed-by`, and it notes Snap as an
  automatic-update option
  ([Google Cloud CLI installation](https://docs.cloud.google.com/sdk/docs/install-sdk)).
- Firebase offers a Windows standalone binary, macOS/Linux standalone binaries,
  npm installation, and a macOS/Linux automatic installation script. Updating
  follows the original channel: rerun the script, replace the binary, or update
  the npm package
  ([Firebase CLI installation](https://firebase.google.com/docs/cli)).
- Kubernetes publishes standalone `kubectl` binaries for Linux architectures,
  version-specific download URLs, matching SHA-256 files, rootless installation
  instructions, and signed APT packages. Its docs explicitly support selecting
  a fixed version rather than `stable.txt`
  ([kubectl installation](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)).

These examples establish a useful pattern for a Go CLI: build one native binary
per supported OS and architecture, wrap it where the platform benefits from a
native installer, and keep the raw versioned artifacts available.

### Package managers

- Homebrew formulae can install an upstream archive and verify its checksum;
  Homebrew also distributes prebuilt bottles and records installation receipts.
  Formula metadata is published as generated JSON
  ([Homebrew Formula Cookbook](https://docs.brew.sh/Formula-Cookbook)).
- WinGet manifests name the installer URL, architecture and type, and the
  expected SHA-256. Submissions to the public manifest repository are
  automatically validated, including installer-hash matching
  ([WinGet manifests](https://learn.microsoft.com/en-us/windows/package-manager/package/manifest),
  [WinGet submission and validation](https://learn.microsoft.com/en-us/windows/package-manager/package/repository)).
- Signed Linux repositories provide normal operating-system update and pinning
  behavior. Google Cloud and Kubernetes both document APT sources whose keys
  are scoped with `signed-by` rather than placed into a global trust store
  ([Google Cloud CLI installation](https://docs.cloud.google.com/sdk/docs/install-sdk),
  [kubectl installation](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)).

Package-manager metadata should point to WSO2-controlled immutable artifacts.
The package-manager definition is another delivery channel, not the artifact's
root of trust.

### Platform trust

- Apple says directly distributed macOS software should be Developer ID signed
  and notarized. The notary service scans for malicious content and signing
  problems, and a stapled ticket lets Gatekeeper validate the software without
  retrieving the ticket online. Apple supports notarizing installer packages
  and ZIP archives
  ([Apple notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)).
- Microsoft SignTool signs, verifies, and timestamps executables and
  installers; Microsoft recommends SHA-256. Authenticode signing establishes
  publisher authenticity and file integrity
  ([Microsoft SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool),
  [signing a file](https://learn.microsoft.com/en-us/windows/win32/seccrypto/using-signtool-to-sign-a-file)).
- GitHub Releases can attach binary assets to versioned tags. GitHub immutable
  releases lock the tag and release assets and generate a release attestation
  covering the tag, commit, and assets
  ([GitHub releases](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases),
  [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)).

WSO2 should use operating-system trust and its catalog/signature verification
as complementary controls, not substitutes.

## Installation options for WSO2

### Option A: versioned standalone archives

Release `wso2_<version>_<os>_<arch>.tar.gz` or `.zip`, accompanied by a
checksum manifest, detached signature, SBOM, and provenance.

**Advantages**

- Smallest release surface and natural output of a Go cross-build.
- Works for CI, containers, custom install locations, version pinning, and
  offline transfer.
- Provides one canonical artifact set for Homebrew, WinGet, and repository
  packages.

**Trade-offs**

- Users must manage `PATH`, upgrades, and removal unless an installer wraps it.
- Checksums alone prove integrity only when the checksum source is authenticated;
  signatures or signed release metadata are still required.

**Decision:** mandatory in the first release.

### Option B: native installers and package repositories

Ship a notarized macOS `.pkg`, Authenticode-signed Windows MSI, signed DEB/RPM
packages, and WSO2 APT/RPM repositories. Publish Homebrew and WinGet definitions
that reference the same release artifacts.

**Advantages**

- Familiar install, update, uninstall, inventory, and enterprise-management
  behavior.
- Native trust prompts and policy integration on macOS and Windows.
- APT/RPM support managed upgrades and version pinning.

**Trade-offs**

- More signing credentials, packaging pipelines, repository operations, and
  platform-specific testing.
- Package-manager publishing may lag the canonical release.

**Decision:** required for the supported macOS, Windows, and Linux experience.
Implement after the canonical archives exist, but include Homebrew, WinGet/MSI,
APT, and RPM in the initial production scope.

### Option C: `curl | shell` convenience installer

A script can detect OS and architecture, select an artifact, download it, verify
the signed checksum or release metadata, and install it. Firebase demonstrates
the convenience of this channel
([Firebase CLI installation](https://firebase.google.com/docs/cli)).

**Advantages**

- Very low friction on macOS/Linux and useful in ephemeral CI.
- Can share artifact selection and verification logic with other channels.

**Trade-offs**

- Piping remote content directly to a shell prevents ordinary inspection and
  combines download with execution.
- Running the pipeline through `sudo` gives changing network content elevated
  privileges.
- Proxies, redirects, partial downloads, and shell differences complicate safe
  behavior and support.

**Decision:** optional convenience, never the only documented path. Prefer a
two-step form that downloads a versioned installer, verifies it, then executes
it. If a one-line form is offered, use fail-closed HTTP options, do not require
`sudo` for a per-user install, resolve an immutable version before download,
and verify signed metadata before installing.

### Option D: shell-managed self-update

The existing command model assigns `wso2 update` to the root shell, while
modules update independently. A self-updater can use the same signed catalog
and atomic activation rules as module installation.

**Advantages**

- Consistent cross-platform update experience and immediate security updates.
- Useful for archive and offline-style installations without a package manager.

**Trade-offs**

- It can conflict with Homebrew, WinGet, APT, RPM, or MSI ownership.
- Replacing a running executable, escalation, rollback, and Windows locking need
  platform-specific handling.

**Recommendation:** `wso2 update` should detect its installation channel. For
package-managed installations, it should print the appropriate package-manager
upgrade instruction rather than modify managed files. If shell-managed
self-update is approved, it should apply only to installations created by the
WSO2 standalone installer. Automatic background replacement should not be the
default. The final self-update decision remains open.

## Proposed release pipeline

For each semantic version and release channel:

1. Build reproducibly for supported OS/architecture pairs from one tagged
   commit.
2. Run tests and vulnerability/license checks; generate an SBOM and provenance.
3. Package immutable archives.
4. Sign Windows executables/installers with Authenticode and a timestamp.
5. Sign macOS binaries/packages with Developer ID, notarize the distributed
   outer artifact, and staple the ticket.
6. Produce signed DEB/RPM packages and signed repository metadata.
7. Publish all canonical artifacts, checksums, signatures, SBOMs, and
   provenance to an immutable WSO2-controlled release.
8. Publish or update Homebrew, WinGet, APT, and RPM metadata only after canonical
   artifacts are final.
9. Publish the shell release into the WSO2 signed update metadata, separately
   from the product-module catalog.

Signing keys must be held by protected release infrastructure, not stored in
Git repositories or ordinary CI variables.

## Publication and approval model

WSO2 does not need approval from every operating-system vendor before it can
distribute the CLI:

- WSO2 can create and operate its own Homebrew tap immediately. A tap is a Git
  repository maintained by its owner; publishing to `homebrew/core` is a
  separate contribution and acceptance process
  ([Homebrew tap documentation](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap),
  [Homebrew Core requirements](https://docs.brew.sh/Acceptable-Formulae)).
- WSO2 first publishes its signed Windows MSI or other supported installer.
  It then submits a manifest pull request to the WinGet community repository,
  where automated validation and possible manual review check the manifest,
  installer hash, installation behavior, uninstall behavior, and safety
  ([WinGet submission and validation](https://learn.microsoft.com/en-us/windows/package-manager/package/repository)).
- WSO2 can host its own signed APT and RPM repositories without getting the
  packages accepted into Ubuntu, Debian, Fedora, or Red Hat's official
  repositories. Users explicitly add the WSO2 repository and its scoped signing
  key. Acceptance into an operating-system distribution can remain a later,
  optional effort
  ([Ubuntu third-party repository guidance](https://ubuntu.com/desktop/docs/en/latest/how-to/software/add-a-software-repository/),
  [RPM signing](https://rpm.org/docs/6.1.x/man/rpmsign.1)).

Repository ownership does not replace platform signing. WSO2 still needs
protected signing identities for Apple Developer ID/notarization, Windows
Authenticode, APT metadata and packages, and RPM packages and repository
metadata.

## Connected installation flow

One tagged release pipeline builds and signs every platform artifact. The
supported installation channels consume those immutable artifacts:

1. Homebrew installs the macOS archive referenced by the WSO2 tap.
2. WinGet installs the signed Windows installer referenced by its validated
   manifest.
3. APT and RPM install signed Linux packages from WSO2-operated repositories.
4. A thin CDN-hosted Bash installer may detect the platform, resolve an
   immutable version, download the correct signed archive, verify it, and
   perform a per-user installation.
5. Users and CI may download an exact standalone archive directly.

The CDN script is a convenience channel, not a separate artifact or trust
system. Its source is public and auditable, it supports explicit versions, and
it fails closed when signed metadata or artifact verification fails. A
two-step download, inspect, and execute path is documented alongside any
one-line form. A PowerShell equivalent may be provided for Windows, although
WinGet and MSI remain the preferred Windows paths.

## Air-gapped installation and bundle plan

An air-gapped target cannot use the CDN installer because that script downloads
its payload at runtime. Offline installation therefore uses self-contained,
platform-specific packages prepared on a connected machine.

### Root-only offline installation

WSO2 publishes a signed standalone root installer for each supported platform.
The user downloads it on a connected machine, transfers it through an approved
process, and installs the shell without network access. This is sufficient when
product modules will be transferred separately as individual
`.wso2module` files.

### Fresh machine: self-installing offline bundle

WSO2 publishes a platform-specific, self-installing offline bundle containing:

- a platform-specific offline bootstrap installer;
- the exact WSO2 shell release;
- a curated set of compatible official modules;
- a signed catalog snapshot;
- artifact signatures, digests, provenance, and the bundle manifest.

The target machine does not need a preinstalled `wso2` command. After transfer,
the user runs the bundle file directly:

- Windows uses a signed `.msi` or installer executable;
- macOS uses a signed and notarized `.pkg`;
- Linux uses a self-extracting installer such as a `.run` file, accompanied by
  a detached signature and verification instructions.

Windows and macOS verify the installer through their platform trust mechanisms.
Before executing a Linux installer, the administrator verifies its detached
signature with an independently trusted WSO2 public key. The embedded bootstrap
then verifies the remaining package contents, installs the shell, and verifies
and activates the included modules. It performs no network requests and is not
downloaded or generated inside the air-gapped environment.

### Custom bundle

On a connected machine, an installed shell creates a tailored package with a
command such as:

```shell
wso2 bundle create \
  --modules agent,api \
  --platform linux-amd64 \
  --output wso2-offline-linux-amd64.run
```

The command selects compatible releases only from verified catalog metadata and
packages the same signed canonical artifacts used by online installation. The
result is a self-installing package for the selected target platform and
includes its bootstrap installer. The air-gapped machine therefore installs it
by running the transferred package directly and does not need a preinstalled
shell.

### Existing WSO2 CLI installation

If the target already has the shell, `wso2 bundle install <file>` verifies and
imports a transferred bundle to add or update its included modules. This
command is not the fresh-machine installation path.

`wso2 bundle inspect <file>` reports the target platform, shell and module
versions, catalog snapshot, and verification state without installation.
Individual modules use
`wso2 module install --file <module.wso2module>`.

The CLI never auto-discovers or trusts arbitrary executables copied into a
folder. Offline installation is always an explicit import, and it applies the
same identity, signature, digest, compatibility, revocation-metadata, health,
receipt, atomic-activation, and rollback checks as online installation.

## Phased delivery

### Phase 1: portable foundation

- Linux and macOS archives for AMD64 and ARM64; Windows ZIP for AMD64, adding
  ARM64 when product support requires it.
- Signed checksums, provenance, SBOM, immutable release assets, explicit version
  URLs, and documented manual verification.
- Authenticode-sign the Windows executable and Developer ID sign/notarize the
  macOS distribution.
- Provide an offline shell installer and reuse the same artifacts inside
  self-installing offline bundles.
- Support exact-version installation and ensure `wso2 version` works offline.

### Phase 2: mainstream developer channels

- Official Homebrew tap or formula for macOS.
- WinGet manifest backed by the signed Windows installer.
- Signed macOS `.pkg` and Windows per-user/machine-wide MSI.
- WSO2-hosted signed APT and RPM repositories.
- Thin, auditable macOS/Linux download script as a convenience channel.
- Channel-aware `wso2 update`.
- Root-only offline installers, complete official bundles, and custom bundle
  creation, inspection, and installation.

### Phase 3: enterprise distribution

- Version pinning/holds and documented internal mirroring.
- Official minimal container image referenced by immutable digest.
- Enterprise deployment documentation for MSI and native Linux packages.

## Required installation semantics

- A user can choose an exact shell version and release channel; CI examples
  must pin versions or immutable container digests.
- Latest-version aliases may aid interactive installation but must resolve to an
  immutable version before download and verification.
- Installation must verify artifact identity, signature, digest, provenance,
  platform, and version before activation.
- Upgrades must be atomic and retain a previous verified version for rollback
  when the installation channel permits it.
- Offline installation must require no network access, include stapled
  notarization material where relevant, and carry signed metadata with an
  explicit snapshot/expiry policy.
- Uninstall must preserve user configuration and credentials unless the user
  explicitly requests their removal.
- The release channel that installed the shell owns its updates; the shell must
  not overwrite files managed by an operating-system package manager.

## Decisions still required

1. Which OS versions and architectures are supported at general availability?
2. Does WSO2 operate its own Homebrew tap initially, or target Homebrew/core?
3. Is the Windows primary artifact MSI or MSIX, and are both per-user and
   machine-wide installations required?
4. Should WSO2 adopt the channel-aware standalone self-update recommendation,
   or defer root self-update entirely?
5. Where are canonical immutable shell artifacts hosted: GitHub Releases,
   WSO2 artifact storage, or both?
6. What is the offline trust-root rotation and expired-metadata policy?
7. Is an official container image in scope, and which registries and base-image
   policy apply?
