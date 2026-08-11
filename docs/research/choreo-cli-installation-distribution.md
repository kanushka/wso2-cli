# Choreo CLI installation and distribution mechanics

**Status:** Research
**Research date:** 2026-08-10
**Scope:** Primary-source findings on how the WSO2 Choreo CLI (`choreo`) is
distributed and installed by end users, including install-script internals,
release artifact conventions, Windows support, and security-relevant
observations
**Source policy:** Public repositories, release metadata, and public
documentation only

## Summary

Choreo CLI install-script specifics were directly verifiable from primary
sources: the public [`wso2/choreo-cli`](https://github.com/wso2/choreo-cli)
repository's README links a working `curl | bash` installer and a PowerShell
installer, and both scripts were fetched and read in full. No substitution
was required. A second, near-identical CLI, [`wso2/wdp-cli`](https://github.com/wso2/wdp-cli)
("WSO2 Developer Platform CLI"), was found during this research and is
reported alongside Choreo CLI because its install scripts are line-for-line
structural copies and its release stream shares commit history with
`choreo-cli` (see "Relationship to wdp-cli" below). Both public repositories
turned out to be thin release and documentation fronts: neither carries
application source, a GoReleaser configuration, or any CI workflow, so how
the published binaries are built is not publicly observable. Per ADR 0001,
this document reports only publicly verifiable repository information and
excludes private repository names, links, and cross-repository
relationships.

## Install command(s) end users run

The [`wso2/choreo-cli` README](https://github.com/wso2/choreo-cli/blob/main/README.md)
documents two install paths:

```
curl -o- https://cli.choreo.dev/install.sh | bash
```
— for macOS, Linux, and WSL, and

```
iwr https://cli.choreo.dev/install.ps1 -useb | iex
```
— for Windows, both quoted from the
[README](https://github.com/wso2/choreo-cli/blob/main/README.md). The README
also documents a manual path: "Download the appropriate version from the
[releases]" page,
[wso2/choreo-cli/releases](https://github.com/wso2/choreo-cli/releases)
([README](https://github.com/wso2/choreo-cli/blob/main/README.md)).

Both `install.sh` and `install.ps1` were fetched directly from
`https://cli.choreo.dev/` (not GitHub) and returned real script content (HTTP
200, `content-type: text/x-sh`, served from an Azure Blob Storage-backed CDN
per the `x-ms-*` and `x-azure-ref` response headers observed at fetch time).
No WSO2 Choreo docs page under `wso2.com/choreo/docs` was reachable during
this research; the newer
`wso2.com/engineering-platform/developer-platform/docs/choreo-cli/get-started-with-the-choreo-cli/`
URL referenced in `product-authentication-compatibility.md` now serves
**WSO2 Developer Platform CLI** content (`wdp`, not `choreo`) — see
"Relationship to wdp-cli" below. A direct `curl` to that URL returned HTTP
403 (likely bot/edge protection); the page content quoted here was retrieved
through a fetch tool that renders the page rather than a bare HTTP client.

## Install script mechanics

### `install.sh` (macOS, Linux, WSL)

Fetched from `https://cli.choreo.dev/install.sh` (174 lines). Key mechanics,
quoted from the script:

- **Header/provenance:** `# Based on Deno and nvm installer: Copyright 2023
  the Deno authors. All rights reserved. MIT license.` and `# TODO(everyone):
  Keep this script simple and easily auditable.` The script runs under
  `set -e`.
- **OS/arch detection:** `local OS=$(uname -s | tr '[:upper:]' '[:lower:]')`
  and a `getArchitecture()` function that maps `uname -m` to `amd64`, `386`,
  `arm64`/`aarch64` → `arm64`, and `arm`; any other value causes
  `echo "Unsupported architecture: $ARCH"; exit 1`. Only `linux` and `darwin`
  are handled for OS; anything else prints `"Unsupported OS: $OS"` and exits
  1 — Windows and other OSes are explicitly rejected by this script (Windows
  instead uses `install.ps1`).
- **Version resolution:** `getVersion()` takes an optional version argument.
  If none is given and `LAST_RELEASE` is not `"true"`, it resolves the
  version by following the redirect target of
  `https://github.com/wso2/choreo-cli/releases/latest` with
  `curl -Ls -o /dev/null -w %{url_effective} ... | cut -d/ -f8` — i.e. it
  reads GitHub's redirect location rather than calling the GitHub Releases
  API for the common case. If `LAST_RELEASE=true` is set in the environment,
  it instead calls the GitHub Releases API
  (`https://api.github.com/repos/wso2/choreo-cli/releases`) and greps for the
  first entry *not* marked `"prerelease": true`.
- **Download URL construction:**
  `local FILE_NAME="choreo-cli-$LATEST_VERSION-$OS-$ARCH"`, with `.tar.gz`
  for Linux and `.zip` for Darwin, and
  `local INSTALLER_URL="https://github.com/wso2/choreo-cli/releases/download/$LATEST_VERSION/$FILE_NAME$FILE_TYPE"`.
  A commented-out line immediately above it reads
  `# TODO: change this to the actual release url` /
  `# local INSTALLER_URL="https://cli.choreo.dev/latest/$FILE_NAME$FILE_TYPE"`,
  i.e. the script still carries a dead reference to a CDN-hosted "latest"
  path that was apparently never wired up; probing
  `https://cli.choreo.dev/latest/...` during this research returned HTTP 404
  for every path tried, confirming that path is not live and all binary
  downloads go through GitHub Releases.
- **Download:** `curl -q --fail --location --progress-bar --output
  "$CHOREO_TMP_DIR/$FILE_NAME$FILE_TYPE" "$INSTALLER_URL"` into a
  `mktemp -d -t choreo-XXXXXXXXXX` temp directory.
- **Checksum/signature verification:** none. The script downloads the
  archive and immediately extracts it (`tar -xzf` or `unzip -q`) with no
  checksum comparison, no signature check, and no call to any manifest or
  provenance endpoint.
- **Install location:** `CHOREO_DIR=~/.choreo`, `CHOREO_BIN_DIR=$CHOREO_DIR/bin`,
  and the extracted `choreo` binary is moved to
  `$CHOREO_BIN_DIR/choreo` then `chmod +x`'d. No `sudo` is used anywhere in
  the script; the entire install is per-user, under the invoking user's home
  directory.
- **Shell completion:** after install, the script runs
  `./choreo completion $SHELL_TYPE > ./choreo-completion` (executing the
  freshly downloaded binary) and marks the completion file executable.
- **PATH / rc-file mutation:** a `detect_profile()` function picks a shell rc
  file to edit, preferring `$PROFILE` if set (skipping entirely if
  `PROFILE=/dev/null`, an explicit opt-out), then detecting bash
  (`~/.bashrc` or `~/.bash_profile`) or zsh (`~/.zshrc` or `~/.zprofile`) from
  `$SHELL`, and finally falling back through `~/.profile`, `~/.bashrc`,
  `~/.bash_profile`, `~/.zshrc`, `~/.zprofile` in that order. If a profile is
  found and does not already contain `$CHOREO_DIR`, the script appends
  `export CHOREO_DIR=...`, `export PATH=$CHOREO_DIR/bin:$PATH`, and a
  completion-sourcing line, wrapped in `# choreo cli` / `# choreo cli end`
  comments — all without prompting for confirmation. There also appears to
  be a **bug** in the "no profile detected" branch: the script logs
  `"No profile detected" / "Please add the following line..."` and then
  immediately still runs `echo ... >> $PROFILE` three times against an empty
  `$PROFILE` variable (rather than skipping the appends as the message
  implies), which would write to a file literally named nothing or fail,
  depending on shell behavior.
- **Upgrade / uninstall / self-update:** none. Re-running the script with a
  version argument re-downloads and overwrites the binary; there is no
  `choreo upgrade`/`choreo uninstall` logic in the installer itself, and no
  self-update mechanism is present in the script.

### `install.ps1` (Windows)

Fetched from `https://cli.choreo.dev/install.ps1` (89 lines):

- **Architecture detection:** `Get-Architecture` only distinguishes 64-bit
  vs 32-bit via `[Environment]::Is64BitOperatingSystem`, returning `amd64` or
  `386` — no ARM64 branch exists in this script, even though the release
  assets described below include a `windows-386.zip` and `windows-amd64.zip`
  but no `windows-arm64` artifact.
- **Version resolution:** `Get-Version` follows the same redirect-based
  approach as the Bash script: `Invoke-WebRequest ... -MaximumRedirection 0`
  against `https://github.com/wso2/choreo-cli/releases/latest`, reads the
  `Location` header on a 302, and takes the last path segment as the version.
- **Download URL:** `https://github.com/wso2/choreo-cli/releases/download/$VERSION/choreo-cli-$VERSION-windows-$ARCH.zip`.
- **Checksum/signature verification:** none, matching the Bash script.
- **Install location:** `$CHOREO_INSTALL_DIR` defaults to `$HOME\.choreo`
  (overridable via a `CHOREO_INSTALL` environment variable), with a `bin`
  subdirectory. The zip is expanded with `Expand-Archive` and the temporary
  zip is deleted.
- **PATH mutation:** the script reads the user-scope `Path` environment
  variable via `[Environment]::GetEnvironmentVariable("Path", $User)` and
  appends the bin directory with
  `[Environment]::SetEnvironmentVariable('Path', "$Path;$BinDir", $User)` if
  not already present — a per-user PATH change, not machine-wide, and no rc
  file equivalent (Windows has none) is touched.
- **Elevation:** the script does not require running PowerShell as
  Administrator overall, but it does escalate for one specific step: if
  `choreo.exe` is not already a symlink, it deletes any existing file and
  runs `Start-Process -FilePath "$env:comspec" -ArgumentList "/c", "mklink",
  $CHOREO_EXE -Verb runAs -WorkingDirectory "$env:windir"` — `-Verb runAs`
  triggers a UAC elevation prompt specifically to create an NTFS symlink from
  `choreo.exe` to the extracted binary, because creating symlinks on Windows
  normally requires administrator privileges by default. This is the only
  elevation point in either installer.
- **Upgrade / uninstall / self-update:** none present in the script.

## Release artifact build and publishing

- **Repository contents:** the full file tree of `wso2/choreo-cli`'s `main`
  branch is exactly `.gitignore`, `LICENSE`, `README.md`,
  `issue_template.md`, and `pull_request_template.md`
  ([tree via GitHub API](https://api.github.com/repos/wso2/choreo-cli/git/trees/main?recursive=true)).
  There is **no application source code, no `.goreleaser.yml`/`.goreleaser.yaml`,
  and no `.github/workflows` directory** in this public repository — a
  request for `.github` contents returned `404 Not Found`. The public repo
  functions purely as a README/issue-tracker/release front; it holds no
  build tooling.
- **Where the source lives:** release notes on `wso2/choreo-cli` credit pull
  requests raised outside this repository, and the repository itself holds no
  source. The practical implication is the only one this research needs: Go
  build tooling (GoReleaser or otherwise) and CI configuration for the Choreo
  CLI are not publicly visible, and only the already-built release binaries
  and the install scripts are. Per ADR 0001, the repository those pull
  requests belong to is not named here.
- **Artifact naming convention:** release assets follow
  `choreo-cli-<version>-<os>-<arch>.<ext>`, e.g.
  `choreo-cli-v1.2.33-darwin-amd64.zip`,
  `choreo-cli-v1.2.33-linux-amd64.tar.gz`,
  `choreo-cli-v1.2.33-windows-amd64.zip`
  ([v1.2.33 release assets](https://github.com/wso2/choreo-cli/releases/tag/v1.2.33)).
  The full `v1.2.33` asset set is: `darwin-amd64.zip`, `darwin-arm64.zip`,
  `linux-386.tar.gz`, `linux-amd64.tar.gz`, `linux-arm.tar.gz`,
  `linux-arm64.tar.gz`, `windows-386.zip`, `windows-amd64.zip` — eight
  platform/architecture combinations, matching what the install scripts can
  address (Linux/macOS/Windows on amd64, 386, arm, and arm64, except Windows
  has no arm64 asset).
- **No checksum or signature files:** none of the assets attached to the
  inspected releases (`v1.2.33` of `choreo-cli`, and the latest `wdp-cli`
  release, see below) include a `checksums.txt`, `.sha256`, `.sig`, or
  `.asc` file. Verification would have to rely on GitHub's own HTTPS
  transport and repository access controls alone.
- **Hosting:** artifacts are hosted directly on GitHub Releases under
  `wso2/choreo-cli`; no separate CDN-hosted binary distribution was found
  (the `cli.choreo.dev/latest/...` path referenced in a commented-out line
  of `install.sh` returned HTTP 404 on every path probed —
  `latest/choreo-cli-latest-linux-amd64.tar.gz`, `latest.txt`, `version`,
  `latest/version.txt`). `cli.choreo.dev` itself only serves the two install
  scripts, over HTTPS.
- **No version manifest endpoint:** no `latest.txt`-style file or API beyond
  GitHub's own `/releases/latest` redirect and `/releases` list endpoint was
  found; the install scripts rely on those two GitHub mechanisms exclusively
  for version resolution.
- **Release volume:** the public repository has published 225 releases,
  from `v0.0.7` through `v1.2.33`
  ([releases list](https://github.com/wso2/choreo-cli/releases)), indicating
  an active, frequently-tagged release cadence rather than a slow-moving
  distribution point.

### Relationship to `wdp-cli`

During this research, the public
[`wso2/wdp-cli`](https://github.com/wso2/wdp-cli) repository ("CLI tool for
WSO2 Developer Platform") was found to be structurally identical to
`choreo-cli`'s distribution setup, and evidence indicates it is the
in-progress rebrand/successor rather than an unrelated product:

- `wdp-cli`'s [README](https://github.com/wso2/wdp-cli/blob/main/README.md)
  documents installers at
  `https://raw.githubusercontent.com/wso2/wdp-cli/main/scripts/install.sh`
  and `.../scripts/install.ps1` (hosted directly in the repo under
  `scripts/`, rather than on a separate CDN domain like Choreo's
  `cli.choreo.dev`).
- The fetched `wdp-cli` `install.sh`
  ([raw source](https://raw.githubusercontent.com/wso2/wdp-cli/main/scripts/install.sh))
  is line-for-line the same script as Choreo's, with `CHOREO`→`WDP` and
  `choreo`→`wdp` substitutions throughout (same `~/.wdp` install directory
  pattern, same redirect-based version resolution against
  `wso2/wdp-cli/releases/latest`, same absence of checksum/signature
  verification, same profile-detection and rc-file append logic, and the same
  dead commented-out `cli.choreo.dev/latest/...` URL fragment left over from
  the Choreo script it was copied from).
  the `wdp-cli` latest release (`v1.2.32-wdp`) release notes explicitly say
  `"wdp cli: Add Ballerina configurable support and API definition export,
  with suspend/redeploy fixes"` and credit pull requests from the same
  non-public tracker Choreo CLI's own release notes credit
  ([wdp-cli latest release](https://github.com/wso2/wdp-cli/releases/latest)),
  which is what indicates both public repositories are release fronts for one
  shared source.
- `wdp-cli` release assets follow the same
  `wdp-cli-<version>-<os>-<arch>.<ext>` convention (e.g.
  `wdp-cli-v1.2.32-wdp-darwin-amd64.zip`,
  `wdp-cli-v1.2.32-wdp-linux-amd64.tar.gz`,
  `wdp-cli-v1.2.32-wdp-windows-amd64.zip`), with the same eight
  platform/arch combinations and, again, no checksum or signature files
  attached
  ([wdp-cli latest release](https://github.com/wso2/wdp-cli/releases/latest)).
  `wdp-cli` has only 9 published releases (versus 225 for `choreo-cli`),
  consistent with it being a newer, still-forming release stream.
- The newer WSO2 docs URL this research was asked to check —
  `wso2.com/engineering-platform/developer-platform/docs/choreo-cli/get-started-with-the-choreo-cli/`
  — currently renders content titled "WSO2 Developer Platform CLI
  Installation" and documents the `wdp-cli` install commands and `wdp`
  command name, not `choreo`, even though "choreo-cli" still appears in the
  URL path. This is treated here as first-hand evidence that WSO2's current
  developer-platform documentation is already presenting the Developer
  Platform CLI as the live successor at a URL slot that still carries the
  Choreo name, rather than as an unrelated or unreachable page.

No renaming announcement, migration guide, or deprecation notice for
`choreo-cli` was found in either repository's README, issues, or release
notes during this research; the evidence above is inferred from identical
script structure, shared private upstream, and the docs-URL content, not
from an explicit WSO2 statement.

## Windows support

Windows is supported, but through a script and ZIP path only — there is no
native installer, no Scoop manifest, and no WinGet manifest found:

- The `install.ps1` script (above) covers `amd64` and `386` only; there is no
  ARM64 branch in `Get-Architecture`, and no `windows-arm64` asset exists in
  the releases inspected.
- Release assets include `choreo-cli-<version>-windows-386.zip` and
  `choreo-cli-<version>-windows-amd64.zip`
  ([v1.2.33 release](https://github.com/wso2/choreo-cli/releases/tag/v1.2.33)) —
  plain ZIP archives, no MSI/MSIX/EXE installer.
- A search of Microsoft's public WinGet manifest repository,
  `microsoft/winget-pkgs`, for "choreo" returned no results via
  `gh search code "choreo" --repo microsoft/winget-pkgs`, and Homebrew's
  public formula API
  (`https://formulae.brew.sh/api/formula/choreo-cli.json` and
  `.../choreo.json`) returned HTTP 404 for both candidate formula names —
  there is no official or community Homebrew formula or WinGet package found
  for either `choreo` or `wdp`.
- The install script itself requires a Windows UAC elevation prompt (see
  "Install script mechanics" above) purely to create the `choreo.exe`
  symlink, which is a friction point specific to the scripted install path
  on Windows.

## Security-relevant observations

- **No checksum or signature verification anywhere in the pipeline.** Neither
  `install.sh` nor `install.ps1` computes or compares a checksum, and no
  release (`choreo-cli` or `wdp-cli`) publishes a checksums file, detached
  signature, or SBOM alongside its binaries
  ([v1.2.33 release assets](https://github.com/wso2/choreo-cli/releases/tag/v1.2.33),
  [wdp-cli latest release](https://github.com/wso2/wdp-cli/releases/latest)).
  Integrity rests entirely on HTTPS transport and GitHub/Azure-CDN access
  controls; this is the single most notable gap relative to the patterns
  documented for other CLIs in
  [root-cli-installation-distribution.md](root-cli-installation-distribution.md)
  (AWS CLI's detached PGP signature, Google Cloud CLI's published SHA-256
  values, kubectl's matching SHA-256 files).
- **`curl | bash` / `iwr | iex` piping with no version pin by default.** Both
  one-line installers execute remote content directly; running them without
  a version argument always resolves whatever GitHub currently reports as
  `/releases/latest`, so a user following the documented command gets
  "latest" implicitly rather than an audited, pinned version. The scripts do
  accept an optional version argument (`getVersion "$1"` /
  `Get-Version $version`), so pinning is *possible* but not the documented
  default in the README.
- **Unconfirmed shell rc-file mutation.** `install.sh` appends PATH,
  `CHOREO_DIR`, and completion-sourcing lines to the detected shell profile
  (`.bashrc`, `.bash_profile`, `.zshrc`, `.zprofile`, or `.profile`) without
  asking for confirmation, though it does honor an explicit `PROFILE=/dev/null`
  opt-out and avoids duplicate insertion by checking for `$CHOREO_DIR` first.
- **All downloads are HTTPS.** Every URL referenced by the install scripts
  and their resolved download targets (`cli.choreo.dev`,
  `github.com/wso2/choreo-cli`, `api.github.com`) uses `https://`; no plain
  HTTP endpoint was observed.
- **No `sudo` on macOS/Linux; one Windows UAC elevation.** The Bash installer
  never elevates and installs entirely under `$HOME`. The PowerShell
  installer only elevates (`-Verb runAs`) to create a single NTFS symlink,
  not for the download or extraction steps.
- **A stale, non-functional URL fragment survives in production scripts.**
  Both `install.sh` and (implicitly, by having been copied into) `wdp-cli`'s
  install script carry a commented-out, dead `cli.choreo.dev/latest/...`
  download URL next to a `# TODO: change this to the actual release url`
  comment — evidence the scripts have not been cleaned up since an earlier,
  apparently abandoned CDN-hosted distribution plan, even though the shipped
  behavior (downloading from GitHub Releases) works correctly.
- **Release front repositories without visible build tooling.** The public
  `wso2/choreo-cli` and `wso2/wdp-cli` repositories that end users are
  pointed to contain no source code and no visible build or release pipeline
  configuration. End users and external auditors cannot inspect how the
  published binaries are built from source, only that they arrive over HTTPS
  from GitHub Releases.
