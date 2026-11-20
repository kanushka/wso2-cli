# ADR 0026: Debug Logging from the Environment, and a Run Log in the State Root

**Status:** Accepted

`--verbose` was the only way to see what the shell attempted, and what it wrote
was gone when the terminal scrolled. Three gaps followed. A CI job or an agent
wrapper that cannot edit the command line had no switch to throw. A product
module wrote nothing beyond the odd warning, so a failure inside `ws apim`
explained only the shell's half. And by the time someone asked "what happened?"
the run was over, with nothing left behind to read. This decision closes all
three, and keeps the privacy cost of a file on disk small.

## `WSO2_DEBUG`

`WSO2_DEBUG` set to anything but empty, `0` or `false` (in any case) does what
`--verbose` does, for every invocation in the environment. A `--verbose` written
anywhere on the line, before or after the command or product namespace, wins
over the variable: `--verbose=false` switches diagnostics off under
`WSO2_DEBUG=1`. The flag is what the person running the command can see and
drop; a variable exported in a profile months ago is not. This is the same
order every shell setting follows: flag, then environment, then default.

`WSO2_DEBUG=0` reads as off because that is how anyone reading it would read
it. `WSO2_NO_INPUT` treats every non-empty value as on; the two differ because
switching diagnostics off by setting the variable to `0` is a common habit,
while asking for input by setting `WSO2_NO_INPUT=0` is not.

## Modules log through the SDK

When diagnostics are on, whichever of the flag and the variable asked, the shell
adds `WSO2_DEBUG=1` to the module's environment, the way it passes
`WSO2_NO_INPUT=1`. A module never sees the user's own `WSO2_DEBUG`; the module
environment is built from nothing, so `--verbose=false` keeps the module quiet
too. Under diagnostics the shell keeps 256 KiB of a module's standard error
instead of 8 KiB, because it now carries a log rather than a warning.

The SDK gives every handler `Request.Log`, a `*slog.Logger` that writes to
standard error when `WSO2_DEBUG` asks and discards otherwise, as JSON lines when
the invocation renders JSON. The handler's context carries the same logger, and
`httplog.Transport` wraps an HTTP transport so each request made with that
context logs its method, URL without query, status and duration. No header or
body is logged. A module wires it in one line, on the `http.Client` it already
has. It lives in its own package, `sdk/diag/httplog`, so that importing `diag`
for redaction does not import an HTTP client: `wso2 version` must resolve
inventory with no `net/http` in its dependency graph.

Redaction moved from the shell into the SDK's `diag` package, and the shell now
uses it from there. One list of secret attribute names covers both sides of the
module boundary, so a module cannot mask less than the shell does. Inside any
string or error value, whatever its key, it also masks the user info and the
secret query and fragment parameters of every URL (`id_token_hint`, `code`,
`access_token` and the like) and anything shaped like a JWT, because a key
cannot tell a URL that carries an ID token from one that does not. Other
structured values (a map or struct) are not inspected, so they must not carry a
secret.

There is no protocol change. An older module ignores the variable; an older
shell never sets it, and a newer module's log stays silent under it.

## The run log

Every run of the shell writes to `<state root>/cli/logs/wso2.log`, beside the
module store. The directory is created `0700` and the file `0600`, and a file
found wider is narrowed. It rotates when a record would carry it past 1 MiB,
keeping `wso2.log.1` and `wso2.log.2`, so it never holds more than 3 MiB.
Records are JSON lines through the same redaction as standard error, each
carrying the run's own `invocation` id and `pid`.

What it holds depends on whether diagnostics are on:

- **Always:** two records per run. `a run started` carries the shell version
  and platform. `a run ended` carries the command path in declared words only
  (`wso2 context use`, `wso2 apim api list`, or `wso2 apim` for a module that
  declares no tree), the exit code, the problem code when it failed, and the
  duration. No argument, no flag value, no problem message (it can carry a URL
  or an id), and no context, organization or account name.
- **Under `--verbose` or `WSO2_DEBUG`:** everything standard error shows, and
  every line the module wrote to standard error. A module line is free text,
  which key-based redaction cannot read, so it reaches the file as the module
  wrote it, as it already reached the terminal; a module logging through
  `Request.Log` is redacted before the line is written.

The always-on records cost a few hundred bytes a run and hold less than shell
history does, and they let `ws doctor` say "the last run failed" after the
fact, without asking the user to reproduce it first. The detail that makes a log
a privacy surface (issuer URLs, context names, request paths) is written only
when someone asked for it. Debug output is opt-in, so a
diagnostic may name an argument the user gave a built-in command, a context
name for instance, while the always-on records never carry an argument; a product command's arguments are never logged by
the shell, since they belong to the module.

A completion request, which runs on every Tab, writes nothing. A log that cannot
be opened never fails a command and prints nothing, so a read-only home is not
noisy on every run; under diagnostics the reason is the first thing written,
and `ws doctor` reports it once as a `run-log` finding (status `notice`) with
the reason in plain words and a recovery naming the directory. Rotation takes no lock: two runs crossing the cap at once may
shift a few lines out early, which a diagnostic log can afford and a lock taken
on every run cannot.

`ws doctor` adds a `last-run` finding when the newest earlier run (doctor's own
runs aside) failed: status `notice`, the command, its time, exit code and
problem code, and a recovery naming `WSO2_DEBUG=1` and the file. It never
decides doctor's exit status. When the newest run succeeded there is nothing to
report and the finding is absent.

## Considered Options

- **An opt-in log file only** (`WSO2_LOG_FILE=<path>`, as some tools offer).
  Nothing would be on disk unless asked for, and nothing would be there when it
  was needed: the run that failed is the one nobody had switched it on for.
  Rejected in favour of a minimal always-on record.
- **Full diagnostics always written to the file**, as `gcloud` does. Most
  useful after the fact, and it puts every issuer URL, context name and request
  path of every run on disk. Rejected.
- **A protocol field for the debug setting.** It would need a protocol version
  for a boolean the environment already carries, the way `WSO2_NO_INPUT` does.
  Rejected.
- **Keeping redaction in the shell and copying it into the SDK.** It would have
  let the shell ship without an SDK release, at the price of two lists that
  drift. Rejected; the release is due anyway.

## Consequences

**The shell builds against an unreleased SDK until `sdk/v0.5.0` is tagged.** It
imports `sdk/diag`, which `sdk/v0.4.0` does not have; inside the workspace that
resolves from source. Before the next shell release: bump
`modules/example/go.mod` to require `sdk v0.5.0`, add the workspace replace
`docs/reference/release-artifacts.md` describes, push `sdk/v0.5.0`, drop the
replace, then bump the shell's own requirement and `sdkPublishedVersion`.

**Modules log once they are rebuilt.** The example module, `apim` and `iam`
gain diagnostics when they are released against `sdk/v0.5.0` and wrap their
HTTP client's transport with `httplog.Transport`. Until then they behave as before
under `WSO2_DEBUG=1`.

**Module diagnostics are still prefixed on standard error.** The shell writes
each module line as `<namespace>: <line>`, so under `--output json` a module's
JSON log lines are not themselves parseable JSON lines on standard error. The
run log carries them as records with the namespace and line as attributes.
