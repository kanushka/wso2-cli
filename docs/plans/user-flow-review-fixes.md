# User flow review: what to fix

**Status:** Decided. Tracked as #147.
**Date:** 2026-09-01
**Evidence:** [User flow review](../examples/user-flow-review.md) — every claim
below is a recorded command and its real output.

Six findings came out of driving the built shell end to end, including a real
Asgardeo login. Four are new. Two are already tracked, and one of those was
accepted as a known cost rather than missed.

## Where each finding stands

| # | Finding | Status |
| --- | --- | --- |
| F1 | Every `--help` advertises four flags the command may refuse | **New.** Regression inside #85 |
| F2 | `make install-module` recommends `wso2 <ns> --help`, which fails | **New**, small. Regression against #113's own acceptance criterion |
| F3 | `wso2 help` never names an installed module's commands | **Tracked.** #86 delivers it; #85 accepted it explicitly as the cost of not registering namespaces as Cobra commands |
| F4 | `--help` on a module command is reported as an error | **New**, deferred to #86 |
| F5 | A permanent endpoint failure is reported as retryable and never names the endpoint | **New** |
| F6 | The reference module cannot be run outside the acceptance harness | **Fixed.** `status` is now self-contained |
| — | `module list` claims every module is current above a pinned row | **Already #143.** Independently reproduced here |

F3 needs nothing new. The namespace-help half of F2 is also #86; only the
installer's broken advice is separable and worth fixing now.

## The fixes

### F1 — help and enforcement disagree

`--help` renders the root's four persistent flags for every command
(`helpTemplate`, `internal/app/command.go:46`), while `forwardShellFlags`
(`:414`) enforces the per-command allowlist in `shellFlagsFor` (`:339`). The two
have no common source, so help advertises what the command refuses, and the
refusal's recovery points back at the help that advertised it.

Reproduces on `config`, `context`, `org`, `identity`, `login`, `version`, and
every `module` subcommand.

The allowlist is right and well reasoned — each entry carries its justification.
Only the help generation is wrong. Two ways to fix it:

- **Narrow:** give each command a flag set filtered by `shellFlagsFor`, so
  Cobra renders help from the same list that enforces. Small, contained, no
  behaviour change.
- **Proper:** each built-in declares its own flags directly, as `login`,
  `logout`, and `module` already do, and `forwardShellFlags` and `shellFlagsFor`
  both disappear. This is what the code says it wants — "It goes away when each
  command declares its flags directly" (`:411`) — and what #85 stories 5, 16,
  and 17 describe.

Either way, the refusal must name the command typed, not its family:
`wso2 module available --output json` currently reports "wso2 module does not
take the flag --output".

**Needs a decision.** See [Decisions](#decisions-needed).

### F2 — the installer recommends a command that fails

`make install-module NAMESPACE=reference` ends with:

```
Run it:
  ./bin/wso2 reference --help
```

which fails with `reference.unknown_command`. Until #86 lands, the dev
installer should print a command that works — `./bin/wso2 reference status` for
a scaffolded module, or nothing. Fix in `cmd/wso2-module-dev`.

Cheap, isolated, no decision needed.

### F4 — help is reported as an error

Corrected: `--help` does not require a login. It is answered before brokering,
and fails as `module.flag_invalid` (exit 64) with a recovery naming the command
that just failed. Deferred to #86, which lets the shell answer help from the
declared tree; there is no channel for prose in the contract today.

### F5 — a configuration fault reported as a service outage

The module GETs `<endpoint>/status`. For `kanushka-cloud` the endpoint is
`https://api.asgardeo.io`, which answers `302 text/html`, so `json.Unmarshal`
fails and the user is told:

```
error: the reference status service answered with something this module cannot read (reference.status_unavailable)
  Retry the command. Report the failure if it persists.
exit=75
```

Three problems:

- **The recovery is wrong.** Retrying never fixes a context pointing at a host
  that does not serve this product.
- **The endpoint is never named.** The module knows it called
  `https://api.asgardeo.io/status`. `--verbose` does not say either — the trace
  stops at "brokering module access".
- **The class may be wrong.** Exit 75 (`EX_TEMPFAIL`) follows from
  `CategoryProductService` (`internal/exit/exit.go:41`), and `unavailable()`
  gives that category to every failure it covers, including "the body was not
  JSON" — which is a configuration fault, not a service one.

Naming the endpoint is unambiguous and should happen regardless. Whether an
unparseable body should be re-categorised is a real question: the module cannot
distinguish "wrong host" from "broken service" from the body alone, but naming
the URL lets the *user* distinguish them.

**Needs a decision** on re-categorisation.

### F6 — the reference module cannot be run by hand

Fixed by changing what the sample does, not by deploying a service for it.

`wso2 reference status` now answers from the invocation alone: it asks the
broker for access and reports what it was granted, calling nothing. It works on
any machine with a session and nothing deployed.

```
$ wso2 reference status
MODULE                 CONTEXT        ACCESS    AUDIENCE           SCOPES                  EXPIRES
reference v0.0.0-dev   kanushka-dev   granted   reference-status   reference:status:read   2026-09-02T07:01:46Z
exit=0

$ wso2 --context local-ci reference status
MODULE                 CONTEXT    ACCESS    REASON                                          RECOVERY
reference v0.0.0-dev   local-ci   refused   the credential source ... is not set            Set the credential ...
exit=0
```

A refusal is a field rather than an error, so the command can answer its own
question when the answer is "no". It is the only command in the shell that
reports an auth failure as exit 0, and the exception is deliberate.

The service-backed handler survives unchanged as `wso2 reference call`. That is
what still proves a brokered token is accepted at the declared audience, that
another organization's token is refused, and that broker denial and service
failure reach different exit classes. 49 acceptance call sites moved from
`status` to `call`.

Building a local status service was considered and dropped: the sample does not
need a real service to demonstrate the contract, and a fixture reachable by hand
would have sat against the boundary that keeps fixtures out of the shell binary.

## Suggested order

1. **F2** — one string in `cmd/wso2-module-dev`. Unblocks nothing, costs nothing.
2. **F5, naming the endpoint** — contained in `status.go`, independent of the
   categorisation question.
3. **F1** — the largest, and the one with a scope fork.
4. **F6** — makes F5's remaining half testable by hand, and is the only one that
   lets anyone verify a *successful* product command outside `go test`.
5. **F4** — not fixed here. Deferred to #86.

F2, F5, and F1 are independent of each other. F6 is worth designing before
starting.

## Decisions taken

1. **F1 scope** — the proper fix, per #85. Each built-in declares its own flags
   directly; `forwardShellFlags` and `shellFlagsFor` both go away. A narrow fix
   would leave two structures that must agree, which is the defect itself.
2. **F5 classification** — exit 75 and `CategoryProductService` stay. Only the
   text changes: name the endpoint, drop "Retry the command" where retrying
   cannot help. Not re-opening what #40 asks about the stable code list.
3. **F6 scope** — build the dev status service. A `make status-service` target
   running the fixture on loopback, pairing with `make install-module`.
4. **Filing** — one issue, #147, covering F1, F2, F4, F5, and F6.

## Verification

A live Asgardeo session exists on this machine, so F5 can be re-checked without
another login until it expires. F1, F2, and F4 need no session. F6, once built,
is what makes a successful product-command result observable by hand for the
first time.
