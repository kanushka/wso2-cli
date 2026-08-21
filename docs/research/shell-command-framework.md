# Shell command framework: own dispatcher versus Cobra

**Status:** Superseded in part by [ADR 0008](../adr/0008-cobra-for-the-shell-command-ui.md)  
**Date:** 2026-07-27  
**Scope:** How the `wso2` shell parses its own arguments, routes built-in
commands, and forwards product arguments to a module

## Executive recommendation

> **This recommendation has been acted on and partly reversed.** The shell now
> routes its own commands with Cobra; see
> [ADR 0008](../adr/0008-cobra-for-the-shell-command-ui.md) for the decision and
> its constraints. The trigger conditions below fired as written. What remains
> current here is the evidence: the unknown-flag passthrough analysis, and in
> particular why the allowlist option must never be used at the namespace
> boundary.

**Keep the shell's own dispatcher for the architecture proof. Adopt Cobra for
the shell when the root command surface grows, and keep the module-facing
argument contract parser-agnostic in both cases.**

The reasons are specific rather than stylistic:

- the shell currently owns two built-in commands and one flag, which is below
  the point where a command framework pays for itself;
- the shell's most load-bearing argument rule — that any flag the shell does
  not recognize belongs to the module — is the one rule Cobra does not support
  without an explicit design choice, and one of the obvious ways to configure
  Cobra for it **silently discards those flags**;
- the payoff is real but arrives later, with the ~30 root commands proposed in
  the [command reference](../reference/commands.md), merged help, and
  completion;
- Cobra remains a **P0 requirement on the module side** regardless of what the
  shell does ([product requirements](../product-requirements.md) §7.5), and
  nothing in the current design blocks it there.

This document records evidence and alternatives. It does not change
[product requirements](../product-requirements.md) or
[architecture](../architecture.md), and it is bounded by the
[first CLI vertical-slice plan](../plans/first-cli-vertical-slice.md).

## What the shell does today

The shell has no command-line framework. Its dispatcher is hand-written:

- `internal/app/app.go:59` — `builtins()` returns a slice of
  `{name, summary, run}`. It currently holds exactly two entries, `help` and
  `version`.
- `internal/app/app.go:81` — `dispatch()` linear-scans that slice, special-cases
  `--help`, `-h`, and `--version`, then falls through to `dispatchNamespace()`.
- `internal/app/app.go:164` — `help()` prints a hand-written usage line plus a
  table generated from the same slice.
- `internal/app/invoke.go:83` — `parseProductArgs()` recognizes `--output`,
  `-o`, and `--output=<mode>` in a hand-written loop, and nothing else.

The whole of it is roughly 30 lines of routing logic and 40 lines of flag
handling. Neither Cobra, `pflag`, nor the standard library's `flag` package is
imported by the shell; the root `go.mod` requires only
`google.golang.org/protobuf`.

### The rule that matters

`parseProductArgs` documents its own contract:

> The shell parses only what it owns. Everything after the first argument it
> does not recognize belongs to the module, so a module can add flags without
> the shell being released.

This is a genuine architectural commitment, not an implementation detail. It is
what lets a product team ship `wso2 api gateway list --new-flag` without a
shell release, which is the independent-release property that
[architecture](../architecture.md) §4.4 and §6.1 exist to protect. It is also
consistent with the SDK handing `Arguments []string` to a module **unparsed**,
so the module owns its own flags in whatever library it prefers.

Observed behaviour of the current parser, printed by calling it directly:

| Input after `wso2 api` | Command path | Forwarded to module | Shell mode |
| --- | --- | --- | --- |
| `gateway list --env prod` | `[gateway list]` | `[--env prod]` | `table` |
| `--output json gateway list --env prod` | `[gateway list]` | `[--env prod]` | `json` |
| `gateway list --output json --env prod` | `[gateway list]` | `[--env prod]` | `json` |
| `gateway list --env prod --output json` | `[gateway list]` | `[--env prod --output json]` | `table` |
| `gateway list --env=prod` | `[gateway list]` | `[--env=prod]` | `table` |

Row 4 is the known cost of the rule: once an unrecognized flag appears, a later
`--output` is treated as the module's, and the shell renders a table. This is
predictable and documented, but it means **flag position changes meaning**,
which is a usability wart the current design accepts deliberately.

## What Cobra provides

Cobra is a command-routing and help-generation layer over the `pflag` flag
parser. Measured against what the shell hand-writes today, it supplies:

| Capability | Currently hand-written or absent |
| --- | --- |
| `cobra.Command` tree and `AddCommand` routing | `builtins()` slice + linear scan |
| POSIX flag parsing via `pflag`: `--output json`, `-o json`, `-o=json`, combined shorthands | partially hand-written for one flag |
| Persistent flags inherited by every subcommand | absent; each command would re-declare `--context`, `--output`, `--no-input` |
| Generated help and usage from `Use`/`Short`/`Long`/`Example` | hand-written `help()` |
| Argument validators (`ExactArgs`, `NoArgs`, `ValidArgs`) | hand-written checks |
| Completion for bash, zsh, fish, and PowerShell, including dynamic `ValidArgsFunction` | absent; deferred by the slice plan §11 |
| "Did you mean" suggestions via Levenshtein distance (`command.go:782`, `command.go:868`) | absent |
| Command aliases | absent |
| Run hooks (`PersistentPreRunE` and siblings, `command.go:974`–`command.go:1038`) | absent; context/auth setup would be open-coded per command |
| A walkable command tree, and `cobra/doc` generation of man, Markdown, reST, and YAML | absent |

Dependency weight is modest and license-compatible. Cobra v1.10.2 is
Apache-2.0, matching this repository's own headers, and `go list -deps` on the
core package resolves to `github.com/spf13/pflag` only. The heavier
requirements in Cobra's `go.mod` — `go-md2man/v2` and `go.yaml.in/yaml/v3` —
are imported solely by `doc/yaml_docs.go` and `doc/man_docs.go`, so they enter
the module graph but are not linked unless the `cobra/doc` subpackage is used.

## The decisive constraint: unknown-flag passthrough

Cobra must be told what to do with a flag it does not know. There are four
strategies, and they are not equivalent. The table below is the observed output
of a probe program built against Cobra v1.10.2, with a persistent `--output`
flag on the root and an `api` namespace command.

| Input | Default | `FParseErrWhitelist{UnknownFlags: true}` | `DisableFlagParsing` | `--` terminator |
| --- | --- | --- | --- | --- |
| `api gateway list --env prod` | error: `unknown flag: --env` | args `[gateway list]`, **`--env prod` gone** | args `[gateway list --env prod]`, `--output` unparsed | n/a |
| `api gateway list --output json --env prod` | error | args `[gateway list]`, mode `json`, **`--env prod` gone** | args `[gateway list --output json --env prod]`, mode `table` | n/a |
| `api gateway list -- --env prod` | — | — | — | args `[gateway list --env prod]`, `ArgsLenAtDash`=2 → clean split |
| `api --output json -- gateway list --env prod` | — | — | — | mode `json`, module side `[gateway list --env prod]` |

### The allowlist option is unsafe here

`FParseErrWhitelist{UnknownFlags: true}` does not forward unknown flags. It
**drops them**, together with any following value. This is visible in `pflag`
v1.0.10 `flag.go`:

- `parseArgs` (`flag.go:1131`) appends a token to `f.args` only when it is not a
  flag, or when it follows the `--` terminator;
- on the allowlist path, `parseLongArg` (`flag.go:997`) and `parseShortArg`
  (`flag.go:1057`) return without ever appending the flag token;
- `stripUnknownFlagValue` (`flag.go:961`) additionally consumes the next
  argument when it does not begin with `-`, on the documented assumption that it
  was the unknown flag's value.

So `wso2 api gateway list --env prod` reaches the module as
`[gateway list]`, with no error and no diagnostic. A product command would run
against the wrong environment. **This option must not be used at the namespace
boundary.**

### The remaining two are a real trade

`DisableFlagParsing` forwards everything faithfully — but it forwards the
shell's own flags too. `--output json` is no longer parsed by the shell
(`command.go:964` substitutes the raw argument slice, and
`command.go:1181` skips required-flag validation). The shell would have to
re-scan the raw slice itself, which is `parseProductArgs` again, obtained at the
cost of a dependency.

The `--` terminator works cleanly in both directions: `pflag` records the split
point and `Flags().ArgsLenAtDash()` returns it, so the shell gets its flags and
the module gets its arguments verbatim. The cost is user-facing —
`wso2 api gateway list -- --env prod` instead of
`wso2 api gateway list --env prod` — and it is a worse experience than the CLIs
being migrated from.

## Comparison

### Keeping the shell's own dispatcher

**For**

- The passthrough rule is expressed directly, in about 40 lines, and is exactly
  what the architecture requires.
- No third-party code sits on the path between user input and module launch,
  which is the path the slice plan asks to be proven closed.
- Built-in precedence over module namespaces
  ([product requirements](../product-requirements.md) §7.1) is a two-line
  property of the dispatch order rather than an interaction between Cobra's
  command lookup and a dynamically registered namespace set.
- No dependency, no version policy, no supply-chain surface for the shell
  binary.

**Against**

- Everything in the Cobra capability table is work the project would write and
  test itself.
- Help output is hand-maintained and will drift from the command set.
- Completion, aliases, suggestions, and doc generation are each substantial to
  build well; completion in particular is per-shell and easy to get subtly
  wrong.
- Flag position changes meaning, as row 4 of the current-behaviour table shows.
- A hand-rolled parser accretes edge cases (`-o=json`, combined shorthands,
  repeated flags, negative numbers as values) that `pflag` already handles.

### Adopting Cobra in the shell

**For**

- All of the capability table arrives at once, tested and widely used.
- The command tree is **introspectable**, which is what
  [architecture](../architecture.md) §5.2 requires when it says module command
  metadata must be generated from the actual command tree and that authors
  "must not maintain a separate handwritten help schema". Walking a
  `*cobra.Command` is how that is satisfied; a hand-rolled tree would need its
  own equivalent.
- The same tree feeds merged help, completion, and `cobra/doc` output for the
  documentation site.
- It is the library every identified WSO2 product CLI already uses
  ([public CLI inventory](public-wso2-cli-inventory.md)), so shell and module
  code would share idioms and reviewer familiarity.
- Apache-2.0, and the core package pulls in only `pflag`.

**Against**

- The namespace boundary requires an explicit and non-obvious configuration
  choice, one variant of which loses user input silently.
- Namespaces are discovered from receipts at runtime, so commands must be
  registered before `Execute()` on every invocation. That is supported, but it
  means the module store is read even for `wso2 --help`.
- Cobra's unknown-command path produces its own error and suggestions
  (`command.go:782`); routing an unregistered first argument to the module store
  instead requires registering namespaces up front rather than intercepting the
  failure.
- A dependency on the shell's argument path is a supply-chain consideration for
  a binary whose entire premise is verified execution.

## Where Cobra actually shines

Cobra's value is not in dispatch. It is concentrated in four places, none of
which the current slice reaches:

**1. A large flat-ish command surface.** The proposed
[command reference](../reference/commands.md) lists about 30 root commands in
seven groups — `login`/`logout`/`whoami`, `org`, `context`, `config`, `module`,
`bundle`, and `doctor` — several with flags (`module install <module>@<version>`,
`module update --all`, `module install --file`). Hand-rolling routing, help, and
flag parsing for that is where the cost becomes obvious. Two commands is not.

**2. Persistent flags.** `--context`, `--output`, `--no-input`, `--quiet`, and
`--verbose` are described by [architecture](../architecture.md) §4.5 as common
to everything. Declaring them once on the root and inheriting them is precisely
what `PersistentFlags` is for; the alternative is re-declaring them on 30
commands and keeping them consistent by review.

**3. Generated, introspectable help.** This is the strongest case. §5.2's
"generated from its actual command tree" requirement, merged shell-plus-module
help, coding-agent-consumable command metadata
([product requirements](../product-requirements.md) §7.4 P1), and completion all
want the same thing: a tree you can walk. Cobra provides the tree, the walk, and
four shells' worth of completion.

**4. The module side, which is already required.**
[Product requirements](../product-requirements.md) §7.5 makes "the SDK
integrates naturally with Cobra" a **P0**, because the pilot migrations are
Cobra CLIs and [architecture](../architecture.md) §12 selects them by their
Cobra shape. Nothing here is in tension with that: the SDK's `Request.Arguments`
is deliberately unparsed, so a module may use Cobra, `flag`, or anything else.
The reference module currently uses the standard library's `flag` only for a
test-only `--module-info` switch, and its `status` handler ignores
`Request.Arguments` entirely.

## Where Cobra does not help

Cobra is a front end. It contributes nothing to the parts of this system that
the vertical slice exists to prove:

- the length-delimited Protobuf transport and framing
  ([ADR 0002](../adr/0002-module-transport.md));
- receipt resolution, path containment, and pre-launch digest verification;
- the authentication broker and token issuance;
- typed `Result` and `Problem` values and their category-to-exit-class mapping
  ([ADR 0003](../adr/0003-shell-owned-output.md));
- shell-owned table and JSON rendering.

Adopting Cobra would not remove or simplify any of these. This is why its
absence from the slice plan is a scoping decision rather than an omission.

## Recommendation and trigger

Keep the current dispatcher through the architecture proof. Revisit when the
first of these becomes true:

1. the shell owns more than roughly five built-in commands, or any built-in
   needs more than two flags;
2. merged shell-plus-module help or completion moves from deferred to in scope;
3. a second shell-owned persistent flag beyond `--output` is added.

Condition 2 is the real trigger. Conditions 1 and 3 are early warnings.

Whichever way the shell goes, two properties should be preserved explicitly
because they are what makes the choice reversible:

- the module receives its arguments **unparsed**, so the shell's parser and the
  module's parser stay independent;
- the shell's own flags are recognized at a **defined position**, so a later
  framework change cannot silently alter which flags the shell consumes.

## Decisions requiring agreement

> **All four are now settled.** 1 keeps positional passthrough, with the
> `--` terminator rejected as a permanent user-facing tax; the position wart is
> fixed instead by having modules declare their command trees. 2 accepts the
> current behaviour until that declaration exists. 3 is yes, bounded by
> [ADR 0008](../adr/0008-cobra-for-the-shell-command-ui.md) to `cobra` and
> `pflag` with the documentation generator excluded and the linked set asserted
> by a test. 4 lands with the module that declares its own tree, rather than
> with the shell. The original wording is kept below because the reasoning that
> made them open is still the reasoning behind the answers.


1. **Flag-passthrough syntax.** Retain positional passthrough
   (`wso2 api gateway list --env prod`, first-unknown-flag wins) or require the
   `--` terminator (`wso2 api gateway list -- --env prod`)? This is a
   user-facing contract and should be settled before a public namespace ships,
   independently of the framework question. The current behaviour is an
   implementation fact, not a ratified decision.
2. **`--output` after an unknown flag.** Row 4 of the current-behaviour table
   forwards `--output json` to the module and renders a table. Accept, reject
   with a usage problem, or resolve by adopting the `--` terminator?
3. **Whether the shell binary may take third-party dependencies on its argument
   path**, given the verified-execution premise.
4. **Owner and timing of the merged help tree**, since that is the trigger
   condition above.

## Sources

Library behaviour was verified against source in the local Go module cache and
by executing a probe program, not from documentation:

- Cobra v1.10.2 `command.go` — `TraverseChildren` (l. 229), `DisableFlagParsing`
  (l. 241, use sites l. 964 and l. 1181), `DisableSuggestions` and
  `SuggestionsMinimumDistance` (l. 253–259, l. 782, l. 868), run hooks
  (l. 974–1038); `go.mod` and `LICENSE.txt`.
  <https://github.com/spf13/cobra>
- pflag v1.0.10 `flag.go` — `ParseErrorsAllowlist` (l. 140–172),
  `stripUnknownFlagValue` (l. 961), `parseLongArg` (l. 980, allowlist branch
  l. 997), `parseShortArg` (l. 1116, allowlist branch l. 1057), `parseArgs` and
  the `--` terminator (l. 1131), `ArgsLenAtDash` (l. 427), `interspersed`
  default (l. 1271). <https://github.com/spf13/pflag>
- This repository — `internal/app/app.go`, `internal/app/invoke.go`,
  `modules/reference/cmd/wso2-module-reference/main.go`, `go.mod`.
