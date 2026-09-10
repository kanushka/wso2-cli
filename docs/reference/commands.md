# WSO2 CLI shell commands

**Status:** Proposed reference
**Related:** [Product requirements](../product-requirements.md),
[architecture](../architecture.md)

This reference describes the proposed shared `wso2` shell commands. It does not
represent an available production interface. Product operations belong to
product modules, such as `wso2 api`, `wso2 identity`, `wso2 integration`, and
`wso2 agent`.

The distinction between catalog refresh and module binary update remains an
open decision. The lifecycle command names below are therefore provisional.

These are built: `wso2 context create <name>`, `wso2 context use <context>`,
`wso2 context list`, `wso2 context current`, `wso2 context show`,
`wso2 account create <name>`, `wso2 account add-product <account>
<namespace>`, `wso2 account remove-product <account> <namespace>`,
`wso2 account rename <account> <new-name>`, `wso2 account list`,
`wso2 <namespace> connect <url>`,
`wso2 login`,
`wso2 login --url <issuer> --client-id <id>`, `wso2 login --only <namespace>`,
`wso2 login --no-products`, `wso2 logout`, `wso2 whoami`, `wso2 doctor`,
`wso2 product available`, `wso2 product list`,
`wso2 product install <product>`, `wso2 product install <product>@<version>`,
`wso2 product install <product> --channel <channel>`,
`wso2 product update <product>`, `wso2 product update --all`,
`wso2 product remove <product>`, `wso2 config list`, `wso2 config get <key>`,
`wso2 config set <key> <value>`, `wso2 org current`, and `wso2 org use
<organization>`. The
[module catalog](module-catalog.md) reference describes what they select, what
they verify, how a channel and a pin are recorded per module, and how each
refusal is reported.

## Commands

| Command | Description |
| --- | --- |
| `wso2 help` | Shows the root command tree and help for a command. |
| `wso2 version` | Shows the shell, protocol, and installed module versions. |
| `wso2 login` | Establishes the login session, then one session per further product the account records, each through the same browser sign-on. `--only <namespace>` authorizes just that product — both of its records when it holds a gateway record, or one record alone as `--only <namespace>/gateway` — refused with `shell.invalid_argument` when the account records no such namespace; `--no-products` authorizes the login session alone, leaving product and gateway sessions unestablished; the two together are refused with `shell.conflicting_arguments`. The report lists every session this run established, naming the strategy that reached it (`direct`, `sibling`, `derived`, or `federated`) as `<strategy>, established`; a gateway record is its own line under its key, as `apim/gateway  sibling, established`. When a later product fails after earlier ones already succeeded, the failure names what was established and points at `wso2 login --only <namespace>` (or `--only <namespace>/gateway`) to retry only the one that was not, keeping the sessions already stored. |
| `wso2 login --url <issuer> --client-id <id>` | Logs in against a named issuer and creates the account and the context it authenticated, reporting both names. `--context <name>` names them both. Without it, an account that already authenticates against that issuer with that client ID is reused; otherwise login asks `Account name [account-1]:`, offering the next free `account-N`, where Enter accepts the default and a name that is not legal or is already taken is asked again. When standard input is not a terminal the default is taken without asking, and the report says the name was assigned, naming `--context <name>` and `wso2 account rename`. An account named by `--context` whose issuer and client ID both match is reused; one that differs in either is refused with `contexts.identity_exists` and never replaced. The first context created becomes the selected one. Nothing is written unless the login succeeded. Omitting `--client-id` prompts in an interactive terminal and is refused with `shell.missing_required_flag` under `--no-input`. |
| `wso2 logout` | Ends every session the account the selected context names holds — the login session and each product's own — asking the identity provider to revoke each refresh token and removing the shell-owned entry that every context sharing that credential reference reaches. `Product sessions` in the report names what happened to each session beyond the login one, `<namespace> ended` or `<namespace> none`. A client-credentials account holds no session to end and reports that plainly, exiting 0. Ending the shell's sessions leaves each identity provider's own browser session in place, and a later login would then be silent; so logout also opens each provider's end-session page, once per provider and client, naming the client and the identity token the session recorded, and reports `Browser session` as `sign-out opened`, `sign-out printed` (no browser could be opened; the URLs are on standard error), `kept` (`--keep-browser-session`, `--no-input` or `WSO2_NO_INPUT`), or `unaffected` (nothing was stored). Ending the browser session is best effort like revocation: the page is opened and its outcome is not observable from the shell. |
| `wso2 whoami` | Built today: shows the selected context, the account it authenticates as, the organization, the session's subject, and the session's own state, all read from local state with no network call. With no context selected it says so and exits 0. With a context selected but no stored session it says so and names `wso2 login`. A stored session is reported present with its expiry either as the issuer's disclosed refresh-token lifetime or, when the issuer disclosed none, as not stated — never as the shorter-lived access token's own expiry. A disclosed lifetime that has passed is reported expired. A session stored before this field existed reports its subject as unknown rather than blank. It also lists every record the account holds, each with the strategy that reaches it (`direct`, `sibling`, `derived`, `federated`, or `inline` for a client-credentials account) and whether that record's own session is stored: every product under its namespace, and a product's gateway record under its key, as `apim/gateway: sibling, present` beside `apim: federated, present`. A client-credentials account reports its own state as `inline` too, with no recovery text, since it holds nothing to recover. |
| `wso2 org` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `current` and `use`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 org list` | Deferred (#112): no control-plane endpoint for enumerating organizations exists yet, and listing would need an access token before an organization is chosen, which the auth broker refuses. Typing it is refused as an unknown `wso2 org` subcommand, naming `current` and `use` as what the family supports. |
| `wso2 org use <organization>` | Built today: sets the `Organization` field on the selected context through `contexts.Update`, and names which context it edited. The auth broker binds a minted token to a context's `Organization` and refuses when it is empty, so a session already signed in under the previous organization no longer matches — the command warns about that on standard error, in both renderings. It writes nothing, and does not migrate, invalidate, or re-mint a session. Refused with `shell.no_context_configured` when no context exists to edit, and with `auth.organization_switch_unsupported`, naming the provider, when the selected context's account authenticates against ThunderID or Identity Server, which have no organization to switch to: the auth broker would refuse every later command with the same code, so the value is refused at the flag instead. An empty value is allowed on those providers, because it clears the field rather than switching to an organization, and this command is the only writer of it: refusing that too would leave a context that already carries an organization unrepairable. |
| `wso2 org current` | Built today: shows the organization the selected context runs within, read from `contexts.Context.Organization`. With no context configured it reports that state and exits 0, worded exactly as `wso2 context current`. A configured context with no organization set says so distinctly, rather than reporting the same blank field either state would otherwise share. |
| `wso2 context` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `create`, `current`, `list`, `show` and `use`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 context create <name>` | Creates a context naming an account with `--account`, and optionally an organization and project with `--organization` and `--project`. It writes no credential and makes no network call, so an unreachable issuer or a misspelled organization is reported by the command that needs it rather than here. Creating a context whose name is taken is refused. The first context created becomes the selected one. |
| `wso2 context list` | Lists saved cloud and on-premises contexts, marking the selected one. |
| `wso2 context use <context>` | Selects the context used by default for later commands. |
| `wso2 context current` | Shows the active context. |
| `wso2 context show` | Built today: reports where the context document lives — inside the shell's state root, which `WSO2_HOME` overrides — and shows it whole: the schema version, the default context, every account it declares, and every context. The path is reported whether or not a document has been written there yet; `written: false` (JSON) or "No context document has been written yet" (table) says so rather than the command failing. Every account is rendered with its credential *source* — a secure-store reference or the environment variable a client-credentials account reads its secret from — never a credential value, which `contexts.Account` has nowhere to hold. It makes no network call and reads nothing from the secure store. |
| `wso2 account` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `create`, `add-product`, `remove-product`, `rename` and `list`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 account create <name> --issuer <url> --client-id <id>` | Declares an account and a same-named context without logging in, and selects the context when none is selected. `--client-secret-variable <VAR>` makes it a client-credentials account that reads its secret from that variable at use; otherwise it is a browser account whose credential reference is its own name. `--provider` names `asgardeo`, `identity-server` or `thunder`, which decides how the shell narrows access. `--product <namespace> --endpoint <url> [--audience <uri>] [--scope <s>]...` records one product at the same time; the four belong together and are refused apart with `shell.conflicting_arguments`, and a Thunder account's product needs `--audience` because its login is bound to that resource. It is what a product module's bootstrap prints for you to run next. Nothing is written to the secure store and no network call is made. A name already declared is refused with `contexts.account_exists`. The output ends with the command to run next: `wso2 login` for a browser account, the product's `status` for a CI account. |
| `wso2 account add-product <account> <namespace>` | Records a product the account reaches, with `--endpoint`, and optionally `--audience` and a comma-separated `--scopes`. It modifies an account `wso2 login` wrote and creates no account and no context: logging in is the only thing that creates an account. Nothing is written to the secure store and no network call is made, so the record is an assertion that the login's session reaches the product, checked by the first command that needs it. A namespace the account already records is refused with `contexts.product_exists`; `--replace` overwrites it, replacing the whole record rather than merging with it. An endpoint embedding user information is refused, and the rejected value is not echoed. `--grant jwt-bearer` or `--grant federated`, given with `--grant-issuer` and `--grant-client-id`, records a product reached at its own issuer instead of directly from the login session: a jwt-bearer grant presents an identity token from the login session to that issuer, narrowed by a comma-separated `--grant-scopes` (jwt-bearer only, refused with `shell.invalid_argument` under `federated`). Those assertion scopes decide which claims the identity token carries, so they decide what the product can map a role from: a product that reads groups from the assertion grants nothing at all when the scope carrying them was not among them, which looks from the command like the deployment refusing the user; a federated grant signs in at the product's own issuer as the named public client, through the same browser sign-on. `--grant-resource` names the resource indicator the grant's own session carries, when the issuer it runs at requires one. `--grant` without both `--grant-issuer` and `--grant-client-id` is refused with `shell.missing_required_flag`; a grant needs `--audience` too, the value the derived access is proved against. |
| `wso2 account remove-product <account> <namespace>` | Stops the account reaching a product. It drops the product's record, and the product's gateway record with it; `<namespace>/gateway` drops the gateway record alone and keeps the product. A record the account does not hold is refused with `contexts.unknown_product`, naming every record it does. Before the record goes, every session the removal leaves nothing to reach is ended the way `wso2 logout` ends one: its refresh token is revoked at the issuer, best effort as [ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md) decides, and its entry is deleted from the OS secure store. That has to come first, because `wso2 logout` finds a product's session through its record and the secure store cannot be listed, so a session whose record is gone could never be ended by the shell again. A sibling, federated or derived product holds such a session, and so does a gateway record that is not reached through the login session. A direct product shares the login session, which is kept, because the account still logs in through it. An exchanged product, and every product of a client-credentials account, holds no session, so nothing is ended. Removing the product the account logs in through is refused with `contexts.login_product` before anything is touched: the login session was authorized for that product, and with it gone the session would answer for a product the account no longer records. No command changes an account's login product, so the recovery names creating an account that logs in through the other product with `wso2 account create` and `wso2 login --context`, not `wso2 login` alone. The login product's gateway record is a separate record and can be removed on its own. A removal the document would refuse, such as taking the last product from an account whose deployment binds its login to one, is refused with `shell.invalid_argument` before any session is ended. The report names the records removed, each session ended with its revocation outcome, and what became of the login session; `--output json` renders the same facts. No browser is opened, so the identity providers' browser sessions are left in place. |
| `wso2 account rename <account> <new-name>` | Renames an account, and points every context that used the old name at the new one. The account's credential reference is left as it was, so no secure-store entry moves: the store cannot list its entries, and one moved and lost could never be ended. Context names are left as they are too, so a same-named context keeps the old name, and the report says so and names `wso2 context create <name> --account <new-name>` for a handle under the new one. An account that does not exist is refused with `contexts.unknown_identity`, a new name another account holds with `contexts.identity_exists`, and a name that is not lower-case letters, digits and hyphens starting with a letter with `shell.invalid_argument`. `--output json` renders the old name, the new one, and the contexts that now use it. Nothing is written to the secure store and no network call is made. |
| `wso2 account list` | Lists the accounts and, for each, the products it reaches. It names no credential and reads nothing from the secure store. |
| `wso2 <namespace> connect <url>` | Records the product a module serves at a URL, from the **product descriptor** the module's receipt carries (`capabilities.product` in its manifest): the issuer derived from the URL, the audience, the scopes its commands need, and the grant it is reached by. It is the shell's own subcommand of every installed namespace, read before the module is launched, so the module never sees it; nothing is written to the secure store and no network call is made. A product whose descriptor names an identity provider (`identity` on ThunderID) creates the account, with a same-named context selected when none is, and pins it as the account's login product; when the selected account already authenticates against that issuer the product is recorded on it instead. `--account <name>` names a new account, and a name already taken on another issuer is refused with `contexts.identity_exists`. Without it, connect asks `Account name [account-1]:`, offering the next free `account-N`, where Enter accepts the default and a name that is not legal or is already taken is asked again; under `--no-input`, `WSO2_NO_INPUT`, or a standard input that is not a terminal the default is taken without asking, and the report says the name was assigned, naming `--account <name>` and `wso2 account rename`. `--client-id <id> --client-secret-variable <VAR>` creates a client-credentials account for a pipeline instead. A product whose descriptor names no provider attaches to the selected account under the descriptor's grant. An `exchange` grant (`api` on the WSO2 API Platform) records no issuer or client of its own, because the exchange runs at the account's issuer as the account's client, so `--client-id`, `--client-id-variable` and `--client-secret-variable` are refused with `shell.conflicting_arguments`; its audience is the descriptor's default, else the URL connect was given, unless `--audience` names another. A `federated` or `jwt-bearer` grant needs `--client-id` when the descriptor names no default client. With several accounts `--login-provider <issuer-url>` picks one, and with none it is refused with `shell.login_provider_required`, naming the `connect` of every installed module whose product is a login provider (`wso2 account create` when none is). `--login-provider` naming an issuer no account authenticates against is refused the same way whichever kind of product it is, rather than quietly creating an account the flag did not ask for. On a client-credentials account, what a product accepts is the descriptor's `machine` list: a product declaring `credential` needs its own, `--client-id <id> --client-secret-variable <VAR>` and `--client-id-variable <VAR>` when the id is held in the environment too, and is refused with `auth.product_not_configured` without them; a product declaring `inline` is minted from the account's own machine client and refuses those flags; a product declaring neither is refused for the account kind. A `--client-secret-variable` or `--client-id-variable` value that is not a variable name is refused at the flag with `shell.invalid_argument`, before the document is opened. `--audience` and `--scopes` override the descriptor's defaults; a recorded product is refused with `contexts.product_exists` unless `--replace`. A module whose receipt carries no descriptor has `connect` refused with `shell.connect_unsupported`, naming `wso2 account add-product`. `--gateway` records the product's **gateway record** instead: the URL is the gateway endpoint, `--audience` the API's own resource identifier (defaulting to the gateway URL when the product is reached by exchange, required otherwise when the descriptor's `gateway` block binds by resource or the account's deployment binds access by resource, and defaulting to the account's client when neither does) and `--scopes` the API's own permissions, written beside the product's own record on the account that already records the product — the account found the way a non-provider product's `connect` finds it — and refused with `shell.product_required`, naming `wso2 <namespace> connect <management-url>`, when no account records the product yet. The gateway is reached at the account's login provider as the account's own client, so `--client-id`, `--client-id-variable` and `--client-secret-variable` are refused with `shell.conflicting_arguments`; a descriptor without a `gateway` block has `--gateway` refused with `shell.connect_unsupported`, naming the product; on a client-credentials account the gateway is minted inline from the account's machine client when the block's `machine` list allows `inline`, the only machine strategy a gateway can name since it is reached from the account's own client, and refused with `auth.product_not_configured` otherwise. The report adds `Record  gateway` and the strategy the shell derived (`exchanged` when the product is, `direct` when the gateway's resource and scopes equal the login session's, else `sibling`, or `inline`); the next line is `wso2 login --only <namespace>` when the account already holds its login session and the product's own, else `wso2 login`. For a product or gateway reached by exchange, an account that already holds its login session has nothing left to authorize, and the next line is `wso2 <namespace> --help` instead. A recorded gateway is refused with `contexts.product_exists` unless `--replace`. |
| `wso2 config` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `list`, `get` and `set`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 config list` | Built today: shows every key in the closed set of shell preferences — the default output mode and the catalog origin override — and whether each is currently configured. |
| `wso2 config get <key>` | Built today: shows one shell preference. `key` must be one of `output`, `catalog-origin`; any other value is refused with `config.unknown_key`, naming the valid keys. |
| `wso2 config set <key> <value>` | Built today: changes one shell preference. An unknown key is refused the same way `config get` refuses one; a value a key does not accept is refused with `config.invalid_value`, naming what is acceptable (`table` or `json` for `output`; an absolute http or https URL for `catalog-origin`). Each preference is the lowest-precedence source for what it governs: `--output` wins over a configured output mode, and `WSO2_CLI_CATALOG_ORIGIN` wins over a configured catalog origin — a saved preference can never override either. `output` governs exactly the commands that accept `--output`: `wso2 whoami`, `wso2 doctor`, `wso2 logout`, and the `context`, `account`, `config` and `org` families. It does not reach `wso2 version` or the `product` family, which render fixed prose and refuse `--output` with `shell.unsupported_flag`; a preference that silently did nothing for them would be a worse contract than a flag refused out loud. A colour preference is not in this set: `output.ColorEnabled` has no production caller yet, so a key that claimed to govern colour would change nothing observable; it is the obvious first key to add once something renders in colour. |
| `wso2 update` | Applies the approved installation-channel policy for root shell updates. |
| `wso2 module` | With no subcommand, prints this family's help on standard output and exits 0: naming a family without a subcommand is an incomplete command, not a failed one, and every subcommand it names works. The help names `available`, `install`, `list`, `remove` and `update`. A subcommand the family does not have is a different case and is still refused with `shell.unknown_command` and the usage exit class (#133), so a typo is never reported to a script as success. |
| `wso2 product available` | Lists official modules the module catalog publishes. |
| `wso2 product install <product>` | Installs the latest compatible stable release of a module. |
| `wso2 product install <product>@<version>` | Installs an exact compatible module version. |
| `wso2 product list` | Lists installed modules, versions, and update availability. |
| `wso2 product info <module>` | Shows catalog, compatibility, and installation information. |
| `wso2 product update <product>` | Updates one product module. Naming a module is already an explicit target, so this does not prompt; `--dry-run` still reports what it would do without changing anything. A module the catalog publishes no version of on its followed channel — withdrawn, renamed, or moved to a channel this install no longer follows — is reported by name rather than called current, since the catalog cannot say whether the installed version is current when it does not publish the module at all; this does not change the exit status. |
| `wso2 product update --all` | Built today: updates every installed product module that has a newer version on its followed channel, skipping a pinned one. Being unbounded, it prompts for confirmation before moving anything; `--yes` skips the prompt, `--dry-run` reports what it would do without changing anything, and `--no-input` (or `WSO2_NO_INPUT`) refuses rather than prompt. Refuses `shell.non_interactive` when it may not prompt and `--yes` was not given — either because `--no-input` or `WSO2_NO_INPUT` asked that nothing prompt, or because standard input is not a terminal; the refusal names the control that fired and offers the one way out that applies to it, and `shell.conflicting_arguments` for `--yes` with `--dry-run`. A module the catalog publishes no version of on its followed channel — withdrawn, renamed, or moved to a channel this install no longer follows — is reported by name rather than called current, since the catalog cannot say whether the installed version is current when it does not publish the module at all; this does not change the exit status, so a scheduled `--all` run does not start failing the moment one module goes unpublished upstream. |
| `wso2 product verify <module>` | Verifies an installed module and its receipt. |
| `wso2 product rollback <module>` | Reactivates a retained compatible version. |
| `wso2 product remove <product>` | Built today: removes one installed module, leaving configuration and credentials alone. Prompts for confirmation after confirming the module is installed; `--yes` skips the prompt, `--dry-run` reports what it would remove without removing anything, and `--no-input` (or `WSO2_NO_INPUT`) refuses rather than prompt. Refuses `shell.module_not_installed` before any prompt when the module is not installed, `shell.non_interactive` when it may not prompt and `--yes` was not given — either because `--no-input` or `WSO2_NO_INPUT` asked that nothing prompt, or because standard input is not a terminal; the refusal names the control that fired and offers the one way out that applies to it, and `shell.conflicting_arguments` for `--yes` with `--dry-run`. |
| `wso2 product install --file <module.wso2module>` | Installs one module from an offline file. |
| `wso2 bundle create` | Creates a platform-specific, self-installing offline bundle from catalog releases. |
| `wso2 bundle inspect <file>` | Shows bundle contents without installing it. |
| `wso2 bundle install <file>` | Imports a bundle when the WSO2 CLI is already installed. |
| `wso2 doctor` | Built today: checks that the context document is valid, that the OS secure store is reachable, and that the selected context's account has a stored session. The context check's own detail names the document it checked — its path inside the shell's state root, which `WSO2_HOME` overrides — whichever way the check comes out; `wso2 context show` shows that same document whole. The session check covers the login record and every product record the account holds, a product's gateway record among them under its key (`apim/gateway`). It reports none, with the `wso2 login` pointer in its recovery column, when a record has no stored session at all and names the ones without one, because being logged out is the state a completed `wso2 logout` leaves behind, not a health fault; a session that is stored but cannot be read still fails. `--online` adds two more checks: that the OpenID configuration of every issuer the selected context's account names can be read — the check that finds a certificate this machine does not trust, refused as `auth.certificate_untrusted` with the same recovery a product command gives, or `auth.discovery_failed` for any other reason — and module catalog reachability; without it, `wso2 doctor` makes no network call. On an unconfigured machine, the secure-store and session checks report not-applicable rather than failure; on a context document that fails to decode or validate, the session check reports not-applicable too, because no credential reference can be resolved from it, while the secure-store check still runs since it never reads the document. The session check is also not-applicable for a client-credentials account, which acquires access inline and holds no session to check. Exits 0 when every check passes, is not-applicable, or reports none, otherwise the exit class of the most severe failing check, in this rank: secure-store, then the document, then the session, then (only under `--online`) the issuer, then the catalog — a rank this command defines and not the numeric order of the exit classes those checks carry. Receipt, module integrity, compatibility, and protocol status are not built yet; see [architecture](../architecture.md#14-operational-behavior-and-recovery). |

`project` commands are intentionally not included yet. Product-specific
projects, deployment, and runtime operations remain within their product
modules.

On a fresh air-gapped machine, the user runs the transferred platform-specific
offline bundle directly; no `wso2` command exists yet. `wso2 bundle install`
is only for a machine where the shell is already installed. The administrator
must establish trust in the bootstrap before execution through platform signing
on Windows or macOS, or detached-signature verification on Linux.

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
misconfigured account rather than hang until its own timeout.

A browser or device login under `--no-input` is refused with
`auth.non_interactive` and exit class `77`. Automation authenticates with a
client-credentials account instead, which acquires access inline with no login
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
applies the same trust on its own. The troubleshooting entry in the
[login guide](../guides/login.md#authcertificate_untrusted) has the same
commands.

## What a module may ask the broker for

A module asks the shell for access by audience and scopes. The audience must
be one its receipt declares. A scope must be declared by the receipt, or be
recorded on the selected account's product entry for that module's
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

### Active account

```text
$ wso2 whoami
Context          cloud-us
Account         acme-cloud
Organization     acme
Subject          jane@example.com
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
Subject    ops
Email      ops@customer.example
Products   none configured

Created account "customer" and context "customer".
It is the first context, so it is now the selected one.

No products are configured for this account. A self-hosted deployment is not
discoverable, so each product's endpoint has to be recorded:

  wso2 account add-product customer <namespace> \
      --endpoint <url> --audience <resource-id> --scopes <list>
```

The authorization URL is written to the diagnostic stream, not to this one: it
is an instruction to act on rather than the command's result, so a caller
redirecting standard output still sees it.

### Ending a session

```text
$ wso2 logout

Ended the session for the "cloud-us" context.
Context      cloud-us
Account     acme-cloud
Session      ended
Revocation   confirmed
Shared with  cloud-us

The identity provider accepted the request to revoke this session's refresh token.

A browser single-sign-on session at the identity provider is unaffected by this
command, so a later login may not prompt for credentials.
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
$ wso2 context create cloud-us --account acme-cloud --organization acme

Created the "cloud-us" context.
Context        cloud-us
Account       acme-cloud
Organization   acme
Project
Selected       yes

It is the first context, so it is now the selected one. Run wso2 context use
<name> to select another.

$ wso2 context list
CURRENT   CONTEXT    IDENTITY     ORGANIZATION   PROJECT
*         cloud-us   acme-cloud   acme

$ wso2 context current
Context        cloud-us
Account       acme-cloud
Organization   acme
Project
```

An account is created by `wso2 login`, not by a command of its own, so
`--account` names one that already exists and a name that does not is refused
with `contexts.unknown_account`.

`wso2 context show` shows that same document whole, wherever it lives:

```text
$ wso2 context show
Path      /home/alex/.wso2/cli/contexts.json
Written   yes

Schema version: 3
Default context: cloud-us

Accounts
NAME         TYPE    KIND            ISSUER                CREDENTIAL SOURCE          PRODUCTS
acme-cloud   cloud   oauth-browser   https://idp.example   secure store: acme-cloud   reference

Contexts
DEFAULT   NAME       ACCOUNT      ORGANIZATION   PROJECT
*         cloud-us   acme-cloud   acme
```

On a machine nobody has logged in on yet, the path is still reported, naming
what would need to change (`WSO2_HOME`) to point somewhere else:

```text
$ wso2 context show
Path      /home/alex/.wso2/cli/contexts.json
Written   no

No context document has been written yet.

Run wso2 context create <name> --account <account> [--organization <name>]
[--project <name>].
```

### Module inventory

```text
$ wso2 product list
MODULE        INSTALLED   CHANNEL   UPDATE
api           v0.9.0      stable    current
agent         v1.2.0      stable    v1.3.0 available
integration   v0.4.0      —         pinned to v0.4.0

1 module(s) have an update available. Run wso2 product update --all to take them.
```

CHANNEL names the channel a module follows for updates; it shows `—` for a
module installed at an exact version with no channel chosen, such as
`integration` above. A pin overrides the channel — it is what makes a pinned
prerelease installable without putting the module on that channel — so there
is no channel to name while the pin holds, and the blank cell is not a
prediction that the module would move to stable once unpinned: `wso2 module
install integration` without `@<version>` records whatever channel that
command names, stable by default, which need not be the channel the pinned
version actually came from.

Credentials are never shown by these commands. Tokens are stored separately in
the operating system's secure credential store.
