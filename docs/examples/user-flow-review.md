# User flow review — recorded session

**Status:** Observation record. Not a specification.
**Date:** 2026-09-02. Supersedes the 2026-09-01 recording, which is in git
history and is quoted in [#147](https://github.com/wso2/wso2-cli/issues/147).
**Shell under test:** `make build-shell` at `main` (417537c), reporting
`v1.0.0-dev`, protocol v2 and v1, on `darwin/arm64`.
**Module under test:** `reference` installed from this checkout with
`make install-module NAMESPACE=reference`, reporting `v0.0.0-dev`.

> **The module was not at 417537c.** The `status`/`call` split recorded in
> [section 7](#7-product-commands) is uncommitted work in the tree, and
> `main.go` changed twice while this session was being recorded. Every product
> block below was re-run after the last change and against a freshly installed
> module; the shell blocks are 417537c exactly. Anyone reproducing this needs
> that working tree, not the commit.

Every block below is a command that was actually typed against that build and
the output it actually produced, with the process exit code. Nothing here is
illustrative: where the output is surprising, it is reproduced as-is and the
surprise is named in [Findings](#findings).

The machine had two contexts (`kanushka-dev`, `local-ci`) and two identities.
`kanushka-dev` carried a live Asgardeo session throughout, established by a
human in a browser on 2026-09-01 and deliberately left in place. The
no-session paths are therefore recorded against `local-ci`, which has no
session and no credential, rather than by logging out.

Three fixes landed between the two recordings — `fc85700` (F1), `006c66e` (F2),
and `f700f7a` (F5). A fourth change, the reference module's `status`/`call`
split, is in the working tree and not yet committed. Each finding below carries
what this run found.

## How to read this

- `$ wso2 …` is the command. `exit=N` is the process exit status.
- Exit codes seen: `0` success, `64` usage problem, `75` product service
  problem, `77` authentication required.
- Output is verbatim. Trailing spaces on empty table cells are trimmed.

---

## 1. First contact

```
$ wso2 --version
WSO2 CLI   v1.0.0-dev
Protocol   v2, v1
Platform   darwin/arm64

Installed modules
NAME        VERSION      PLATFORM
reference   v0.0.0-dev   darwin/arm64
exit=0
```

```
$ wso2 --help
Usage: wso2 <command> [arguments]

Shell commands
   config        Show and change shell preferences.
   context       Create, select, and list the targets commands run against.
   doctor        Check the shell's context, secure-store, and session health.
   help          Show the shell command tree.
   identity      Record and inspect what an identity reaches.
   login         Log in, creating the identity and context when an issuer is named.
   logout        End the selected context's session.
   module        Install, list, and update product modules from the module catalog.
   org           Show and change the organization the selected context runs within.
   version       Show the shell, protocol, and installed module versions.
   whoami        Show who is signed in, and to what context, identity, and session.

Flags
      --context string   Use the named context instead of the selected one.
  -h, --help             Show help for a command.
  -o, --output string    Render results as table or json. (default "table")
      --verbose          Write diagnostics about what the shell attempted to stderr.

Product commands are provided by installed modules.
exit=0
```

`wso2 help` prints a byte-identical tree. The closing line still promises
product commands and names none, with `reference` installed. See
[F3](#f3-help-never-names-an-installed-modules-commands--open).

```
$ wso2 bogus
error: "bogus" is not a shell command and no installed module owns that namespace (shell.unknown_command)
  Run wso2 help to see the shell commands, or wso2 version to see the installed modules.
exit=64
```

## 2. Where am I pointed

```
$ wso2 context list
CURRENT   CONTEXT        IDENTITY         ORGANIZATION   PROJECT
*         kanushka-dev   kanushka-cloud   kanushka
          local-ci       local-machine
exit=0
```

```
$ wso2 whoami
Context          kanushka-dev
Identity         kanushka-cloud
Organization     kanushka
Subject          24e39564-859a-4063-bcea-28471ce5cd1d
Session          present
Session expiry   not stated by the issuer
exit=0
```

```
$ wso2 context current
Context        kanushka-dev
Identity       kanushka-cloud
Organization   kanushka
Project
exit=0
```

```
$ wso2 identity list
IDENTITY         TYPE     ISSUER                                            PRODUCT     ENDPOINT                  SCOPES
kanushka-cloud   cloud    https://api.asgardeo.io/t/kanushka/oauth2/token   reference   https://api.asgardeo.io   reference:status:read,reference:status:write
local-machine    onprem   https://localhost:9443/oauth2/token               reference   https://localhost:9443    reference:status:read
exit=0
```

The other context has no session, and says so without treating it as an error:

```
$ wso2 whoami --context local-ci
Context          local-ci
Identity         local-machine
Organization
Subject
Session          none
Session expiry
Recovery         Run wso2 login to establish a session for this context.
exit=0
```

Naming a context that does not exist fails without guessing, from both the
read and the write side:

```
$ wso2 whoami --context nosuchcontext
error: no context named "nosuchcontext" is configured (contexts.unknown_context)
  Select a configured context, or remove the context document to run without one.
exit=64
```

```
$ wso2 context use nosuchcontext
error: no context named "nosuchcontext" is configured (contexts.unknown_context)
  Select a configured context, or remove the context document to run without one.
exit=64
```

## 3. Health check

With a session, `doctor` is three passes and the `RECOVERY` column empties:

```
$ wso2 doctor
CHECK          STATUS   DETAIL                                             RECOVERY
context        pass     the context document is valid
secure-store   pass     the OS secure store is reachable
session        pass     a stored session exists for the selected context
exit=0
```

Against the context that has none, the table renders first and the failing
check is then re-stated as the process error, so a human sees the whole report
and a script gets a non-zero status:

```
$ wso2 --context local-ci doctor
CHECK          STATUS   DETAIL                                                    RECOVERY
context        pass     the context document is valid
secure-store   pass     the OS secure store is reachable
session        fail     no stored login session exists for the selected context   Run wso2 login to establish a session for this context.
error: no stored login session exists for the selected context (auth.login_required)
  Run wso2 login to establish a session for this context.
exit=77
```

`--output json` carries the same structure:

```
$ wso2 doctor -o json
{
  "checks": [
    {
      "check": "context",
      "status": "pass",
      "detail": "the context document is valid"
    },
    {
      "check": "secure-store",
      "status": "pass",
      "detail": "the OS secure store is reachable"
    },
    {
      "check": "session",
      "status": "pass",
      "detail": "a stored session exists for the selected context"
    }
  ]
}
exit=0
```

## 4. Preferences

A bare family command refuses and enumerates its subcommands in the recovery
line rather than dumping a help tree:

```
$ wso2 config
error: wso2 config needs a subcommand (shell.missing_argument)
  Run wso2 config list to show every preference, wso2 config get <key> to show one, or wso2 config set <key> <value> to change one.
exit=64
```

```
$ wso2 org
error: wso2 org needs a subcommand (shell.missing_argument)
  Run wso2 org current to show the organization the selected context runs within, or wso2 org use <organization> to change it.
exit=64
```

Both families do have subcommands, and `--help` lists them:

```
$ wso2 config --help
Usage: wso2 config <subcommand>

Subcommands: list, get, set.

Shell commands
   get           Show one shell preference.
   list          Show every shell preference in the closed key set.
   set           Change one shell preference.

Flags
  -h, --help            Show help for a command.
  -o, --output string   Render results as table or json. (default "table")
      --verbose         Write diagnostics about what the shell attempted to stderr.
exit=0
```

A second reader of this document read the two blocks above in the opposite
order and concluded that `config` and `org` were unimplemented stubs. See
[F8](#f8-a-bare-family-command-reports-a-usage-error-and-reads-as-an-unimplemented-command).

```
$ wso2 config list
KEY              VALUE   SET
output                   no
catalog-origin           no
exit=0
```

```
$ wso2 config get output
Key     output
Value
Set     no
exit=0
```

```
$ wso2 org current
Context        kanushka-dev
Organization   kanushka
exit=0
```

A subcommand that needs arguments names them and counts what it got:

```
$ wso2 identity add-product
error: wso2 identity add-product needs an identity and a product namespace, got 0 (shell.missing_argument)
  Run wso2 identity add-product <identity> <namespace> --endpoint <url> [--audience <resource-id>] [--scopes <list>] [--replace].
exit=64
```

## 5. Modules

```
$ wso2 module list
MODULE      INSTALLED    CHANNEL   UPDATE
reference   v0.0.0-dev   —         pinned to v0.0.0-dev

Every installed module is current.
exit=0
```

The closing line contradicts the row above it, which is
[#143](https://github.com/wso2/wso2-cli/issues/143), reproduced again here.

```
$ wso2 module available
MODULE      CHANNEL      VERSION
reference   prerelease   v0.1.0-rc.4

Run wso2 module install <module> to install one.
exit=0
```

`module update` now says why the pinned module is left alone, instead of
reasoning about a stable channel the catalog does not publish:

```
$ wso2 module update --all --dry-run
reference is pinned to v0.0.0-dev and would not be updated.

Nothing was changed. Run without --dry-run to apply this.
exit=0
```

An unknown module name is distinguished from a network failure explicitly:

```
$ wso2 module install nosuchmodule
error: no module named "nosuchmodule" is published in the module catalog (catalog.unknown_module)
  Check the module name. This is not a network failure: the catalog was read and names no such module.
exit=64
```

Flags are scoped to the subcommand that can act on them:

```
$ wso2 module available --channel stable
error: unknown flag: --channel (shell.unknown_flag)
  Run wso2 module available.
exit=64
```

```
$ wso2 module list --all
error: unknown flag: --all (shell.unknown_flag)
  Run wso2 module list.
exit=64
```

## 6. Installing the module from this checkout

```
$ make install-module NAMESPACE=reference
Installed reference v0.0.0-dev for darwin/arm64 into /Users/…/.wso2/cli/modules.
It was installed by the ordinary installer from a catalog served at http://127.0.0.1:56736 for the length of this run.
The version is pinned, so wso2 module update leaves this build alone.

Confirm it is installed:
  ./bin/wso2 version
Take it off again:
  ./bin/wso2 module remove reference
```

Both closing commands work. The previous recording ended with
`Run it: ./bin/wso2 reference --help`, which did not.

## 7. Product commands

**`wso2 reference status` succeeds.** This is the first rendered success
result observed outside the acceptance suite:

```
$ wso2 reference status
MODULE                 CONTEXT        ACCESS    AUDIENCE           SCOPES                  EXPIRES
reference v0.0.0-dev   kanushka-dev   granted   reference-status   reference:status:read   2026-09-02T07:04:40Z
exit=0
```

```
$ wso2 reference status -o json
{
  "module": "reference v0.0.0-dev",
  "context": "kanushka-dev",
  "access": "granted",
  "audience": "reference-status",
  "scopes": "reference:status:read",
  "expiresAt": "2026-09-02T07:04:52Z"
}
exit=0
```

It works because `status` no longer calls the product service. It reports what
the module is and what the shell granted it, and calls nothing
(`modules/reference/cmd/wso2-module-reference/main.go:146`). The
service-backed proof moved to a new `wso2 reference call`, which is not
mentioned anywhere in the previous recording.

```
$ wso2 reference call
error: the reference status service at https://api.asgardeo.io/status answered with something this module cannot read (reference.status_unavailable)
  Check that this endpoint is a reference status service; it is recorded for the reference product in wso2 identity list. Retrying will not change this answer.
exit=75
```

The message now names the URL and withdraws the retry advice. The exit code is
still `75`. See [F5](#f5-a-permanent-failure-is-named-honestly-and-still-exits-75--partly-fixed).

```
$ wso2 reference whoami
error: the reference status service at https://api.asgardeo.io/whoami answered with something this module cannot read (reference.status_unavailable)
  Check that this endpoint is a reference status service; it is recorded for the reference product in wso2 identity list. Retrying will not change this answer.
exit=75
```

A refusal is reported by `status` as a field with exit `0`, and by `call` as a
problem with exit `77`, against the same context:

```
$ wso2 --context local-ci reference status
MODULE                 CONTEXT    ACCESS    REASON                                                          RECOVERY
reference v0.0.0-dev   local-ci   refused   the credential source the "local-ci" context names is not set   Set the credential source this context names, then retry the command.
exit=0
```

```
$ wso2 --context local-ci reference call
error: the credential source the "local-ci" context names is not set (auth.credential_unavailable)
  Set WSO2_LOCAL_CI_SECRET to the client secret for this context, then retry the command.
exit=77
```

The split is deliberate and documented in the handler: `status` exists to
answer "did the broker grant anything?", and a command that cannot say "no"
cannot answer it. `call` keeps the ordinary exit-class contract. The
`WSO2_LOCAL_CI_SECRET` recovery is a better answer than the previous
recording's, which named no variable.

The namespace itself still has no help, and an unknown command names the full
path rather than the leaf:

```
$ wso2 reference --help
error: the "reference" module does not implement "reference" (reference.unknown_command)
  Run wso2 help to see the available commands.
exit=64
```

```
$ wso2 reference bogus
error: the "reference" module does not implement "reference bogus" (reference.unknown_command)
  Run wso2 help to see the available commands.
exit=64
```

```
$ wso2 reference status --help
error: pflag: help requested (module.flag_invalid)
  Run wso2 reference status --help to see the flags this command accepts.
exit=64
```

## 8. Flags, help, and machine-readable output

The help a command prints now matches the flags it accepts, and a refusal
names the command typed rather than its family:

```
$ wso2 identity list --context local-ci
error: wso2 identity list does not take the flag --context (shell.unsupported_flag)
  Run wso2 identity list --help to see the flags it accepts.
exit=64
```

```
$ wso2 identity list --help
Usage: wso2 identity list [flags]

Flags
  -h, --help            Show help for a command.
  -o, --output string   Render results as table or json. (default "table")
      --verbose         Write diagnostics about what the shell attempted to stderr.
exit=0
```

`--context` is gone from that block, so the loop the previous recording found
is closed. `version` and `login` no longer advertise `--output` either:

```
$ wso2 version --help
Usage: wso2 version

Flags
  -h, --help      Show help for a command.
      --verbose   Write diagnostics about what the shell attempted to stderr.
exit=0
```

```
$ wso2 login --help
Usage: wso2 login

Flags
      --client-id string   Present this registered OAuth application. Required with --url.
      --context string     Use the named context instead of the selected one.
  -h, --help               Show help for a command.
      --no-input           Refuse rather than prompt, open a browser, or wait for a human.
      --url string         Log in against this issuer, creating the identity and context it authenticates.
      --verbose            Write diagnostics about what the shell attempted to stderr.
exit=0
```

`-o json` still works on `whoami`, `identity list`, `doctor`, `config list`,
and the `context` family:

```
$ wso2 context list -o json
{
  "contexts": [
    {
      "name": "kanushka-dev",
      "identity": "kanushka-cloud",
      "organization": "kanushka",
      "project": "",
      "selected": true
    },
    {
      "name": "local-ci",
      "identity": "local-machine",
      "organization": "",
      "project": "",
      "selected": false
    }
  ]
}
exit=0
```

It is refused on the whole `module` family and on `version`, as before. But the
long and short spellings of the same flag now produce different errors:

```
$ wso2 module list --output json
error: wso2 module list does not take the flag --output (shell.unsupported_flag)
  Run wso2 module list --help to see the flags it accepts.
exit=64
```

```
$ wso2 module list -o json
error: unknown shorthand flag: 'o' in -o (shell.unknown_flag)
  Run wso2 module list.
exit=64
```

See [F7](#f7--o-and---output-are-refused-with-different-errors-and-one-of-them-is-pflags).

## 9. Diagnostics

`--verbose` writes to stderr and leaves stdout clean:

```
$ wso2 whoami --verbose
time=2026-09-02T11:30:22.337+05:30 level=DEBUG msg="the shell started" command=whoami shell_version=1.0.0-dev platform=darwin/arm64 output_mode=table
Context          kanushka-dev
Identity         kanushka-cloud
…
exit=0
```

The product-command trace now carries the invocation id, the context, and the
organization, and continues past brokering to a rendered result:

```
$ wso2 reference status --verbose
… msg="the shell started" command=wso2 shell_version=1.0.0-dev platform=darwin/arm64 output_mode=table
… msg="resolved a product namespace" namespace=reference \
  executable=/Users/…/.wso2/cli/modules/reference/versions/0.0.0-dev/wso2-module-reference \
  module_version=0.0.0-dev protocol_version=2
… msg="brokering module access" namespace=reference invocation_id=3cb21893d0e20e635057743582d72af2 \
  context=kanushka-dev organization=kanushka grant_kind=oauth-browser \
  declared_audiences=reference-status declared_scopes=reference:status:read narrowing=scoped-refresh
MODULE                 CONTEXT        ACCESS    AUDIENCE           SCOPES                  EXPIRES
reference v0.0.0-dev   kanushka-dev   granted   reference-status   reference:status:read   2026-09-02T07:04:54Z
exit=0
```

The trace still stops at "brokering module access" and says nothing about the
HTTP call `call` then makes, so a failing `call` is still undiagnosable from
`--verbose` alone.

---

## Findings

### F1. Help and flag enforcement disagreed — fixed

Fixed in `fc85700`. Each built-in declares its own flags, so `--help` renders
the set the command enforces, and the refusal names the command typed:
`wso2 identity list`, not `wso2 identity`. Verified on `config`, `context`,
`org`, `identity`, `login`, `version`, and every `module` subcommand.

Two residues, both new and both small: [F7](#f7--o-and---output-are-refused-with-different-errors-and-one-of-them-is-pflags),
and the usage line is now inconsistent. Every nested subcommand renders
`[flags]` and no top-level command does, because `DisableFlagsInUseLine` is set
on the root, on each family command, and on the top-level leaves, but never on
a family's subcommands:

```
Usage: wso2 config list [flags]      Usage: wso2 doctor
Usage: wso2 module available [flags] Usage: wso2 version
Usage: wso2 org current [flags]      Usage: wso2 login
```

### F2. The installer recommended a command that fails — fixed

Fixed in `006c66e`. `make install-module` now closes with `./bin/wso2 version`
and `./bin/wso2 module remove reference`. Both work.

### F3. `help` never names an installed module's commands — open

Unchanged. `wso2 help` still ends with "Product commands are provided by
installed modules." and names none, with `reference` installed. `wso2 version`
remains the only place an installed module is visible, and it shows versions,
not commands.

Tracked by [#86](https://github.com/wso2/wso2-cli/issues/86), which
[#85](https://github.com/wso2/wso2-cli/issues/85) accepted explicitly as the
cost of not registering namespaces as Cobra commands.

### F4. `--help` on a module command is reported as an error — open

Unchanged, and now cheaper to reproduce because `status` succeeds:

```
$ wso2 reference status --help
error: pflag: help requested (module.flag_invalid)
  Run wso2 reference status --help to see the flags this command accepts.
exit=64
```

Asking for documentation exits 64 and says "error", and the recovery is the
command that just failed. `cobratree.invoke` calls `ParseFlags`, pflag answers
`--help` with its `ErrHelp` sentinel, and `flagProblem` classifies that as
`module.flag_invalid`.

Deferred to [#86](https://github.com/wso2/wso2-cli/issues/86): there is nowhere
for a module to put prose today, and a declared command tree lets the shell
answer help itself, launching nothing.

### F5. A permanent failure is named honestly, and still exits 75 — partly fixed

Fixed in `f700f7a`, in two of three parts. The endpoint is named and the retry
advice is withdrawn:

```
$ wso2 reference call
error: the reference status service at https://api.asgardeo.io/status answered with something this module cannot read (reference.status_unavailable)
  Check that this endpoint is a reference status service; it is recorded for the reference product in wso2 identity list. Retrying will not change this answer.
exit=75
```

The third part was not taken. Exit `75` is `EX_TEMPFAIL`, and the message
directly above it says retrying will not change the answer. A CI job that
retries on 75 still retries forever, and now does so against text telling it
not to. The message and the exit code disagree, which is worse than the
previous state where they agreed and were both wrong.

The re-categorisation question the fix plan raised is still open.

### F6. The reference module cannot be run outside the acceptance harness

> **Fixed 2026-09-02.** `wso2 reference status` now answers from the invocation
> alone and calls nothing, so it works with nothing deployed. The service-backed
> handler survives as `wso2 reference call`, which is what still proves a
> brokered token is accepted at the declared audience.

The service the reference module talked to existed only as
`internal/statusservice`, constructed in-process by `test/acceptance` and
deliberately unreachable from the shell binary. So `wso2 reference status` had
no endpoint it could succeed against on a developer's machine, and #113 closed
on "a developer can build their own module from the checkout and run it under
the real `wso2` shell" with the run half dead-ending.

### F7. `-o` and `--output` are refused with different errors, and one of them is pflag's

New, and introduced by the F1 fix in this branch. A command that does not accept
`--output` no longer declares the flag at all, so the long spelling reaches the
shell's own refusal and the short one never gets there — it fails inside pflag
first:

```
$ wso2 module list --output json
error: wso2 module list does not take the flag --output (shell.unsupported_flag)
  Run wso2 module list --help to see the flags it accepts.
exit=64

$ wso2 module list -o json
error: unknown shorthand flag: 'o' in -o (shell.unknown_flag)
  Run wso2 module list.
exit=64
```

Same request, two problem codes, and the second leaks the parser's vocabulary
into user-facing text. Its recovery is weaker too — it names the command with no
way to find out what the command does accept:

```
$ wso2 version -o json
error: unknown shorthand flag: 'o' in -o (shell.unknown_flag)
  Run wso2 help to see the shell commands and the flags they accept.
exit=64
```

That points at the whole shell tree where the long form points at
`wso2 version --help`. Reproduces on `version`, `login`, and every `module`
subcommand.

This does not reopen F1's design, which is right: the defect is that a flag the
command does not declare has two ways of being refused, and only one of them is
the shell's.

**Suggested shape:** declare the refused flag hidden and reject it in the same
place the long form is rejected, or map pflag's unknown-shorthand error onto the
`shell.unsupported_flag` problem the long form already produces.

### F8. A bare family command reports a usage error, and reads as an unimplemented command

> **Fixed 2026-09-02** by [#148](https://github.com/wso2/wso2-cli/pull/148),
> which landed in `main` before this branch. A bare family name now prints that
> family's help on standard output and exits 0. An unknown subcommand is still
> refused with `shell.unknown_command` and the usage exit class, so #133's
> guarantee is untouched.

`wso2 config` and `wso2 org` exited 64 with the word "error", and named their
subcommands only in the recovery line:

```
$ wso2 config
error: wso2 config needs a subcommand (shell.missing_argument)
  Run wso2 config list to show every preference, wso2 config get <key> to show one, or wso2 config set <key> <value> to change one.
exit=64
```

A second reader of this document took those blocks as evidence that `config` and
`org` had no subcommands, and proposed hiding both commands from the tree until
subcommands were implemented. They are fully implemented, and `--help` lists
them. What misled was the framing: a non-zero exit and a leading "error:" for
what is not a failure but an incomplete command.

The recovery text was genuinely good, and read as an afterthought under a line
that had already said the command was broken. `git remote`, `git stash`, and
`kubectl config` all print their subcommand list on stdout at exit 0 for the
bare form.

The fix folded each family's recovery sentence into its `Long`, so it is still
the first thing printed and `wso2 config` and `wso2 config --help` now agree.
All five families — `config`, `org`, `context`, `identity`, `module` — share one
helper, so they cannot drift apart.

### Minor: a JSON request gets a plain-text error

Unchanged. `-o json` is honored for the result and not for the failure,
consistently across the shell, so it reads as deliberate. Noting it again
because a caller parsing stdout still has to fall back to the exit code, and
the dotted problem code most useful to a script is the part left unparseable.

## What worked well

- **The F1 fix is complete and verifiable.** Every command's help now matches
  what it enforces, and refusals name the command typed.
- **`wso2 reference status` succeeds by hand.** The whole chain — session,
  namespace resolution, receipt reading, brokering, process launch, protocol v2
  frames, and a rendered success result — is now exercisable without `go test`.
- **Splitting `status` from `call`** is the right call: the command a newcomer
  types first needs nothing deployed, and the service-backed contract still has
  a command that proves it.
- **Reporting an access refusal as a field** lets `wso2 reference status`
  answer its own question when the answer is "no", and the handler documents
  why it is the one command exempt from the auth exit class.
- **Error text.** Every failure carries a stable dotted code, a plain sentence,
  and a recovery naming a command — F7's shorthand path excepted.
- **The `WSO2_LOCAL_CI_SECRET` recovery** names the exact variable to set.
- **Exit codes are distinct and correct** — 64 usage, 75 product service,
  77 authentication — with F5's classification the open question.
- **`whoami` and `doctor`** remain the clearest surfaces in the CLI: empty
  fields are not errors, and the next command to type is always stated.
- **`doctor` renders its full report *and* exits non-zero.**
- **`--verbose` keeps diagnostics on stderr**, and now carries the invocation
  id, context, and organization.
- **`module update --dry-run` explains the pin** instead of reasoning about a
  channel the catalog does not publish.
- **`Session expiry   not stated by the issuer`** — a field that says it does
  not know, rather than blank or invented.

## Not covered

- `wso2 login` was not re-run. The session from 2026-09-01 was live throughout
  and left in place, so the login report, the authorize URL, and the
  first-login path are unchanged from the previous recording rather than
  re-observed.
- `wso2 logout`, and the selected context in a no-session state. Recording
  either means ending the session and asking a human for a browser login to
  restore it.
- `wso2 org use`, `wso2 context create`, and `wso2 config set` past their
  `--help` — all mutate state that the rest of this recording depends on.
- `wso2 login --url` creating a new identity and context.
- The device-code and client-credentials grants.
- A successful `wso2 reference call`. Blocked by F6.

## Reproducing this

```
make build-shell
make install-module NAMESPACE=reference
./bin/wso2 <command>
```

The module store at `~/.wso2/cli/modules` holds `reference v0.0.0-dev` from
this checkout, pinned. `./bin/wso2 module remove reference` takes it off.

The shell blocks reproduce at 417537c. The product blocks need the uncommitted
`status`/`call` split in the working tree, and `make install-module` must be
re-run after any change to it — the store holds a built binary, not the source,
so an edit made after installing is invisible until the next install. This
recording was invalidated once by exactly that.
