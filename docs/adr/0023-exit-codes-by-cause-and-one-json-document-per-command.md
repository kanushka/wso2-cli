# ADR 0023: Exit Codes by Cause and One JSON Document per Command

**Status:** Accepted

Agents and CI jobs run the same commands people do. They branch on the exit
status and parse standard output; they do not read standard error and they do
not scrape prose. This decision gives them both halves: an exit status that
says why a command failed, and exactly one JSON document on standard output
under `--output json`, whether the command succeeded or failed.

## Exit codes by cause

Three problem categories join the five the module contract already names, each
with its own exit status: `not_found` exits 66, `unreachable` exits 68, and
`conflict` exits 73. The other five keep their statuses.

| Status | Class | Category |
| --- | --- | --- |
| `0` | Success | none |
| `64` | Usage | `usage` |
| `66` | Not found | `not_found` |
| `68` | Unreachable | `unreachable` |
| `69` | Module trust | `module_trust` |
| `70` | Module process | `module_process` |
| `73` | Conflict | `conflict` |
| `75` | Product service | `product_service` |
| `77` | Authentication policy | `auth_policy` |

Before this, the status said who refused and not why. A context that does not
exist and a typo in a flag both exited 64. A product module had no category
for a resource that is not there, for a resource that already exists, or for a
deployment it could not reach. So the first product modules sent all three as
`auth_policy`, and each one exited 77, the status that means "log in" to a
script. A caller could not tell "retry later" from "create it first" from
"sign in again" without parsing the message, and the message is prose that may
be reworded.

The new statuses follow the sysexits convention that 64, 69, 70, 75, and 77
already follow: `EX_NOINPUT` (66, input that does not exist), `EX_NOHOST` (68,
a host that cannot be reached), and `EX_CANTCREAT` (73, something that cannot
be created). Each status means one category, and each category one status.

The shell's own problems move where the cause is now nameable:

- **Not found (66):** `contexts.unknown_context`, `contexts.unknown_product`,
  `catalog.unknown_module`, `catalog.version_not_published`,
  `modules.not_installed`, `shell.module_not_installed`, and
  `shell.product_not_installed`. A command or flag that does not exist stays a
  usage error (64). That is a mistake in how the command was typed, not a
  missing thing it was asked about.
- **Conflict (73):** `contexts.context_exists`, `contexts.product_exists`, and
  `shell.completion_file_exists`.
- **Unreachable (68):** `catalog.origin_unreachable` and
  `catalog.artifact_unreachable` when the host gave no answer at all, and
  `auth.discovery_failed` when the identity provider gave none. The codes stay
  the same, because the list of authentication codes is closed. A catalog
  host that answers with an error status or an oversized body keeps exiting
  70, and an identity provider that answers with something the shell will not
  use keeps `auth.discovery_failed` at 77.

## One JSON document per command

**Every shell command accepts `--output` and `--no-input`.** Both are root
persistent flags, so a caller never needs to know which commands render JSON
or which ones could prompt before asking. The one exception to a single JSON
document is `ws completion <shell>`, which prints the script a shell sources
as it is, since wrapping it would break every profile that loads it; a
failure before the script is printed is still an error document. A
standard input that is not a terminal refuses every question the way
`--no-input` does, and the module is handed `WSO2_NO_INPUT=1` either way.
It does not stop a browser login: the person completes that in the browser,
not on standard input, and the authorization URL is on standard error for an
agent to relay. An explicit
`--no-input` still refuses the login with `auth.non_interactive`.

**A success keeps the shape the command already had.** The object's members are
the command's own data, at the top level. There is no `data` wrapper, so
`ws help --output json`, `ws whoami --output json`, a module's result and every
other existing document stay as they were. One member is added: when the table
form of a command ends with a `Next` line, the JSON form carries `next`, an
array of the commands that line names, as runnable lines with real names and
ids filled in. A placeholder such as `<url>` stays only where the shell cannot
know the value. A command with no next step leaves `next` out. A command whose
report is only prose, such as `ws login`, writes `{"message": ...}` with the
prose, and `next` from its `Next` line, until it gets a structured result of
its own. `ws version` and `ws product list` render their data as fields.

**A failure is an object with an `error` member:**

```json
{
  "error": {
    "code": "contexts.unknown_context",
    "category": "not_found",
    "message": "no context named \"staging\" is configured",
    "recovery": "Run ws context list to see the configured contexts, then ws context use <name> to select one.",
    "next": ["ws context list", "ws context use <name>"],
    "exit": 66
  }
}
```

`code`, `category` and `message` are the problem's own fields. `recovery` is the
problem's guidance as written, left out when there is none; it is where a
reader finds why each command is suggested. `next` holds the commands the
recovery names, in the same shape as a success's `next`, so an agent reads one
shape whichever way the command ended. It is always present, and empty when
the recovery names no command. `exit` is the process exit status. A top-level
`error` member is how a reader tells a failure from a success; no success
document has one. A command that rendered a JSON report before failing, such as
`ws doctor`, keeps its members and gains `error`; one that printed prose keeps
it as `message`.

The same problem is still written to standard error as prose, because a person
reading a CI log reads that stream, and a script that already reads it keeps
working.

A module's problem takes the same path. The shell builds the document from the
`Problem` the module returned, so the module contract and the SDK do not
change for it.

## Considered Options

- **Classifying by problem code**, such as treating any code ending in
  `.not_found` as 66, would also reclassify modules already published, with no
  module release. But it would turn a naming habit into a contract that no
  author agreed to, and the category is already the documented thing the exit
  status is derived from. Rejected.
- **One "resource" category with the cause in a new field** would keep the
  category list short. But it adds a protocol field to express what a category
  already expresses. Rejected.
- **Statuses outside sysexits** (for example 2, 3, 4) would be shorter to type,
  but they would mix two conventions in one table. Rejected.
- **A `{"data": ..., "next": ...}` wrapper on every success** would make success
  and failure symmetric. But it changes every JSON document the shell has
  written, including `ws help --output json`, which agents already read.
  Rejected.
- **`next` as `{command, why}` objects** would put the reason beside each
  command. But a module's `next` already renders as an array of command
  strings, and the release built from the `release/asgardeo-cli` branch ships
  that shape. One shape for both outcomes is worth more than a second copy of
  the recovery. Rejected.
- **The failure document on standard error** keeps standard output empty on
  failure, as it was. But a caller that captures only standard output, the
  common case for a tool-calling agent, would see nothing. Rejected.
- **A structured `next` in the module contract** would let a module state its
  commands instead of the shell extracting them from prose. It needs a
  protocol field and an SDK release for something the shell already extracts
  from every recovery in this repository. Deferred until a module needs what
  extraction cannot give.

## Consequences

**No protocol change.** The category is a string on the wire. A shell that
does not know a category maps it to 70, so a newer module talking to an older
shell fails and never reports success. A module that sends a new category
should raise its minimum shell version, so the status it means is the status
the user sees. The SDK change is three new constants; nothing is renamed or
removed.

**Published modules keep their statuses.** A module built against an earlier
SDK still sends `auth_policy` for a missing resource and still exits 77, until
it is released against an SDK that names the new categories.

**`ws doctor` reports the class of its most severe failing check, as
before.** An unreachable catalog or issuer under `--online` now exits 68
instead of 70 or 77.

**Extraction is only as good as the prose.** A recovery that names a command
without the shell's name in front of it yields no `next` entry. Recoveries in
this repository always write the full command, and the extractor is the one
that already highlights commands for the table form, so both forms agree on
what counts as a command.
