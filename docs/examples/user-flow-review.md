# User flow review — recorded session

**Status:** Observation record. Not a specification.
**Date:** 2026-09-01
**Shell under test:** `make build-shell` at `main` (7039b10), reporting
`v1.0.0-dev`, protocol v2 and v1, on `darwin/arm64`.
**Module under test:** `reference` installed from this checkout with
`make install-module NAMESPACE=reference`, reporting `v0.0.0-dev`.

Every block below is a command that was actually typed against that build and
the output it actually produced, with the process exit code. Nothing here is
illustrative: where the output is surprising, it is reproduced as-is and the
surprise is named in [Findings](#findings).

The machine already had two contexts (`kanushka-dev`, `local-ci`) and two
identities configured. Sections 1–9 were recorded with **no login session**;
section 10 repeats the ones that change after a real `wso2 login` against
Asgardeo, completed by a human in a browser.

## How to read this

- `$ wso2 …` is the command. `exit=N` is the process exit status.
- Exit codes seen: `0` success, `64` usage problem, `77` authentication
  required.
- Output is verbatim, including trailing prose lines and blank lines.

---

## 1. First contact

A user who has just installed the shell types the bare command or asks its
version.

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

`wso2 help` prints the identical tree. Note the closing line: it promises
product commands but names none, even with a module installed. See
[F3](#f3-help-never-names-an-installed-modules-commands).

An unknown command is answered precisely, and points at both places a command
could have come from:

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
Subject
Session          none
Session expiry
Recovery         Run wso2 login to establish a session for this context.
exit=0
```

`whoami` is the strongest moment in the whole surface: the empty fields are not
errors, and the `Recovery` row says exactly what to type next.

```
$ wso2 identity list
IDENTITY         TYPE     ISSUER                                            PRODUCT     ENDPOINT                  SCOPES
kanushka-cloud   cloud    https://api.asgardeo.io/t/kanushka/oauth2/token   reference   https://api.asgardeo.io   reference:status:read,reference:status:write
local-machine    onprem   https://localhost:9443/oauth2/token               reference   https://localhost:9443    reference:status:read
exit=0
```

Naming another context works, and naming one that does not exist fails without
guessing:

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

```
$ wso2 whoami --context nosuchcontext
error: no context named "nosuchcontext" is configured (contexts.unknown_context)
  Select a configured context, or remove the context document to run without one.
exit=64
```

## 3. Health check

```
$ wso2 doctor
CHECK          STATUS   DETAIL                                                    RECOVERY
context        pass     the context document is valid
secure-store   pass     the OS secure store is reachable
session        fail     no stored login session exists for the selected context   Run wso2 login to establish a session for this context.
error: no stored login session exists for the selected context (auth.login_required)
  Run wso2 login to establish a session for this context.
exit=77
```

The table renders first and the failing check is then re-stated as the process
error, so a human sees the whole report and a script gets a non-zero status.
`--output json` carries the same structure, with `recovery` present only on the
failing check:

```
$ wso2 doctor -o json
{
  "checks": [
    { "check": "context",      "status": "pass", "detail": "the context document is valid" },
    { "check": "secure-store", "status": "pass", "detail": "the OS secure store is reachable" },
    { "check": "session",      "status": "fail", "detail": "no stored login session exists for the selected context",
      "recovery": "Run wso2 login to establish a session for this context." }
  ]
}
error: no stored login session exists for the selected context (auth.login_required)
  Run wso2 login to establish a session for this context.
exit=77
```

*(JSON reformatted to fit the page; field names and values are verbatim.)*

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
$ wso2 config list
KEY              VALUE   SET
output                   no
catalog-origin           no
exit=0
```

`wso2 org` behaves the same way:

```
$ wso2 org
error: wso2 org needs a subcommand (shell.missing_argument)
  Run wso2 org current to show the organization the selected context runs within, or wso2 org use <organization> to change it.
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

```
$ wso2 module available
MODULE      CHANNEL      VERSION
reference   prerelease   v0.1.0-rc.4

Run wso2 module install <module> to install one.
exit=0
```

```
$ wso2 module update --all --dry-run
The catalog publishes no version of reference on the stable channel, so whether v1.0.0 is up to date is unknown. Run wso2 module available to see what it publishes.

Nothing was changed. Run without --dry-run to apply this.
exit=0
```

*(Run before the local reinstall, against the previously installed `v1.0.0`.)*

An unknown module name is distinguished from a network failure explicitly,
which is the right distinction to draw:

```
$ wso2 module install nosuchmodule
error: no module named "nosuchmodule" is published in the module catalog (catalog.unknown_module)
  Check the module name. This is not a network failure: the catalog was read and names no such module.
exit=64
```

Flags are scoped to the subcommand that can act on them. `--channel` belongs to
`install`, `--all` to `update`, and asking for either on the wrong subcommand is
refused against that subcommand's own name:

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

## 6. Product commands

Installing the module from this checkout ends by telling the user what to run:

```
Installed reference v0.0.0-dev for darwin/arm64 into /Users/…/.wso2/cli/modules.
It was installed by the ordinary installer from a catalog served at http://127.0.0.1:58239 for the length of this run.
The version is pinned, so wso2 module update leaves this build alone.

Run it:
  ./bin/wso2 reference --help
```

That exact command does not work:

```
$ wso2 reference --help
error: the "reference" module does not implement "reference" (reference.unknown_command)
  Run wso2 help to see the available commands.
exit=64
```

See [F2](#f2-a-modules-namespace-has-no-help-and-the-installer-recommends-the-command-that-proves-it).

Before logging in, the module's commands resolve and then stop at the session
boundary, before the module process is launched:

```
$ wso2 reference status --verbose
… msg="resolved a product namespace" namespace=reference \
  executable=/Users/…/.wso2/cli/modules/reference/versions/0.0.0-dev/wso2-module-reference \
  module_version=0.0.0-dev protocol_version=2
… msg="brokering module access" namespace=reference grant_kind=oauth-browser \
  declared_audiences=reference-status declared_scopes=reference:status:read narrowing=scoped-refresh
error: no stored login session exists for the selected context (auth.login_required)
  Run wso2 login to establish a session for this context.
exit=77
```

Section 10 records what happens once a session exists.

Asking for a command the module does not have names the full path, not just the
leaf:

```
$ wso2 reference bogus
error: the "reference" module does not implement "reference bogus" (reference.unknown_command)
  Run wso2 help to see the available commands.
exit=64
```

But help for a real module command is also refused for want of a session:

```
$ wso2 reference status --help
error: no stored login session exists for the selected context (auth.login_required)
  Run wso2 login to establish a session for this context.
exit=77
```

See [F4](#f4---help-on-a-module-command-requires-a-login).

## 7. Login

A real browser login against Asgardeo, completed by a human:

```
$ wso2 login
Open this URL to log in:
https://api.asgardeo.io/t/kanushka/oauth2/authorize?client_id=…&code_challenge=…
  &code_challenge_method=S256&nonce=…&redirect_uri=http%3A%2F%2F127.0.0.1%3A10425%2Fcallback
  &response_type=code&scope=openid+offline_access+reference%3Astatus%3Aread+reference%3Astatus%3Awrite
  &state=…

Logged in to the "kanushka-dev" context.
Subject        24e39564-859a-4063-bcea-28471ce5cd1d
Organization   kanushka
Products       reference
exit=0
```

*(URL wrapped and its opaque values elided. `client_id` is a public PKCE
client, and the challenge, nonce, and state are single-use.)*

The URL is printed **and** the browser is opened
(`internal/auth/oauthflow/login.go:201` then `:207`), so the flow works over
SSH or from a detached process where opening a browser fails silently. The
report afterwards names the subject, the organization, and which products the
session reaches — all three are what the next command depends on.

The requested scope is exactly the union of what the identity declares for the
`reference` product; nothing broader was asked for.

```
$ wso2 login --help
Usage: wso2 login

Flags
      --client-id string   Present this registered OAuth application. Required with --url.
      --context string     Use the named context instead of the selected one.
  -h, --help               Show help for a command.
      --no-input           Refuse rather than prompt, open a browser, or wait for a human.
  -o, --output string      Render results as table or json. (default "table")
      --url string         Log in against this issuer, creating the identity and context it authenticates.
      --verbose            Write diagnostics about what the shell attempted to stderr.
exit=0
```

This block advertises `-o/--output`, which `wso2 login` refuses:

```
$ wso2 login --output json --no-input
error: wso2 login does not take the flag --output (shell.unsupported_flag)
  Run wso2 login --help to see the flags it accepts.
exit=64
```

See [F1](#f1-every---help-advertises-four-flags-the-command-may-refuse).

## 8. Machine-readable output

`-o json` works on `whoami`, `identity list`, `doctor`, `config list`, and the
`context` family:

```
$ wso2 context list -o json
{
  "contexts": [
    { "name": "kanushka-dev", "identity": "kanushka-cloud", "organization": "kanushka", "project": "", "selected": true },
    { "name": "local-ci",     "identity": "local-machine",  "organization": "",         "project": "", "selected": false }
  ]
}
exit=0
```

It is refused on the whole `module` family and on `version`:

```
$ wso2 module list -o json
error: wso2 module does not take the flag --output (shell.unsupported_flag)
  Run wso2 module --help to see the flags it accepts.
exit=64
```

```
$ wso2 version --output json
error: wso2 version does not take the flag --output (shell.unsupported_flag)
  Run wso2 version --help to see the flags it accepts.
exit=64
```

The refusal is deliberate and reasoned in
`internal/app/command.go:375` (`shellFlagsFor`) — module lifecycle output is
prose, not a schema. The problem is only that nothing the user can read says so
before they type it.

## 9. Diagnostics

`--verbose` writes to stderr and leaves stdout clean:

```
$ wso2 whoami --verbose
time=2026-09-01T16:44:52.573+05:30 level=DEBUG msg="the shell started" command=whoami shell_version=1.0.0-dev platform=darwin/arm64 output_mode=table
Context          kanushka-dev
Identity         kanushka-cloud
…
exit=0
```

---

## 10. After a real login

Every shell surface that reported a missing session now reports a live one.

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

`Session expiry   not stated by the issuer` is the right answer to give: the
field is not blank and not invented.

```
$ wso2 doctor
CHECK          STATUS   DETAIL                                             RECOVERY
context        pass     the context document is valid
secure-store   pass     the OS secure store is reachable
session        pass     a stored session exists for the selected context
exit=0
```

`doctor` goes to three passes and exit 0, and the `RECOVERY` column empties.

```
$ wso2 org current
Context        kanushka-dev
Organization   kanushka
exit=0
```

### The product command still fails

```
$ wso2 reference status
error: the reference status service answered with something this module cannot read (reference.status_unavailable)
  Retry the command. Report the failure if it persists.
exit=75
```

`wso2 reference whoami` and `wso2 reference status -o json` fail identically.

**This is progress, not success.** Compare the verbose trace with the
pre-login one in section 6: brokering now completes, the module process is
launched, and the error carries the module's own `reference.` prefix. The whole
chain — session, namespace resolution, receipt reading, access brokering,
process launch, protocol v2 frames, and the shell rendering a module-authored
problem — is exercised for the first time. What fails is the module's own HTTP
call to its product service.

The cause is the endpoint. `wso2 identity list` records `reference` for
`kanushka-cloud` at `https://api.asgardeo.io`, and the module gets `/status`
under it (`modules/reference/cmd/wso2-module-reference/status.go:38`):

```
$ curl -s -o /dev/null -w "status=%{http_code} type=%{content_type}\n" https://api.asgardeo.io/status
status=302 type=text/html
```

`json.Unmarshal` on that HTML fails, which is exactly the reported problem.
Asgardeo is an identity provider; it does not serve the reference status
service. That service exists only as `internal/statusservice`, stood up
in-process by `test/acceptance`, with no supported way to run it standalone.

**So `wso2 reference status` cannot succeed against a real cloud deployment,
and nothing in the CLI says so.** See [F5](#f5-a-permanent-endpoint-failure-is-reported-as-retryable-and-never-names-the-endpoint)
and [F6](#f6-the-reference-module-cannot-be-run-outside-the-acceptance-harness).

## Findings

### F1. Every `--help` advertises four flags the command may refuse

`--help` prints the root's four persistent flags for every command, but
`forwardShellFlags` refuses a per-command subset, and the refusal's recovery
line points back at the help that just advertised the flag:

```
$ wso2 identity list --context local-ci
error: wso2 identity does not take the flag --context (shell.unsupported_flag)
  Run wso2 identity --help to see the flags it accepts.
exit=64
```

`wso2 identity --help` then lists `--context` again. The same loop reproduces on
`config` (`--context`), `context` (`--context`), `org` (`--context`), `login`
(`--output`), `version` (`--output`, `--context`), and every `module`
subcommand (`--output`, `--context`).

The per-command scoping itself is well reasoned — `shellFlagsFor` in
`internal/app/command.go:339` carries a comment justifying each entry. The
defect is that the help output is generated from the root's flag set instead of
from that same allowlist, so the two disagree, and the error text sends the user
to the side that is wrong. Worse, the message names the *family* (`wso2 module`)
rather than the command typed (`wso2 module available`), so it reads as if a
different command were at fault.

**Suggested shape:** render each command's Flags block from `shellFlagsFor`, and
name the typed command in the refusal. This is the same class of fix as
7178cbf, which scoped `module available`/`module list`'s *unknown*-flag errors;
the *unsupported*-flag path was not covered.

### F2. A module's namespace has no help, and the installer recommends the command that proves it

`make install-module` closes with `Run it: ./bin/wso2 reference --help`, and
that command fails with `reference.unknown_command`. A bare `wso2 reference`
fails identically. There is no way to discover a module's commands from the
module itself; the recovery line sends the user to `wso2 help`, which does not
name them either (F3), so the loop closes with the user no better informed.

**Suggested shape:** the shell should answer `wso2 <namespace>` and
`wso2 <namespace> --help` from the module's declared command tree, or — until a
module declares one — forward `--help` to the module. Failing both, the dev
installer should stop recommending a command that cannot work.

### F3. `help` never names an installed module's commands

`wso2 help` ends with "Product commands are provided by installed modules." and
lists none, with `reference` installed. `wso2 version` is the only place an
installed module is visible, and it shows versions, not commands.

Related to F2, and probably one fix: both need the shell to know a module's
command tree without executing it.

### F4. `--help` on a module command is reported as an error

> **Corrected 2026-09-02.** This finding first said `--help` required a login.
> That was wrong: the reading was taken against the module installed before
> `make install-module` replaced it, and was not repeated afterwards. `--help`
> is answered before any brokering. What follows is the current build.

```
$ wso2 --context local-ci reference status --help
error: pflag: help requested (module.flag_invalid)
  Run wso2 reference status --help to see the flags this command accepts.
exit=64

$ wso2 --context local-ci reference status
error: the credential source the "local-ci" context names is not set (auth.credential_unavailable)
exit=77
```

The same context answers `--help` as a usage error and the bare command as an
auth error, which is what proves the broker is never reached.

- **Help is reported as a failure.** `cobratree.invoke` calls
  `command.ParseFlags(request.Arguments)`; pflag answers `--help` with its
  `ErrHelp` sentinel, and `flagProblem` classifies that as
  `module.flag_invalid`. Asking for documentation exits 64 and says "error".
- **The recovery is the command that failed.** It says "Run wso2 reference
  status --help", which is what was typed.

Deferred to #86. There is nowhere for a module to put help today: standard
output carries protocol frames only, standard error is rendered by
`output.ModuleDiagnostics` which prefixes every line (`reference: Usage:
status`), and `result.Result` is a list of name/label/value fields rather than
prose. A declared command tree lets the shell answer help itself, from the
receipt, launching nothing.

### F5. A permanent endpoint failure is reported as retryable, and never names the endpoint

```
$ wso2 reference status
error: the reference status service answered with something this module cannot read (reference.status_unavailable)
  Retry the command. Report the failure if it persists.
exit=75
```

Three problems in four lines:

- **The recovery is wrong.** "Retry the command" will never fix a context
  pointing at a host that does not serve this product. The user is told to do
  the one thing guaranteed not to work.
- **Exit 75 means transient** (`EX_TEMPFAIL`). A CI job that retries on 75 will
  retry forever. The code is not a per-problem choice: it follows from
  `CategoryProductService` (`internal/exit/exit.go:41`), and `unavailable()`
  assigns that category to every failure it covers. So the question is one of
  classification — an unparseable body from a host that does not serve this
  product is a configuration fault, and the module currently reports it as the
  product service misbehaving.
- **The endpoint is never named.** The module knows it called
  `https://api.asgardeo.io/status`; the user has to read
  `wso2 identity list` and then the module's source to find that out. `--verbose`
  does not help either: the trace stops at "brokering module access" and says
  nothing about the call the module then made.

`readStatus` already distinguishes "did not answer", "stopped part-way", and
"cannot read" (`status.go:63`), and all three collapse into one
`unavailable()` with the same generic recovery. A body that parsed as neither
JSON nor the expected shape is a configuration fault, not a service outage, and
it should say which URL disappointed it.

### F6. The reference module cannot be run outside the acceptance harness

The service the reference module talks to exists only as
`internal/statusservice`, constructed in-process by `test/acceptance`. There is
no target, flag, or documented procedure that stands it up for a
hand-driven run, so `wso2 reference status` has no endpoint it can succeed
against on a developer's machine.

The reference module's stated purpose is to "prove and test the shell, SDK, and
module contract before a real product is migrated" (`CONTEXT.md`). It proves
them under `go test`; it cannot prove them to a person typing commands. Anyone
dogfooding the CLI, or following the module-authoring guide, hits F5's dead end.

**Suggested shape:** a `make status-service` (or a `wso2-status-service` dev
command) that runs the fixture on loopback with a printed endpoint, alongside
the `make install-module` flow that already exists for the module side.

### Minor: a JSON request gets a plain-text error

```
$ wso2 reference status -o json
error: the reference status service answered with something this module cannot read (reference.status_unavailable)
  Retry the command. Report the failure if it persists.
exit=75
```

`-o json` was honored for the result and not for the failure. This is
consistent across the shell — `doctor -o json` also renders JSON then a text
error — so it reads as deliberate rather than as an oversight. Noting it
because a caller parsing stdout still has to fall back to the exit code, and
the dotted problem code that would be most useful to a script is the part left
unparseable.

## What worked well

- **Error text.** Every failure carries a stable dotted code, a plain sentence,
  and a recovery line naming a command. `catalog.unknown_module` explicitly
  rules out a network failure; `shell.unknown_command` names both places a
  command could have come from.
- **Exit codes are distinct and correct** — `64` for usage, `77` for
  authentication, throughout.
- **`whoami` and `doctor`** are the clearest surfaces in the CLI: empty fields
  are not errors, and the next command to type is always stated.
- **`doctor` renders its full report *and* exits non-zero**, so it serves a
  human and a script from one run.
- **`--verbose` keeps diagnostics on stderr**, leaving stdout parseable.
- **Module flag scoping** (`--channel` on `install`, `--all` on `update`) is
  correct and its errors name the right subcommand.
- **Login is honest about scope.** The authorize URL requests exactly the union
  of what the identity declares for the product, and the report afterwards
  names the subject, organization, and reachable products.
- **The URL is printed as well as opened**, so login survives SSH and detached
  shells.
- **`Session expiry   not stated by the issuer`** — a field that says it does
  not know, rather than blank or invented.
- **The auth broker refuses before spending anything.** Pre-login, the module
  process is never launched.

## Not covered

- `wso2 logout` and `wso2 org use` past their `--help` — the session was left
  in place deliberately.
- `wso2 login --url` creating a new identity and context.
- The device-code grant, and the client-credentials grant (nothing was
  listening on `localhost:9443`).
- A **successful** product-command result. The module contract is now proven as
  far as a module-authored problem being rendered; a rendered *success* result
  still has not been observed outside the acceptance suite.

## Reproducing this

```
make build-shell
make install-module NAMESPACE=reference
./bin/wso2 <command>
```

The module store at `~/.wso2/cli/modules` now holds `reference v0.0.0-dev` from
this checkout, pinned, in place of whatever was installed before.
`./bin/wso2 module remove reference` takes it off.
