# Cloud CLI architecture comparison

**Status:** Research
**Research date:** 2026-07-24  
**Scope:** Azure CLI, AWS CLI v2, and Google Cloud CLI as architectural
references for the WSO2 CLI.
**Source policy:** Only first-party documentation and official source repositories are used. Statements under “Verified findings” describe observed behavior; “WSO2 implications” are recommendations.

## Executive conclusion

The three cloud CLIs strongly support making the `wso2` shell responsible for:

- authentication and credential resolution;
- named contexts/profiles and configuration precedence;
- global output, querying, diagnostics, and non-interactive behavior;
- help, completion, version reporting, and update policy.

They do **not** provide a model WSO2 can copy unchanged:

- Azure has independently updated extensions, but loads Python wheels into the host process and permits direct URL/local installation.
- AWS achieves a highly uniform experience through a model-driven monolith; its plugin API is provisional and in-process.
- Google Cloud has a mature component manager and static command metadata, but components generally move on one SDK release train and public component authoring is closed.

The best fit remains the **SDK-first hybrid**: WSO2-published product modules
are separately signed Go executables, while a mandatory SDK/control contract
gives the shell enough structured information and runtime cooperation to
guarantee consistent authentication, output, errors, help, and completion.

## At-a-glance comparison

| Concern | Azure CLI | AWS CLI v2 | Google Cloud CLI | WSO2 direction |
|---|---|---|---|---|
| Runtime architecture | Python core plus in-process command modules and extension wheels | Python model-driven monolith with customizations | Python SDK with installable components | Go shell plus managed out-of-process Go modules |
| Independent feature delivery | Yes, extensions | No first-class managed module system | Components, usually on one SDK release train | Yes, independent signed product releases |
| UX consistency mechanism | Shared Python runtime/framework | Shared driver, service models, and global options | Shared runtime plus static CLI trees | Mandatory Go SDK plus generated schema/control contract |
| Auth/config owner | Core CLI | Core CLI/profile resolver | Core CLI/configurations | Shell and credential broker |
| Extension trust | Index checksum; direct wheel install also supported | Main installers can be PGP-verified; plugin API is unmanaged | Signed package repositories/installers; archive checksums | Signed catalog and manifests, provenance, revocation, and mandatory verification |
| Exact versions | Extension version selection | Exact installer/container versions | Exact SDK archive/component update target | Exact shell and module versions with pins/channels |
| Offline/enterprise | Local wheel/private index, packages, Docker | Offline/portable builds, MSI, containers | Versioned archives, package-manager ownership | Signed offline bundles and mirrorable registry snapshots |

## 1. Azure CLI

### Verified findings

#### Architecture and authoring

Azure CLI is implemented in Python and uses the hierarchy `az [group] [subgroup] [command]`. Its command modules inherit from `AzCommandsLoader`, register command groups and handlers, and use shared argument and help machinery. The official authoring workflow provides generators plus `azdev` commands for setup, tests, style checks, linting, packaging, and publishing. [Azure CLI repository](https://github.com/Azure/azure-cli), [command-module authoring](https://github.com/Azure/azure-cli/blob/dev/doc/authoring_command_modules/README.md), [extension authoring](https://github.com/Azure/azure-cli/blob/dev/doc/extensions/authoring.md)

An Azure CLI extension is a Python wheel that can add, modify, or remove commands. It is not an external executable: the host installs it into an extension directory and adds that code to the Python import path. Core modules and extensions consequently share one runtime and dependency environment. [extension model](https://github.com/Azure/azure-cli/blob/dev/doc/extensions/README.md), [extension loading and installation source](https://github.com/Azure/azure-cli/blob/dev/src/azure-cli-core/azure/cli/core/extension/operations.py)

#### Discovery, lifecycle, and compatibility

The CLI can list available and installed extensions; install, update, and remove them; list available versions; and install a specific version. It can also dynamically recognize a missing extension command and offer to install it. Extensions may come from Microsoft’s index, a private index, a URL, or a local wheel. [extension commands](https://learn.microsoft.com/en-us/cli/azure/extension), [extension management](https://learn.microsoft.com/en-us/cli/azure/azure-cli-extensions-overview)

Extension metadata may declare inclusive minimum and maximum CLI core versions. Resolution filters incompatible releases, and version listing distinguishes compatibility and the maximum compatible release. The update implementation backs up the installed extension and restores it when replacement fails. [extension metadata](https://github.com/Azure/azure-cli/blob/dev/doc/extensions/metadata.md), [resolution and rollback implementation](https://github.com/Azure/azure-cli/blob/dev/src/azure-cli-core/azure/cli/core/extension/operations.py)

`az version` reports CLI, core, telemetry, and extension versions. `az upgrade` upgrades to the latest CLI and, by default, all extensions; automatic upgrade is disabled by default and can be enabled. The built-in upgrader supports only the latest CLI, while platform package managers provide their own version controls. [installation and version output](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli), [upgrade behavior](https://learn.microsoft.com/en-us/cli/azure/update-azure-cli)

#### Auth, configuration, output, and UX

Authentication is core-owned. Azure CLI supports interactive users, managed identities, and service principals, selects a default subscription, and exposes short-lived access tokens through `az account get-access-token`. MSAL owns token acquisition and caching. Microsoft documents that its MSAL cache is encrypted on Windows but plaintext on Linux and macOS. [authentication methods](https://learn.microsoft.com/en-us/cli/azure/authenticate-azure-cli), [MSAL cache](https://learn.microsoft.com/en-us/cli/azure/msal-based-azure-cli)

Configuration follows a documented precedence: command-line parameters, then environment variables, then the configuration file. The user can set defaults such as resource group, location, output, confirmation behavior, logging, and telemetry. [configuration](https://learn.microsoft.com/en-us/cli/azure/azure-cli-configuration)

All commands receive shared global flags for JSON, JSONC, YAML, YAMLC, table, TSV, or suppressed output; JMESPath `--query`; verbosity; debug logging; warning suppression; and help. Queries are evaluated client-side before formatting. The CLI documents exit codes for success, generic failure, parser failure, and missing resources, and supports command/parameter completion. [output formats](https://learn.microsoft.com/en-us/cli/azure/format-output-azure-cli), [query semantics](https://learn.microsoft.com/en-us/cli/azure/use-azure-cli-successfully-query), [exit codes and completion](https://github.com/Azure/azure-cli#highlights)

#### Security, enterprise use, and telemetry

Official Linux package instructions configure Microsoft’s signing key. For indexed extensions, the index supplies a SHA-256 digest and the core rejects a downloaded wheel whose digest differs. However, direct URL and local-path wheel installation are also supported; the reviewed public extension documentation does not describe mandatory publisher signatures, build provenance, revocation metadata, or rollback-attack protection. [signed Linux package repository](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli-linux), [extension checksum enforcement](https://github.com/Azure/azure-cli/blob/dev/src/azure-cli-core/azure/cli/core/extension/operations.py), [direct-source installation](https://learn.microsoft.com/en-us/cli/azure/extension)

Enterprise deployment can use OS packages, MSI/ZIP installers, Docker, private extension indexes, proxy configuration, and documented network allowlists. Local wheels also provide a basic offline path. [installation choices](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli), [private indexes and local wheels](https://learn.microsoft.com/en-us/cli/azure/azure-cli-extensions-overview), [required endpoints](https://learn.microsoft.com/en-us/cli/azure/azure-cli-endpoints)

Azure CLI telemetry is enabled by default and can be disabled in configuration. Microsoft describes collecting aggregated usage data rather than private or personal data. [telemetry behavior](https://learn.microsoft.com/en-us/cli/azure/what-is-azure-cli#data-collection)

### WSO2 implications

Adopt:

- one author SDK for commands, flags, help, output, errors, and tests;
- exact module versions, compatible-version resolution, version inventory, and rollback on failed update;
- a curated registry with private-mirror support;
- one core-owned auth and configuration system.

Adapt:

- dynamic installation should require an explicit confirmation that shows publisher, version, capabilities, and verification status;
- compatibility should include host SemVer and module protocol ranges, not only host package versions;
- extension code should run out of process even though the SDK makes it feel integrated.

Reject:

- loading product modules into the host process;
- arbitrary URL or unsigned local installation in v1;
- checksum-only trust as the complete supply-chain design;
- dependency sharing between independently released modules.

## 2. AWS CLI v2

### Verified findings

#### Architecture and authoring

AWS CLI v2 is a Python application distributed with a bundled Python runtime in its portable builds. It is primarily model-driven: the driver enumerates Botocore services, constructs commands from service operation models, derives arguments from input shapes, and uses the same model data for help. An event system and the `awscli/customizations` tree provide explicit seams for bespoke workflows. [project metadata](https://github.com/aws/aws-cli/blob/v2/pyproject.toml), [source-build packaging](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-source-install.html), [CLI driver](https://github.com/aws/aws-cli/blob/v2/awscli/clidriver.py), [customizations](https://github.com/aws/aws-cli/tree/v2/awscli/customizations)

This is a monolithic release rather than independently versioned service modules. AWS’s documented plugin interface is an exception: it imports configured Python modules in-process, is explicitly “completely provisional,” provides no compatibility guarantee, and advises users to pin and retest the CLI. [plugin warning](https://docs.aws.amazon.com/cli/latest/topic/config-vars.html), [plugin loading](https://github.com/aws/aws-cli/blob/v2/awscli/clidriver.py)

#### Installation and versions

AWS publishes installers for Linux, macOS, and Windows plus official container images. The command-line installer can install an exact past release and manually replace an installation with `--update`; it does not automatically update. Snap auto-refreshes but cannot select minor versions, so AWS recommends the command-line installer when pinning is required. [installation and update behavior](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html), [past releases](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-version.html)

Exact `<major.minor.patch>` container tags are documented as immutable, while `latest` has no backward-compatibility guarantee. `aws --version` reports the CLI version along with Python runtime, OS, architecture, distribution type, and prompt mode. [container version policy](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-docker.html), [version output](https://github.com/aws/aws-cli/blob/v2/README.rst)

#### Auth, profiles, output, and UX

The root credential resolver owns named profiles shared with AWS SDKs. AWS documents a precedence chain across command options, environment variables, shared files, external processes, SSO, web identity, containers, and instance metadata. Long-term access keys are not accepted as global command-line parameters; a command selects a profile with `--profile`. AWS recommends short-lived IAM Identity Center/federated credentials, which the CLI caches and refreshes. [authentication precedence](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-authentication.html), [global options](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-options.html), [shared profile format](https://docs.aws.amazon.com/sdkref/latest/guide/file-format.html)

The shared `config` and `credentials` files are plaintext. `credential_process` offers an external credential-provider seam, while role, container, instance, and web-identity providers participate in the same resolver. [shared files](https://docs.aws.amazon.com/sdkref/latest/guide/file-format.html), [configuration providers](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html)

Common options cover profile, region, output, query, pagination, pager, timeouts, TLS/CA settings, color, auto-prompt, and error format. Output supports JSON, YAML, YAML stream, text, table, and suppression; `--query` uses JMESPath. The core normalizes service pagination and provides help and completion throughout the hierarchy. [global options](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-options.html), [pagination](https://docs.aws.amazon.com/cli/latest/userguide/cli-usage-pagination.html), [help](https://docs.aws.amazon.com/cli/latest/userguide/cli-usage-help.html), [completion](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-completion.html)

AWS documents structured error formats and return codes for parsing, configuration, service, interrupt, and general failures, although some codes remain overloaded. [return codes](https://docs.aws.amazon.com/cli/latest/topic/return-codes.html)

#### Security, enterprise use, and telemetry

Linux installer and source archives have detached PGP signatures, with documented manual verification. Signature verification is optional in the quick path and the reviewed docs do not describe signed update metadata, automatic rollback protection, module revocation, provenance, or SBOM admission. [installer signature verification](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html), [past-release signatures](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-version.html)

Enterprise/offline options include portable source builds, staged installation with `DESTDIR`, silent Windows MSI installation, exact-version containers, proxies, and custom CA bundles. [portable/offline source build](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-source-install.html), [Windows installation](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html), [proxy support](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-proxy.html)

The CLI adds version, installer, platform, prompt, command-lineage, and session metadata to AWS request user agents. Optional local CLI history can record command parameters and API requests/responses; AWS warns that this may include customer data. [driver metadata](https://github.com/aws/aws-cli/blob/v2/awscli/clidriver.py), [local history warning](https://docs.aws.amazon.com/cli/latest/userguide/data-protection.html)

### WSO2 implications

Adopt:

- generate repetitive command metadata from API/schema definitions while retaining escape hatches for workflow commands;
- profiles/contexts, documented credential precedence, short-lived sessions, automatic refresh, and logout;
- exact immutable versions and rich diagnostic version output;
- root-level paging, output, query, help, completion, and structured errors.

Adapt:

- use an OS-keychain-backed auth broker rather than plaintext shared credentials;
- make update/signature checks automatic rather than manual;
- use stable, non-overlapping WSO2 error categories instead of overloaded exit codes.

Reject:

- an in-process, provisional plugin interface;
- requiring users to pin the entire shell merely to keep one module compatible;
- recording arguments, results, or raw API traffic as default telemetry/history.

## 3. Google Cloud CLI

### Verified findings

#### Architecture and components

Google Cloud CLI uses Python 3.10–3.14 and organizes commands into nested product groups with explicit alpha, beta, preview, and GA surfaces. It generates static JSON “CLI trees” containing groups, commands, flags, positional arguments, help, and completer paths; these trees support completion, help search, interactive help, and documentation reproduction. [startup/runtime requirements](https://cloud.google.com/sdk/gcloud/reference/topic/startup), [CLI overview](https://cloud.google.com/sdk/gcloud), [CLI trees](https://cloud.google.com/sdk/gcloud/reference/topic/cli-trees)

A component may contain a CLI tool, command set, or dependency. The manager supports list, install, remove, update, and reinstall, installs dependencies, and can offer required components on first command use. Additional repositories are documented for Google Trusted Tester programs rather than as a general public plugin ecosystem. [component model](https://cloud.google.com/sdk/docs/components), [component commands](https://cloud.google.com/sdk/gcloud/reference/components), [additional repositories](https://cloud.google.com/sdk/gcloud/reference/components/repositories)

When Google Cloud CLI is installed through APT or YUM, the internal component manager is disabled and the OS package manager owns updates. [package-manager boundary](https://cloud.google.com/sdk/docs/components)

#### Versioning and updates

Internally managed components are selected for the current Google Cloud CLI version and generally move on one SDK release train. `gcloud components update` updates all installed components together and can target a newer or older SDK version. Component listing and `gcloud version` report installed and available versions. [component install](https://cloud.google.com/sdk/gcloud/reference/components/install), [component update](https://cloud.google.com/sdk/gcloud/reference/components/update), [version reporting](https://cloud.google.com/sdk/gcloud/reference/version)

Google also publishes self-contained, versioned archives for pinning, CI, fleet synchronization, and rollback. [versioned archives](https://cloud.google.com/sdk/docs/downloads-versioned-archives)

#### Auth, configurations, output, and UX

`gcloud init` combines authentication and initial configuration. The core supports human and federated identities, credential files, tokens, metadata-server identities, service accounts, and service-account impersonation through a documented precedence system. Google recommends federation and short-lived credentials over long-lived service-account keys. Application Default Credentials are deliberately separate from the credentials used by `gcloud` itself. [authentication and precedence](https://cloud.google.com/sdk/docs/authenticate), [ADC login](https://cloud.google.com/sdk/gcloud/reference/auth/application-default/login)

Named configurations group account, project, region/zone, verbosity, and product settings. One is active, another can be chosen per invocation, and flags and standardized environment variables override stored properties. [named configurations](https://cloud.google.com/sdk/docs/configurations), [property precedence](https://cloud.google.com/sdk/docs/properties)

The global formatting system supports JSON, YAML, CSV, table, text, value, flattened, and specialized formats. A common resource pipeline provides filtering, projections, flattening, sorting, and limiting. Google advises automation to select structured formats and depend on exit status, because default stdout and human-readable stderr can change. [format system](https://cloud.google.com/sdk/gcloud/reference/topic/formats), [filters](https://cloud.google.com/sdk/gcloud/reference/topic/filters), [scripting guidance](https://cloud.google.com/sdk/docs/scripting-gcloud)

Static CLI trees allow help and completion without loading every implementation. `gcloud info` reports environment and certificate diagnostics and can anonymize shareable output. [CLI trees](https://cloud.google.com/sdk/gcloud/reference/topic/cli-trees), [diagnostics](https://cloud.google.com/sdk/gcloud/reference/info)

#### Security, enterprise use, and telemetry

APT installation uses Google’s repository public key, the Windows installer is signed by Google LLC, and versioned archive pages publish SHA-256 checksums. The reviewed component documentation does not describe per-component publisher identities, signed manifests, provenance/SBOMs, revocation metadata, or rollback-attack protection. [APT trust](https://cloud.google.com/sdk/docs/install-sdk#deb), [signed Windows installer](https://cloud.google.com/sdk/docs/downloads-interactive), [archive checksums](https://cloud.google.com/sdk/docs/downloads-versioned-archives)

Versioned archives can be copied into CI and controlled fleets, while proxy settings, custom CAs, and certificate diagnostics support enterprise networks. [versioned archives](https://cloud.google.com/sdk/docs/downloads-versioned-archives), [proxy and CA configuration](https://cloud.google.com/sdk/docs/proxy-settings)

Usage reporting is opt-in during installation. Google says it collects command identity, duration, and error occurrence, but not argument values or personal information. The component manager periodically checks for updates, and configuration can disable automatic checks. [usage statistics](https://cloud.google.com/sdk/docs/usage-statistics), [component update behavior](https://cloud.google.com/sdk/gcloud/reference/components)

### WSO2 implications

Adopt:

- SDK-generated static command trees for host-rendered help, completion, and discovery;
- named contexts plus explicit flags/environment/context/default precedence;
- exact-version install, downgrade, reinstall, and readable version inventory;
- one installation owner at a time;
- first-class diagnostics, proxy, custom-CA, and anonymized support output;
- consent-based, shell-owned telemetry without argument values.

Adapt:

- keep modules independently released rather than copying Google’s lockstep component train;
- start with table/JSON/YAML and a modest selector instead of copying the entire formatting DSL;
- make on-demand installation explicit and security-aware.

Reject:

- requiring a shared Python runtime;
- a closed or undocumented authoring path;
- conflating the installed CLI’s user credentials with application/workload credentials.

## Cross-CLI design decisions for WSO2

### Adopt, adapt, or reject

| Pattern | Decision | Reason |
|---|---|---|
| Core-owned auth, profiles/contexts, and precedence | **Adopt** | All three demonstrate that consistency requires one credential and configuration authority. |
| Short-lived/federated credentials | **Adopt** | Reduces secret exposure and supports interactive and automated identities. |
| SDK-generated command/help schema | **Adopt** | Azure’s shared authoring and gcloud’s CLI trees show how to unify help/completion without duplicate manual metadata. |
| Typed results rendered by the shared layer | **Adopt** | Enables stable JSON/YAML/table behavior and query semantics across products. |
| Exact versions, pins, reinstall, and rollback | **Adopt** | Essential for enterprise reproducibility and independently released modules. |
| One installation owner | **Adopt** | Avoids conflicts between an internal updater and Homebrew/APT/MSI ownership. |
| Proxy, custom CA, mirror, and offline support | **Adopt** | Required for enterprise and air-gapped environments. |
| On-demand module installation | **Adapt** | Useful, but require explicit publisher/version/capability confirmation and full verification. |
| Global query/format DSL | **Adapt** | Begin with common formats and a small query surface; expand only from real use cases. |
| API-model-driven command generation | **Adapt** | Generate routine CRUD surfaces, while preserving hand-authored product workflows. |
| Telemetry and update checks | **Adapt** | Shell-owned, consent-based, metadata-only, rate-limited, and centrally disableable. |
| In-process extensions/plugins | **Reject** | Create dependency conflicts, host-crash risk, and a larger credential exposure boundary. |
| Arbitrary URL/unsigned local modules | **Reject for v1** | Conflicts with the “WSO2-published only” trust scope. |
| Checksum-only trust | **Reject** | Integrity without publisher authorization, revocation, or rollback protection is insufficient. |
| Lockstep shell and product releases | **Reject** | WSO2 product teams need independent ownership and cadence. |
| Module-owned credential stores/output/help | **Reject** | Would recreate today’s fragmented user experience. |

### Resulting recommended contract

The comparison strengthens these requirements for the SDK-first hybrid:

1. **Every production module uses the Go SDK.** Raw executable passthrough is a time-bounded migration state, not a compliant end state.
2. **The SDK generates the command schema** from the module’s command definitions. Product teams do not maintain a second help/completion document.
3. **The shell owns authentication and context.** Modules request short-lived, audience/scope-bound credentials through an inherited private IPC channel and never receive refresh tokens.
4. **Handlers return typed data or typed problems.** The SDK/shell owns JSON, YAML, table, quiet mode, redaction, stable error categories, and exit-code mapping.
5. **The registry resolves exact compatible versions.** Receipts record shell version, module version, protocol version, channel, pin, digest, publisher, verification result, and installation time.
6. **Verification is mandatory.** Use signed catalog metadata and release manifests, delegated product-team identity, immutable artifact digests, provenance/SBOM admission, expiry, rollback protection, and revocation.
7. **Installation is atomic.** Download to quarantine, verify, safely extract, run identity/health checks, activate atomically, and retain the previous verified version.
8. **Online and offline use have identical trust semantics.** Offline bundles and mirror snapshots carry the same signed metadata and verification evidence.
9. **The shell emits all telemetry.** Modules cannot independently phone home; arguments, output data, tokens, tenant/project names, endpoints, and raw error text are excluded.

## Remaining research gaps

Before freezing the architecture contract, investigate:

1. **Supply-chain standard:** compare TUF, Sigstore bundles, in-toto/SLSA provenance, and OCI artifacts for the WSO2 registry and offline verification.
2. **Auth broker protocol:** inventory WSO2 product audiences, scopes, login types, token exchange, on-prem authentication, impersonation, and non-interactive workload identities.
3. **OS credential stores:** confirm macOS Keychain, Windows Credential Manager, and Linux Secret Service behavior, including headless Linux fallback policy.
4. **Go SDK feasibility:** prototype automatic Cobra schema extraction, flags after the module namespace, completion, structured streaming, cancellation, and progress.
5. **Module sandbox boundary:** document which capabilities can truly be enforced for native executables and whether mediated HTTP or WebAssembly is justified later.
6. **Enterprise lifecycle:** validate proxies, custom CAs, mirrors, offline bundle import/export, fleet pinning, revocation freshness, and package-manager ownership with WSO2 customers.
7. **Compatibility policy:** define SemVer/channel rules, deprecation windows, minimum supported shell versions, protocol negotiation, and emergency rollback.
8. **Migration evidence:** pilot one clean CLI and one legacy CLI to measure SDK integration effort and identify unavoidable escape hatches.
