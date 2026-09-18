# WSO2 CLI shell commands

**Status:** Reference
**Related:** [Product requirements](../product-requirements.md),
[architecture](../architecture.md)

This reference describes the shared `wso2` shell commands: `context`, `login`,
`logout`, `whoami`, `org`, `doctor`, `config`, `product`, `version` and
`help`. Product operations belong to product modules, such as `wso2 api`,
`wso2 identity`, `wso2 integration`, and `wso2 agent`; the
[module catalog](module-catalog.md) reference describes what a product install
selects, what it verifies, how a channel and a pin are recorded per module,
and how each refusal is reported.

## Commands

| Command | Description |
| --- | --- |
| `wso2 help` | Shows the root command tree and help for a command. |
| `wso2 version` | Shows the shell, protocol, and installed module versions. |
| `wso2 completion <shell>` | Writes the tab-completion script for `bash`, `zsh`, `fish` or `powershell` to standard output. |
| `wso2 login` | Establishes the login session, then one session per further product the context records, each through the same browser sign-on. `--only <namespace>` authorizes just that product — both of its records when it holds a gateway record, or one record alone as `--only <namespace>/gateway` — refused with `shell.invalid_argument` when the context records no such namespace; `--no-products` authorizes the login session alone, leaving product and gateway sessions unestablished; the two together are refused with `shell.conflicting_arguments`. The report lists every session this run established, naming the strategy that reached it (`direct`, `sibling`, `derived`, or `federated`) as `<strategy>, established`; a gateway record is its own line under its key, as `apim/gateway  sibling, established`. When a later product fails after earlier ones already succeeded, the failure names what was established and points at `wso2 login --only <namespace>` (or `--only <namespace>/gateway`) to retry only the one that was not, keeping the sessions already stored. Without `--context` and `--url`, in a terminal, login asks `Log in to:` an existing context (listed, the selected one as the default) or a new one; a new one runs the `wso2 context create` wizard (below), without its closing offer to log in, and then logs in to the context it wrote; a client-credentials context ends there, reporting that it needs no login. With no contexts it goes straight to a new one. A `--context` naming no context runs the same wizard, taking that name, in a terminal and is refused with `shell.missing_required_flag` otherwise, naming `--url` and `--client-id`. Under `--no-input`, `WSO2_NO_INPUT`, `WSO2_CONTEXT`, or a standard input that is not a terminal, nothing is asked and the selected context is used. |
| `wso2 login --url <issuer> --client-id <id>` | Logs in against a named issuer and creates the context it authenticated, reporting its name. `--context <name>` names it. Without it, a context that already logs in against that issuer with that client ID is reused; otherwise login asks `Context name [context-1]:`, offering the next free `context-N`, where Enter accepts the default and a name that is not legal or is already taken is asked again. When standard input is not a terminal the default is taken without asking, and the report says the name was assigned, naming `--context <name>` and `wso2 context rename`. A context named by `--context` whose issuer and client ID both match is reused; one that differs in either is refused with `contexts.context_exists` and never replaced. The first context created becomes the selected one. Nothing is written unless the login succeeded. Omitting `--client-id` prompts in an interactive terminal and is refused with `shell.missing_required_flag` under `--no-input`. A ThunderID issuer binds every login to a product, so there a login that creates its context is refused with `auth.product_not_configured`, naming `wso2 context create <name> --login-product identity --url <url>`. |
| `wso2 logout` | Ends every session the selected context holds — the login session and each product's own — asking the identity provider to revoke each refresh token and removing each shell-owned entry. A context's sessions are its own (ADR 0016), so no other context is affected. `Product sessions` in the report names what happened to each session beyond the login one, `<namespace> ended` or `<namespace> none`. A client-credentials context holds no session to end and reports that plainly, exiting 0. Ending the shell's sessions leaves each identity provider's own browser session in place, and a later login would then be silent; so logout also opens each provider's end-session page, once per provider and client, naming the client and the identity token the session recorded, and reports `Browser session` as `sign-out opened`, `sign-out printed` (no browser could be opened; the URLs are on standard error), `kept` (`--keep-browser-session`, `--no-input` or `WSO2_NO_INPUT`), or `unaffected` (nothing was stored). Ending the browser session is best effort like revocation, and some providers end the user's other refresh tokens with it; the report says so rather than claiming either way. |
| `wso2 whoami` | Built today: shows the selected context, the issuer it logs in through, the organization when the context names one, the session's subject, and the session's own state, all read from local state with no network call. With no context configured or selected it says so and exits 0. With a context selected but no stored session it says so and names `wso2 login`. A stored session is reported present with its expiry either as the issuer's disclosed refresh-token lifetime or, when the issuer disclosed none, as not stated — never as the shorter-lived access token's own expiry. A disclosed lifetime that has passed is reported expired. A session stored before this field existed reports its subject as unknown rather than blank. It also lists every record the context holds, each with the strategy that reaches it (`direct`, `sibling`, `derived`, `federated`, `exchanged`, or `inline` for a client-credentials context) and whether that record's own session is stored: every product under its namespace, and a product's gateway record under its key, as `apim/gateway: sibling, present` beside `apim: federated, present`. An exchanged or inline record holds no session of its own, so it is written once: an inline one as its strategy, and an exchanged one as `api: by exchange (not checked)`, since whoami makes no call and cannot tell whether the issuer will grant the exchange. JSON keeps `exchanged` in both `strategy` and `session`. A client-credentials context reports its own state as `inline` too, with no recovery text, since it holds nothing to recover. |
| `wso2 org` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `current` and `use`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 org list` | Deferred (#112): no control-plane endpoint for enumerating organizations exists yet, and listing would need an access token before an organization is chosen, which the auth broker refuses. Typing it is refused as an unknown `wso2 org` subcommand, naming `current` and `use` as what the family supports. |
| `wso2 org use <organization>` | Built today: sets the `Organization` field on the selected context through `contexts.Update`, and names which context it edited. The auth broker binds a minted token to a context's `Organization` and refuses when it is empty, so a session already signed in under the previous organization no longer matches — the command warns about that on standard error, in both renderings. It writes nothing, and does not migrate, invalidate, or re-mint a session. Refused with `shell.no_context_configured` when no context exists to edit, and with `auth.organization_switch_unsupported`, naming the provider, when the selected context logs in against ThunderID or Identity Server, which have no organization to switch to: the auth broker would refuse every later command with the same code, so the value is refused at the flag instead. An empty value is allowed on those providers, because it clears the field rather than switching to an organization, and this command is the only writer of it: refusing that too would leave a context that already carries an organization unrepairable. |
| `wso2 org current` | Built today: shows the organization the selected context runs within, read from `contexts.Context.Organization`. With no context configured it reports that state and exits 0, worded exactly as `wso2 context current`. A configured context with no organization set says so distinctly, rather than reporting the same blank field either state would otherwise share. |
| `wso2 context` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `create`, `product`, `apply`, `use`, `list`, `current`, `show`, `rename`, `delete`, `edit` and `export`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 context create [<name>]` | With neither `--login-product` nor `--issuer`, in a terminal, asks for the setup instead (#189): `Sign in with:` the WSO2 identity product, one of `Thunder`, `Asgardeo`, `WSO2 Identity Server (coming soon)`, or `WSO2 Cloud (coming soon)`, the two marked coming soon of which are refused where they are picked, each saying so. Thunder logs in through the installed product whose descriptor names Thunder, or `identity`, installed first, and asks its URL, plus a client ID or audience only when the descriptor names none. Asgardeo asks the organization URL (`https://api.asgardeo.io/t/<organization>`) and writes it with `/oauth2/token` added as the issuer, with the matching `provider`, and asks the client ID. Then `Sign in using:` a browser, a device code, or client credentials, whose secret variable is asked by name; `Add a product this context reaches:` each installed product that is not a login provider, with its URL, its gateway URL when it has one, and its client ID when its grant needs one the descriptor does not name; the name, unless given; then a summary, `Create the "<name>" context?`, `Select "<name>" as the current context?` unless `--use`, and `Log in now?`. Flags given are not asked. Every answer goes through the same checks and descriptor resolution as the flags, and nothing is written before the last question, so the result is what the equivalent command lines write. A name that is taken or illegal is refused before any question. Answering no at the summary, or cancelling with Ctrl+C, is `shell.cancelled` and writes nothing; input that ends before a required answer is `shell.missing_required_flag`. With `--output json` it asks only for the context and writes one result. Under `--no-input`, `WSO2_NO_INPUT`, or a standard input that is not a terminal, nothing is asked: a missing name is `shell.missing_argument`, and the flag rules below apply. |
| `wso2 context create <name> --login-product <product> --url <url>` | Creates a context that logs in through an installed product that is a login provider (a descriptor naming a `provider`, such as `identity`). The descriptor fills in the issuer (the URL plus its `issuerPath`), the client (`--client-id` overrides), the provider (`--provider` overrides), and the login product's audience and scopes (`--audience`, `--scopes` override), and the complete record is written (frozen defaults, ADR 0016). A product that is not installed is installed first, reported on standard error; `--no-install` refuses instead with `shell.product_not_installed`. A product that is not a login provider is refused with `shell.invalid_argument`. `--device` makes it an `oauth-device` context and `--client-id <id> --client-secret-variable <VAR>` a `client-credentials` one, refused when the descriptor's `machine` list does not allow `inline`. `--organization` and `--project` set those members. The context's `credentialRef` is its name, or the name with a numeric suffix when another context already holds that reference. Nothing is selected unless `--use` is given. A name already declared is refused with `contexts.context_exists`, and an illegal one with `shell.invalid_argument` before anything is read. |
| `wso2 context create <name> --issuer <url> --client-id <id>` | Creates a context that logs in through any OpenID provider directly, with no product installed and no product recorded yet: add them with `wso2 context product add`. `--provider asgardeo|identity-server` names the provider; `--provider thunder` is refused with `shell.conflicting_arguments`, because a ThunderID login is bound to a product and this form records none. `--url` without `--login-product` is refused with `shell.missing_required_flag`: a URL alone does not say which product's defaults apply, and the shell does not guess. Neither form, or both, is refused the same way. No network call is made besides an install. |
| `wso2 context product add [<product>]` | Without `--url`, in a terminal, asks `Product:` (installed products the context does not record and that are not login providers, when no product is named), its URL, its gateway URL when it has one, and its client ID when its grant needs one; then shows the `--dry-run` report and asks `Add the <product> product?` before writing. With `--dry-run` or `--output json` the report is the only result and nothing more is asked. Declining is `shell.cancelled`. Under `--no-input`, `WSO2_NO_INPUT`, or a standard input that is not a terminal, nothing is asked: a missing product is `shell.missing_argument` and a missing `--url` `shell.missing_required_flag`. |
| `wso2 context product add <product> --url <url>` | Records a product on the selected context, or the one `--context` names (the one `context` subcommand that takes the flag). The installed product's descriptor fills in the audience, scopes and grant; `--audience`, `--scopes` and `--client-id` override them, and a product whose module declares no descriptor is recorded exactly as given. `--gateway <url>` records the product's gateway beside it, its audience defaulting to the gateway URL for a product reached by exchange (`--gateway-audience` and `--gateway-scopes` override). A login provider whose issuer is not the context's is refused, naming `wso2 context create`. On a client-credentials context the descriptor's `machine` list decides whether the product is reached from the context's own client or needs `--client-id-variable`/`--client-secret-variable`. The login product is frozen before the product goes in, so a product that sorts earlier never moves the login. A product already recorded is refused with `contexts.product_exists` unless `--replace`, which replaces the whole record and first ends every session the new record no longer matches (its URL, audience or grant changed), with revocation best effort as ADR 0010 decides. `--dry-run` shows the record and the sessions that would end and writes nothing; for a product not installed yet it says what would be installed. A missing product is installed first unless `--no-install`. |
| `wso2 context product remove <product>` | Stops the selected context (or `--context`) reaching a product. It drops the product's record, and the product's gateway record with it; `<product>/gateway` drops the gateway record alone. Every session the removal leaves nothing to reach is ended first, the way `wso2 logout` ends one, because the secure store cannot be listed and a session whose record is gone could never be ended again. Removing the login product is refused with `contexts.login_product`; a record the context does not hold with `contexts.unknown_product`, naming every record it does. `--dry-run` names what would be removed and ended. |
| `wso2 context apply -f <file>` | Writes each context in a shared **input file** (`-f -` reads standard input). The file lists contexts only, each as short as the installed descriptors allow; it never names a `credentialRef` or a `defaultContext`, and unknown members are refused, all with `shell.input_malformed`. In order: the whole file is validated; the plan is made (products to install, products installed at a version other than a pinned `"version"`, and for each context create, replace with a field-by-field diff, or unchanged, plus the sessions that would end); `--dry-run` stops there; missing products are installed; defaults are resolved from the installed descriptors; the sessions whose bindings change are ended; and the document is written under its lock. A context in the file replaces the one of the same name whole, keeping its `credentialRef`; contexts the file does not name are kept. The selection changes only with `--use <name>`, which must name a context in the file. A product installed at another version than its pin is left as it is and reported, unless `--update-products`. `--no-install` installs nothing, and needs each product installed or stated in full. When an install fails after another succeeded, the successful ones stay installed and `contexts.json` is untouched; running the command again continues. |
| `wso2 context use <context>` | Selects the context used by default for later commands. |
| `wso2 context list` | Lists the configured contexts with their type, issuer and products, marking the selected one, and says so when none is selected. |
| `wso2 context current` | Shows the selected context. |
| `wso2 context show` | Reports where the context document lives — inside the shell's state root, which `WSO2_HOME` overrides — and shows it whole: the schema version, the selected context, every context with its login and credential *source* (a secure-store reference, or the environment variable a client-credentials context reads its secret from — never a credential value), and every product record. The path is reported whether or not a document has been written there yet. It ends with notes: that a document read at an earlier schema version is written as version 4 on the next write, and any record whose frozen values differ from what its installed product's descriptor would produce now, naming the command that adopts them. It makes no network call and reads nothing from the secure store. |
| `wso2 context rename <name> <new-name>` | Renames a context and keeps it selected if it was. Its `credentialRef` is a stable identifier that does not follow the name, so no session moves and no login is needed. |
| `wso2 context delete <name>` | Deletes a context after ending every session under its `credentialRef` — the login session, each product's and each gateway's — so no secret is left in the secure store that no document names. Deleting the selected context leaves nothing selected. `--dry-run` names what would be ended. |
| `wso2 context edit` | Opens the complete document in `$VISUAL` or `$EDITOR`, validates the result as a whole, refuses an invalid one (asking whether to edit again at a terminal) and leaves the file as it was, and ends the sessions a changed record no longer matches before writing. It takes complete records only; `wso2 context apply` is the way to write short ones. Refused with `shell.not_interactive` under `--no-input` or without a terminal, naming the file to edit directly. |
| `wso2 context export [<name>]` | Prints contexts in the input-file form: complete records, so the file applies the same whatever product versions another machine has installed, with every `credentialRef` and the selection removed. |
| Removed commands | `wso2 <product> connect …` and the `wso2 account` family were removed (ADR 0016). Typing one prints the exact replacement, built from the line typed, and exits in the usage class with `shell.command_moved`: `wso2 identity connect <url> --account demo` names `wso2 context create demo --login-product identity --url <url> --use`; `wso2 api connect <url> --gateway --account demo` names `wso2 context product add api --url <api-url> --gateway <url> --replace --context demo`, with the product's own URL left as a placeholder because the old line never named it; `wso2 account add-product <a> <p> --endpoint <u>` names `wso2 context product add <p> --url <u> --context <a>`; `wso2 account list` names `wso2 context show`. A redirect fires only after normal dispatch has declined the words, so a module that declares a `connect` command of its own is reached as usual. |
| `wso2 config` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `list`, `get`, `set` and `unset`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 config list` | Built today: shows every key in the closed set of shell preferences — the default output mode and the catalog origin override — and whether each is currently configured. |
| `wso2 config get <key>` | Built today: shows one shell preference. `key` must be one of `output`, `catalog-origin`; any other value is refused with `config.unknown_key`, naming the valid keys. |
| `wso2 config set <key> <value>` | Built today: changes one shell preference. An unknown key is refused the same way `config get` refuses one; a value a key does not accept is refused with `config.invalid_value`, naming what is acceptable (`table` or `json` for `output`; an absolute http or https URL for `catalog-origin`). Each preference is the lowest-precedence source for what it governs: `--output` wins over a configured output mode, and `WSO2_CLI_CATALOG_ORIGIN` wins over a configured catalog origin — a saved preference can never override either. `output` governs exactly the commands that accept `--output`: `wso2 whoami`, `wso2 doctor`, `wso2 logout`, and the `context`, `config` and `org` families. It does not reach `wso2 version` or the `product` family, which render fixed prose and refuse `--output` with `shell.unsupported_flag`; a preference that silently did nothing for them would be a worse contract than a flag refused out loud. A colour preference is not in this set: `output.ColorEnabled` has no production caller yet, so a key that claimed to govern colour would change nothing observable; it is the obvious first key to add once something renders in colour. |
| `wso2 config unset <key>` | Built today: removes one preference, so its built-in default governs again. Unsetting a key that was never set succeeds and reports the default that governs, since the config family already treats "unset" as a fact rather than an error. |
| `wso2 product available` | Deprecated spelling of `wso2 product list`, which ADR 0015 merged it into. It is hidden from help, runs the merged list, and names `wso2 product list` on standard error. |
| `wso2 product install <product>` | Installs the latest compatible stable release of a module. |
| `wso2 product install <product>@<version>` | Installs an exact compatible module version. |
| `wso2 product list` | Lists every product the module catalog publishes or this machine has installed, in one table: the installed version or `—`, the channel, and the update available or the version an install would take. When the catalog cannot be reached, it lists the installed products with update `unknown`, warns on standard error that the updates and the installable products are unknown, and exits 0. |
| `wso2 product update <product>` | Updates one product module. Naming a module is already an explicit target, so this does not prompt; `--dry-run` still reports what it would do without changing anything. A module the catalog publishes no version of on its followed channel — withdrawn, renamed, or moved to a channel this install no longer follows — is reported by name rather than called current, since the catalog cannot say whether the installed version is current when it does not publish the module at all; this does not change the exit status. |
| `wso2 product update --all` | Built today: updates every installed product module that has a newer version on its followed channel, skipping a pinned one. Being unbounded, it prompts for confirmation before moving anything; `--yes` skips the prompt, `--dry-run` reports what it would do without changing anything, and `--no-input` (or `WSO2_NO_INPUT`) refuses rather than prompt. Refuses `shell.non_interactive` when it may not prompt and `--yes` was not given — either because `--no-input` or `WSO2_NO_INPUT` asked that nothing prompt, or because standard input is not a terminal; the refusal names the control that fired and offers the one way out that applies to it, and `shell.conflicting_arguments` for `--yes` with `--dry-run`. A module the catalog publishes no version of on its followed channel — withdrawn, renamed, or moved to a channel this install no longer follows — is reported by name rather than called current, since the catalog cannot say whether the installed version is current when it does not publish the module at all; this does not change the exit status, so a scheduled `--all` run does not start failing the moment one module goes unpublished upstream. |
| `wso2 product remove <product>` | Built today: removes one installed module, leaving configuration and credentials alone. Prompts for confirmation after confirming the module is installed; `--yes` skips the prompt, `--dry-run` reports what it would remove without removing anything, and `--no-input` (or `WSO2_NO_INPUT`) refuses rather than prompt. Refuses `shell.module_not_installed` before any prompt when the module is not installed, `shell.non_interactive` when it may not prompt and `--yes` was not given — either because `--no-input` or `WSO2_NO_INPUT` asked that nothing prompt, or because standard input is not a terminal; the refusal names the control that fired and offers the one way out that applies to it, and `shell.conflicting_arguments` for `--yes` with `--dry-run`. |
| `wso2 doctor` | Built today: checks that the context document is valid, that the OS secure store is reachable, and that the selected context has a stored session, and (the `defaults` check) whether any product record differs from what its installed product's descriptor would write now, reported `differs` without failing, since a product update changes nothing until a context is applied again. The context check's own detail names the document it checked — its path inside the shell's state root, which `WSO2_HOME` overrides — whichever way the check comes out; `wso2 context show` shows that same document whole. The session check covers the login record and every product record the context holds, a product's gateway record among them under its key (`apim/gateway`). It reports none, with the `wso2 login` pointer in its recovery column, when a record has no stored session at all and names the ones without one, because being logged out is the state a completed `wso2 logout` leaves behind, not a health fault; a session that is stored but cannot be read still fails. `--online` adds two more checks: that the OpenID configuration of every issuer the selected context names can be read — the check that finds a certificate this machine does not trust, refused as `auth.certificate_untrusted` with the same recovery a product command gives, or `auth.discovery_failed` for any other reason — and module catalog reachability; without it, `wso2 doctor` makes no network call. On an unconfigured machine, the secure-store and session checks report not-applicable rather than failure; on a context document that fails to decode or validate, the session check reports not-applicable too, because no credential reference can be resolved from it, while the secure-store check still runs since it never reads the document. The session check is also not-applicable for a client-credentials context, and when no context is selected; a client-credentials context acquires access inline and holds no session to check. Exits 0 when every check passes, is not-applicable, or reports none, otherwise the exit class of the most severe failing check, in this rank: secure-store, then the document, then the session, then (only under `--online`) the issuer, then the catalog — a rank this command defines and not the numeric order of the exit classes those checks carry. Receipt, module integrity, compatibility, and protocol status are not built yet; see [architecture](../architecture.md#14-operational-behavior). |

`project` commands are intentionally not included yet. Product-specific
projects, deployment, and runtime operations remain within their product
modules.

**Not built yet:** a root-shell self-update command, `product info`/`verify`/
`rollback`, installing a module from an offline `.wso2module` file, and the
offline bundle family (`create`, `inspect`, `install`) for air-gapped
machines.

## Login errors

Every refusal `wso2 login` and a brokered command make carries a typed code.

| Code | Meaning | Fix |
| --- | --- | --- |
| `auth.context_not_selected` | No context document exists, or none is selected. | `wso2 context use <name>`, `wso2 context apply -f <file>`, or `wso2 login --url <issuer> --client-id <id>`. |
| `auth.discovery_failed` | The issuer's OpenID configuration could not be read or is unusable: an inexact `issuer`, no network path, no `S256` support, or no loopback port free. | Compare `issuer` to `<issuer>/.well-known/openid-configuration` character for character; check connectivity; enable PKCE `S256`; free a port in 10425-10428. |
| `auth.certificate_untrusted` | The issuer's TLS certificate is not trusted by this machine — the usual first failure against a fresh self-hosted install. | Trust the certificate, or set `WSO2_CA_FILE`; see [Trusting a deployment's certificate](#trusting-a-deployments-certificate). |
| `auth.login_required` | No usable session for this `credentialRef`: never logged in, or the deployment stopped accepting the stored refresh token. | Run `wso2 login` again. |
| `auth.keyring_unavailable` | The OS secure store could not be used (commonly no Secret Service on headless Linux). | Start a keyring daemon, or use a `client-credentials` context. |
| `auth.credential_unavailable` | A `client-credentials` context's secret variable is unset or rejected, or a browser/device login ended without tokens. | Name the variable correctly and export it; for a browser login, retry and complete consent. |
| `auth.narrowing_unavailable` | A token was obtained but the shell could not prove it was narrowed to what the module asked for, so it refused to hand it over — by design. | The message names which of five causes applied: opaque token, no scope claimed, deployment ignored the narrower request, wrong `audience`, or `invalid_scope`. Fix the named cause in the context document or the deployment's application config. |
| `auth.organization_switch_unsupported` | The context's `organization` names something other than its `login.tenant`. | Make them match, or use a context whose home tenant is the target organization. |
| `auth.product_not_configured` | A module asked for an audience or scope this context does not register for it, or a login named no resource server a ThunderID deployment requires. | `wso2 context product add <product> --url <url> [--replace]`, or `wso2 context create <name> --login-product <product> --url <url>`. |
| `auth.audience_not_declared` / `auth.scope_not_declared` | The module asked for more than its own installation declares. | Reinstall the module; this is not a context problem. |
| `auth.login_not_required` | `wso2 login` was run against a context that carries its own credential. | Just run the command; there is no session to establish. |
| `auth.non_interactive` | A browser or device login was attempted under `--no-input` or `WSO2_NO_INPUT`. | Use a `client-credentials` context for automation, or drop the flag/variable to run interactively. |
| `auth.kind_not_implemented` | The context's `login.kind` is `pat`, which the schema names but this release does not implement. | Use `oauth-browser`, `oauth-device`, or `client-credentials`. |
| `auth.session_issuer_mismatch` | The stored session was established against a different issuer than the context now names. | Run `wso2 login` again. |
| `auth.session_required` | Nothing is stored for a product and `--no-input`/`WSO2_NO_INPUT` forbids the browser that would authorize it. | `wso2 login --only <namespace>` where a browser can open. |
| `auth.reauthorization_required` | A session for the product is stored, but the deployment would not renew it to the permissions the module needs. | Log in again where a browser can open; if it still refuses, an administrator has to grant the role. |

## Exit classes

The shell alone decides the process exit status: a module returns a typed
problem and the shell maps its category to one of these classes. The mapping is
a stable contract for automation, so a script may branch on the status without
parsing any output.

| Status | Class | What produces it |
| --- | --- | --- |
| `0` | Success | The command completed. |
| `64` | Usage | Invalid arguments, flags, or configuration, including a malformed context document. |
| `69` | Module trust | A module integrity, signature, platform, or compatibility failure. |
| `70` | Module process | A protocol violation, a module process that failed to launch or crashed, or an unreachable module catalog origin (including `wso2 doctor --online`'s catalog check). |
| `75` | Product service | A failure the product service itself reported. |
| `77` | Authentication policy | An authentication or broker policy failure, including no context selected, and a missing or expired session. |

An unrecognized problem category is reported as a module process failure, `70`,
rather than as success.

One command opts out of the `77` class on purpose: `wso2 reference status`
reports a broker refusal as fields of its result and exits `0`, because
whether access was granted is the question it exists to answer, and a report
that cannot say "no" cannot answer it. A gate that must fail without access
runs `wso2 reference call` or `wso2 reference whoami`, which keep the `77`
class for the same refusal.

## Non-interactive use

`--no-input` declares that nothing may prompt, open a browser, or wait for a
human. `WSO2_NO_INPUT`, set to any non-empty value, does the same for every
invocation in the environment. A job that sets either wants to fail fast on a
misconfigured context rather than hang until its own timeout.

When a question may be asked, it is asked on standard error, never standard
output. On a terminal it is drawn as a form: options are picked with the arrow
keys and Enter, an answer is checked as it is typed, and Ctrl+C or Esc cancels
with `shell.cancelled`, having written nothing. Where standard input or
standard error is not a terminal, and whenever `WSO2_ACCESSIBLE` is set to any
non-empty value, the same questions are printed as numbered, line-oriented
prompts, which suit a screen reader (ADR 0017).

A browser or device login under `--no-input` is refused with
`auth.non_interactive` and exit class `77`. Automation authenticates with a
client-credentials context instead, which acquires access inline with no login
step.

`--no-input` is the shell's own flag on a product command line too, read
wherever it is written, like `--verbose`, and never forwarded to the
module. Wherever ends at a bare `--`, as it does for `--output` and
`--context`: everything after the separator is the module's, unread, so
`wso2 <namespace> call /x -- --no-input` hands the module the word and
leaves the shell interactive. A module flag whose value is that literal
word takes it attached, `--description=--no-input`, because the shell
reads the separated spelling before it knows which of the module's flags
take values. A product command that finds no session yet for its own product
opens the browser to authorize it, printing the product and the issuer
first, unless `--no-input` or `WSO2_NO_INPUT` asked otherwise, in which
case it refuses with `auth.session_required` and exit class `77`, naming
the control that refused and `wso2 login --only <namespace>` as the way to
establish that session ahead of time. Whichever of the two asked, the
module is handed `WSO2_NO_INPUT=1`, so a module reads one spelling.

## Trusting a deployment's certificate

Every self-hosted product serves TLS with a self-signed certificate on a fresh
install, and until it is trusted the shell cannot read the issuer's discovery
document, so login fails before a browser opens and every product command
fails at the same point. The shell refuses with `auth.certificate_untrusted`
and exit class `77`, naming the host and port it dialled and carrying the two
commands below with them filled in; `wso2 doctor --online` reports the same
refusal from its issuer check. The operating system's trust store is the
ordinary answer. Where it cannot be changed, `WSO2_CA_FILE` names a PEM file
whose certificates the shell trusts beside the system roots, for every request
the shell itself makes:

```sh
openssl s_client -connect localhost:9443 -showcerts </dev/null 2>/dev/null \
  | awk '/BEGIN CERT/,/END CERT/' > localhost-9443.pem
export WSO2_CA_FILE=$PWD/localhost-9443.pem
```

The variable must be exported in the shell that runs `wso2`. It never narrows
trust. A file that cannot be read, or holds no certificate, is refused with
`shell.ca_file_unreadable` and exit class `64` before anything reaches the
network. A product module is a separate process; it is handed the variable and
applies the same trust on its own. See [Login errors](#login-errors) above for
the same commands beside the rest of the login refusals.

## What a module may ask the broker for

A module asks the shell for access by audience and scopes. The audience must
be one its receipt declares. A scope must be declared by the receipt, or be
recorded on the selected context's product entry for that module's
namespace: the entry is the user's own statement of what the shell may
request for that product, and a module that calls the user's API through a
product, such as a gateway, cannot know that API's permissions in advance.
A scope neither the receipt nor the entry names is refused with
`auth.scope_not_declared`. The product entry's scopes remain the ceiling
for every request, whichever party declared them. A request naming no
scopes at all asks for exactly the entry's recorded scopes, which is what
lets a module stop carrying a `--scope` flag on every command.

## What a module process can see

A product module runs with an environment built from nothing. Three things
are added back: `WSO2_CA_FILE`; `WSO2_NO_INPUT=1` when `--no-input` or
`WSO2_NO_INPUT` asked that nothing prompt; and every variable named
`WSO2_<NAMESPACE>_*` for that module's namespace, upper-cased: `WSO2_IAM_`
for `iam`. That is how a secret a command needs, such as an administrator
password for a one-time bootstrap, reaches the module: the user exports it
under the module's prefix and names it on the command line. A module never
sees another module's variables, and nothing else a CI runner exported. An
empty variable is not passed at all.

## Sample output

### Version

```text
$ wso2 version
WSO2 CLI          v0.1.0
Protocol          v2, v1
Platform          darwin/arm64

Installed modules
NAME          VERSION
api           v0.9.0
agent         v1.2.0
integration   v0.4.0
```

### Active context

```text
$ wso2 whoami
Context          cloud-us
Issuer           https://api.asgardeo.io/t/acme/oauth2/token
Organization     acme
User ID          jane@example.com
Session          present
Session expiry   2026-11-15T09:00:00Z
```

`Session expiry` reads `not stated by the issuer` when the identity provider
discloses no refresh-token lifetime, which is the common case and not an
error; it is never the access token's own, much shorter, expiry. With no
context selected, `wso2 whoami` says so and exits 0. With a context selected
but no stored session, it names `wso2 login` instead of a `Session expiry`
row. `--output json` renders the same facts as a JSON object.

### First login against a self-hosted issuer

```text
$ wso2 login --url https://idp.customer.example --client-id wso2-cli \
    --context customer

Logged in to the "customer" context.
User ID    ops
Email      ops@customer.example
Products   none configured

Created context "customer".
It is the first context, so it is now the selected one.

No products are configured for this context. A self-hosted deployment is not discoverable, so record each product's URL:

  wso2 context product add <product> --url <url> --context customer
```

The authorization URL is written to the diagnostic stream, not to this one: it
is an instruction to act on rather than the command's result, so a caller
redirecting standard output still sees it.

### Ending a session

```text
$ wso2 logout

Ended the session for the "cloud-us" context.
Context            cloud-us
Session            ended
Revocation         confirmed
Product sessions   none
Browser session    sign-out opened at https://api.asgardeo.io/t/acme/oauth2/token

The identity provider accepted the request to revoke this session's refresh token.

Each identity provider's sign-out page was opened in the browser, so the next login prompts for credentials again.
```

`Revocation` is `confirmed` when the identity provider accepted the request,
`not-attempted` when it publishes no revocation endpoint and so was never asked,
and `failed` when it was asked and did not accept, or could not be reached. The
shell-owned session is removed under all three, and the command succeeds under
all three; what changes is only what it claims. `--output json` renders the same
fields as a JSON object, which is the only way a script can read which of the
three happened. See
[ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md).

### Context

```text
$ wso2 context apply -f team-context.json --use local

Applied the context file.

CONTEXT   ACTION   CHANGES
local     create   -

Sessions ending: none

Next  Run `wso2 login`.

$ wso2 context list
CURRENT   CONTEXT   TYPE     ISSUER                  PRODUCTS       ORGANIZATION   PROJECT
*         local     onprem   http://localhost:8501   api,identity

$ wso2 context create staging --login-product identity --url https://idp.staging.example

Created the "staging" context.

Context         staging
Type            onprem
Login           oauth-browser
Issuer          https://idp.staging.example
Client ID       wso2-cli
Login product   identity
Products        identity
Organization
Project
Selected        no

Next  Run `wso2 context use staging`, then `wso2 login`. Add more products with `wso2 context product add <product> --url <url>`.
```

`wso2 context show` shows that same document whole, wherever it lives:

```text
$ wso2 context show
Path      /home/alex/.wso2/cli/contexts.json
Written   yes

Schema version: 4
Selected context: local

Contexts
CURRENT   NAME    TYPE     KIND            ISSUER                  LOGIN PRODUCT   CREDENTIAL SOURCE     ORGANIZATION   PROJECT
*         local   onprem   oauth-browser   http://localhost:8501   identity        secure store: local

Products
LOGIN   CONTEXT   RECORD        URL                     AUDIENCE                     SCOPES   GRANT
        local     api           http://localhost:9251   http://localhost:9251                 exchange
        local     api/gateway   http://localhost:9091   http://localhost:9091
*       local     identity      http://localhost:8501   https://localhost:8090/mcp   system
```

On a machine nothing is set up on yet, the path is still reported, naming
what would need to change (`WSO2_HOME`) to point somewhere else:

```text
$ wso2 context show
Path      /home/alex/.wso2/cli/contexts.json
Written   no

No context document has been written yet.

Run wso2 context apply -f <file> --use <name> with the file your platform team shares, or wso2 context create <name> --login-product <product> --url <url> --use.
```

### Module inventory

```text
$ wso2 product list
PRODUCT       INSTALLED   CHANNEL   UPDATE
agent         v1.2.0      stable    v1.3.0 available
api           v0.9.0      stable    current
integration   v0.4.0      —         pinned to v0.4.0
reference     —           stable    v0.1.0 to install

1 product has an update available. Run wso2 product update --all to take it.
1 product is current.
1 product is pinned and will not be updated.
1 product is not installed. Run wso2 product install reference to install it.
```

A product the catalog publishes that is not installed, such as `reference`
above, is a row of its own: INSTALLED shows `—`, CHANNEL the channel a plain
install would follow, and UPDATE the version that install would take. A product
published only on prerelease names that channel, and the install command
beneath the table adds `--channel prerelease`, since a plain install follows
stable.

CHANNEL names the channel a module follows for updates; it shows `—` for a
module installed at an exact version with no channel chosen, such as
`integration` above. A pin overrides the channel — it is what makes a pinned
prerelease installable without putting the module on that channel — so there
is no channel to name while the pin holds, and the blank cell is not a
prediction that the module would move to stable once unpinned: `wso2 product
install integration` without `@<version>` records whatever channel that
command names, stable by default, which need not be the channel the pinned
version actually came from.

Credentials are never shown by these commands. Tokens are stored separately in
the operating system's secure credential store.
