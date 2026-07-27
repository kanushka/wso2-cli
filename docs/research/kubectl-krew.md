# kubectl and Krew plugin architecture: lessons for WSO2 CLI

**Status:** Research
**Research date:** 2026-07-24
**Scope:** Official Kubernetes documentation and source, and official
`kubernetes-sigs/krew` documentation/source, reviewed 2026-07-24.

## Keep the two layers distinct

`kubectl` and Krew solve different problems:

- **kubectl core** is only an executable dispatcher. A plugin is an executable
  named `kubectl-<command>` somewhere on `PATH`; kubectl finds and executes it.
  Core does not install, update, checksum, sign, version, or inventory plugins.
  ([Kubernetes plugin guide](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/),
  [dispatcher source](https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/plugin.go))
- **Krew** is a separate package manager, itself invoked as a kubectl plugin.
  It maintains indexes, manifests, downloads, integrity checks, versioned
  installations, receipts, symlinks, upgrades, and removals.
  ([Krew quickstart](https://krew.sigs.k8s.io/docs/user-guide/quickstart/),
  [Krew path model](https://github.com/kubernetes-sigs/krew/blob/master/internal/environment/environment.go))

For WSO2, the analogous split should exist internally even if both capabilities
ship in one `wso2` binary: a small **command dispatcher** and a separately
testable **module manager**.

## kubectl core behavior

### Discovery, naming, and dispatch

- `kubectl foo` resolves `kubectl-foo` using normal `PATH` lookup. Nested words
  map to dashes in the filename, and kubectl searches from the longest candidate
  to the shortest: `kubectl foo bar baz arg` first tries
  `kubectl-foo-bar-baz-arg`, then shorter candidates.
- Dashes in command words are converted to underscores during lookup.
- Once found, remaining arguments and the environment are passed to the child.
  Current source also adds `KUBECTL_PATH=<path-to-kubectl>`.
- On Unix, kubectl replaces its process with the plugin using `exec`; on
  Windows it starts a child process with inherited stdin/stdout/stderr.
  ([dispatcher source](https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/plugin.go))
- Built-in commands win. Core only searches for a plugin when Cobra cannot find
  a command. kubectl has a narrow exception allowing plugins beneath `create`;
  arbitrary extension of existing built-in command trees is not supported.
  ([command construction and precedence](https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/cmd.go),
  [documented limitations](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/#limitations))

### Listing and failure modes

`kubectl plugin list` scans every `PATH` directory. It warns about:

- prefixed files that are not executable;
- duplicate names where an earlier `PATH` entry shadows a later one;
- names that collide with built-ins.

The first matching executable in `PATH` wins, so discovery is affected by user
environment and can be hijacked by path ordering. Flags before the plugin name
are rejected by the dispatcher. A plugin is otherwise arbitrary code and owns
all parsing, validation, help, and exit behavior.
([Kubernetes plugin guide](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/),
[dispatcher source](https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/plugin.go))

## Krew package-manager behavior

### Index and manifest

An index is a Git repository with manifests directly under `plugins/`.
Custom/private indexes are supported and can use any Git remote transport.
([custom-index hosting](https://krew.sigs.k8s.io/docs/developer-guide/custom-indexes/),
[custom-index usage](https://krew.sigs.k8s.io/docs/user-guide/custom-indexes/))

One YAML manifest describes one plugin and must have the same filename and
`metadata.name`. Its significant fields are:

- API version and kind;
- semantic `spec.version` (with `v` prefix);
- homepage, short/long descriptions, and optional caveats;
- one or more platform entries containing an OS/architecture selector, archive
  URI, SHA-256 digest, optional file-copy rules, and executable path (`bin`).

Packages are `.zip` or `.tar.gz` archives. OS and architecture values follow
Go's `GOOS`/`GOARCH`; selectors use Kubernetes label-selector semantics, and
the first matching platform entry is chosen.
([manifest guide](https://krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/),
[manifest types](https://github.com/kubernetes-sigs/krew/blob/master/pkg/index/types.go),
[platform matching](https://github.com/kubernetes-sigs/krew/blob/master/internal/installation/platform.go))

Krew validates the manifest API version, kind, safe name, matching manifest
name, non-empty descriptions/platforms/version, semantic version, 64-character
lowercase SHA-256, `bin`, file operations, and selectors restricted to `os` and
`arch`. The schema contains **no kubectl-version or host-API compatibility
range**.
([manifest validation source](https://github.com/kubernetes-sigs/krew/blob/master/internal/index/validation/validate.go),
[manifest types](https://github.com/kubernetes-sigs/krew/blob/master/pkg/index/types.go))

### Install locations and state

By default Krew uses `$HOME/.krew`, overridable with `KREW_ROOT`:

```text
$KREW_ROOT/
  index/<index-name>/plugins/*.yaml
  receipts/<plugin>.yaml
  store/<plugin>/<version>/...
  bin/kubectl-<plugin> -> ../store/<plugin>/<version>/<entrypoint>
```

`$KREW_ROOT/bin` must be placed on `PATH`. A receipt embeds the installed
manifest, creation timestamp, version, platform data, and source index, which
lets Krew list and upgrade without asking the executable for metadata.
([path source](https://github.com/kubernetes-sigs/krew/blob/master/internal/environment/environment.go),
[receipt types](https://github.com/kubernetes-sigs/krew/blob/master/pkg/index/types.go),
[receipt source](https://github.com/kubernetes-sigs/krew/blob/master/internal/installation/receipt/receipt.go))

### Install, update, upgrade, list, and version

- `krew update` clones/pulls each Git index. It updates metadata only and
  reports newly available plugins and changed versions.
- `krew install NAME` updates indexes by default, reads the local manifest,
  selects the platform, downloads to a temporary staging directory, verifies
  SHA-256, extracts/copies into `store/<name>/<version>`, links the executable,
  and writes the receipt. Existing plugins are skipped.
- `krew upgrade` updates indexes by default, compares installed and indexed
  semantic versions, installs only a strictly newer version, switches the
  symlink, updates the receipt, then removes the old version. All installed
  plugins or named plugins can be upgraded.
- `krew list` reads receipts and prints plugin plus installed version on a TTY;
  when piped, it prints names only so the result can be backed up and fed to
  `install`.
- `krew outdated` compares receipt versions with the local index and prints
  installed/available versions.
- `krew version` reports Krew's own build tag/commit, index URI, paths, and
  detected platform; it does not query every plugin binary.

([install guide](https://krew.sigs.k8s.io/docs/user-guide/installing-plugins/),
[install source](https://github.com/kubernetes-sigs/krew/blob/master/internal/installation/install.go),
[upgrade guide](https://krew.sigs.k8s.io/docs/user-guide/upgrading-plugins/),
[upgrade source](https://github.com/kubernetes-sigs/krew/blob/master/internal/installation/upgrade.go),
[list source](https://github.com/kubernetes-sigs/krew/blob/master/cmd/krew/cmd/list.go),
[outdated source](https://github.com/kubernetes-sigs/krew/blob/master/cmd/krew/cmd/outdated.go),
[version source](https://github.com/kubernetes-sigs/krew/blob/master/cmd/krew/cmd/version.go))

Important failure characteristics:

- no matching platform, invalid manifest, failed download/checksum/extraction,
  missing entrypoint, or symlink failure aborts installation;
- multi-plugin install continues after individual failures and returns an
  aggregate failure;
- upgrade-all skips individual failures and detached local-manifest installs;
- staging reduces partial state, and the new version is installed before old
  cleanup, but link replacement and receipt writes are separate operations—not
  a general transactional package database.

### Integrity, signatures, and trust

Krew verifies the archive's SHA-256 against the digest stored in the index.
That detects corruption or replacement relative to the manifest.
([verifier source](https://github.com/kubernetes-sigs/krew/blob/master/internal/download/verifier.go))

Krew does **not** require an artifact signature, publisher identity, provenance,
or transparency-log proof. Kubernetes explicitly warns that Krew-index plugins
are not security-audited and execute arbitrary local code; Krew prints the same
warning after default-index installation.
([Kubernetes caution](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/),
[Krew security notice source](https://github.com/kubernetes-sigs/krew/blob/master/cmd/krew/cmd/internal/security_notice.go))

Index contribution adds human/CI governance—open-source source, license,
semantic release tag, local install testing, and a PR—but it is curation, not a
cryptographic publisher guarantee.
([submission checklist](https://krew.sigs.k8s.io/docs/developer-guide/release/new-plugin/),
[release-update process](https://krew.sigs.k8s.io/docs/developer-guide/release/updating-plugins/))

### Offline and private use

- Normal update/install/upgrade requires access to Git indexes and archive
  URIs.
- `--no-update-index` permits use of an already-local index, but an uncached
  archive still needs its URI.
- Developer installation supports a local manifest plus local archive
  (`--manifest ... --archive ...`), which is the basic air-gapped primitive.
- Private Git indexes and authenticated Git transports are supported; HTTP
  proxies are configurable. These are building blocks, not a complete
  air-gapped mirroring workflow.
([local testing/install](https://krew.sigs.k8s.io/docs/developer-guide/testing-locally/),
[custom indexes](https://krew.sigs.k8s.io/docs/user-guide/custom-indexes/),
[advanced configuration](https://krew.sigs.k8s.io/docs/user-guide/advanced-configuration/),
[install flags/source](https://github.com/kubernetes-sigs/krew/blob/master/cmd/krew/cmd/install.go))

## Actionable design lessons for `wso2`

### Adopt

1. **Out-of-process executables.** Product teams release independently and can
   use Go/Cobra without Go plugin ABI coupling or rebuilding the host.
2. **A curated central index plus immutable, versioned artifacts.** Product
   repositories own releases; the CLI team owns the index contract and merge
   gate.
3. **Versioned store + receipt + active pointer.** Use a WSO2-owned location
   such as `~/.wso2/cli/modules/<name>/<version>`, record the complete installed
   manifest, and switch the active version only after verification.
4. **Separate metadata refresh from binary change.** The command contract
   should distinguish refreshing catalog metadata from changing an installed
   binary. Final command names remain an open product decision.
5. **Platform-specific manifest entries and staged installs.** Resolve
   `GOOS/GOARCH`, download to a temporary directory, verify, safely extract,
   validate the entrypoint, then activate.
6. **Receipt-backed version output.** `wso2 version` should report host build
   and module-protocol version; `wso2 module list` should show installed and
   available module versions without executing untrusted modules.

### Improve beyond kubectl/Krew

1. **Do not search arbitrary `PATH` first.** Resolve managed modules from the
   receipt/store so an earlier `wso2-foo` cannot silently shadow an approved
   module. An explicit developer override may be supported but must be visible
   in `module list/doctor`.
2. **Dispatch one top-level product namespace per module** (`wso2 ap ...`,
   `wso2 asgardeo ...`). Avoid kubectl's longest-filename search across every
   argument: it can mistake positional arguments for nested plugin names and
   makes ownership/conflicts harder to reason about.
3. **Built-ins always win and reserved names are rejected at index validation.**
   Define ownership of every top-level namespace.
4. **Add explicit compatibility fields.** Krew lacks these. A WSO2 manifest
   should declare at least module protocol version and supported host CLI
   semantic-version range. Reject incompatible install/upgrade before download
   or activation.
5. **Require publisher verification, not checksums alone.** Verify a signed
   artifact/provenance statement against an allowlisted WSO2 team identity,
   then verify digest. Gate index PRs with schema validation, source/release
   ownership, malware/vulnerability scanning, SBOM/provenance checks, and a
   cross-platform contract smoke test. Clearly distinguish `verified`,
   `unverified/local`, and `tampered` states.
6. **Make activation and rollback explicit.** Preserve the previous version
   until the new receipt and active pointer are durably switched; if smoke
   validation or activation fails, retain the old working module.
7. **Design air-gapped use as a supported workflow.** Allow an organization to
   mirror the index and artifacts, export/import a signed module bundle, disable
   network refresh, and install strictly from local media while retaining the
   same verification rules.

### Suggested version surface

```text
$ wso2 version
WSO2 CLI       v0.1.0
Protocol       v1
Commit         abc123
Platform       darwin/arm64

$ wso2 module list
NAME       INSTALLED  AVAILABLE  HOST COMPATIBLE  VERIFICATION  SOURCE
ap         v0.8.1     v0.9.0     yes              verified      official
asgardeo   v1.4.0     v1.4.0     yes              verified      official
```

Treat the three versions as different contracts:

- **host version**: the `wso2` binary release;
- **module protocol version**: the process-level contract between host and
  modules;
- **product module version**: independently released by its owning team.

This avoids implying that all product modules share the host release cadence.
