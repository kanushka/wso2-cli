# Module SDK

**Status:** Proposed reference
**Related:** [Building a product module](../guides/building-product-modules.md),
[module manifest](module-manifest.md),
[troubleshooting a module](../guides/troubleshooting-modules.md)
**Last reviewed:** 2026-09-10

What a command handler receives, and what it may return. A module imports the
public `github.com/wso2/wso2-cli/sdk/...` packages and never a shell `internal/`
package, so this is the whole surface a product module is written against.

The division to keep in mind: a handler returns semantic values, and the shell
decides how they look and what the process exits with. A handler that formats
output or picks an exit code has taken work that is not its own.

## Serving

```go
func Serve(ctx context.Context, options Options, commands ...Command) error
```

`Serve` speaks the module contract over standard input and output until the
invocation ends. `ServeStreams` is the same over explicit streams, which is what
the test kit uses.

### `module.Options`

```go
type Options struct {
	Namespace     string
	Version       string
	AuthAudiences []string
	AuthScopes    []string
	CommandTree   commandtree.Tree
}
```

The author supplies these. The SDK supplies the protocol versions and the SDK
version itself, so neither can drift from the SDK actually linked.

`CommandTree` is the module's declared command tree, which the shell reads to
parse a product command line as precisely as the module would. A Cobra module
does not set it by hand: `cobratree.Tree.Serve` fills it from the same tree it
serves, which is what keeps a module's commands in one place. Leaving it empty
is supported, and means the shell parses for this module the way it did before
declarations existed: the leading plain words are the command, and everything
from the first flag the shell does not recognize goes to the module unread,
`--output` included, and the shell cannot answer `--help` or suggest a command
for a typo. Serve the tree.

`AuthAudiences` and `AuthScopes` must equal the `capabilities` in
[`module.json`](module-manifest.md#capabilities). They are two declarations of
one fact, and the broker enforces the manifest's copy at runtime.

### `module.Command`

```go
type Command struct {
	Path []string
	Run  Handler
}
```

`Path` is the command path within the namespace, so `wso2 api status` binds as
`[]string{"status"}`; the namespace itself is not an element. Matching is exact
slice equality, with no prefix matching and no aliases. An empty path is the
namespace's own default command. A
command with no handler bound is not served, which is what lets the shell report
an unknown command rather than a silent success.

`sdk/cobratree` serves an existing Cobra tree instead, for a product CLI being
migrated.

```go
tree := cobratree.New(root).Handle(status, runStatus).Handle(list, runList)
err := tree.Serve(ctx, options)
```

`Serve` declares the tree and then serves it. `Declare` and `Commands` are
available separately, but calling `Serve` is one place to change rather than
two to keep in step.

The declaration is generated from the Cobra tree itself: its commands, their
flags, each flag's shorthand and description, and whether a flag carries a
value. Groups that bind no handler are declared too, because the shell has to
parse a path through them. Nothing is hand-written and there is no second schema.

The shell asks for that declaration by running the executable once, at install
time, with `WSO2_MODULE_COMMAND_TREE` set to a file to write. `module.Serve`
answers it and exits without opening the protocol. The request arrives in the
environment rather than as a flag because a module parses its own arguments
before the SDK sees them, and the answer is a file rather than a stream because
standard output carries protocol frames and standard error carries the module's
own diagnostics. A module that ignores the request installs with no declared
tree, which is a supported state.

See [ADR 0013](../adr/0013-a-command-tree-is-parsed-only-from-the-local-receipt.md)
for why the shell parses only from the local receipt and never from the catalog.

## What a handler receives

```go
type Handler func(ctx context.Context, request Request) (result.Result, error)
```

```go
type Request struct {
	InvocationID string
	Command      []string
	Arguments    []string
	OutputMode   OutputMode
	Context      Context
	Access       Broker
}
```

`Arguments` are the user's remaining arguments, unparsed: the shell does not
read a module's flags, and the module owns them.

`OutputMode` is advisory. It is `OutputModeTable` (`"table"`), `OutputModeJSON`
(`"json"`), or `OutputModeUnspecified` (`""`), which is also what an unrecognized
mode decodes to. It tells a handler what the shell intends to render, not what to
produce. The same handler answers every mode, returning the same fields in the
same order.

`InvocationID` identifies this invocation and may appear in a module's own
diagnostics.

### `module.Context`

```go
type Context struct {
	Name           string
	OrganizationID string
	Endpoint       string
}
```

The selected context, and only its non-secret part. `Endpoint` says where to
call, never that the module may: access still comes from the broker.
`OrganizationID` is empty when no context is selected.

### What a request deliberately does not carry

No refresh token, no client secret, no credential of any kind, and no access to
the shell's configuration store. A module can spend access on its product API
and cannot refresh or broaden it.

### What the process environment carries

The shell launches a module with an environment built from nothing and adds
three things back: `WSO2_CA_FILE`, the PEM file of certificates the shell
trusts beside the system roots, which a module's own HTTP client must honour
since the shell's trust does not reach a separate process; `WSO2_NO_INPUT=1`
when `--no-input` or the `WSO2_NO_INPUT` variable asked that nothing prompt or
open a browser; and every variable named `WSO2_<NAMESPACE>_*` for the module's
own namespace, upper-cased. That prefix is how a secret the broker cannot
supply, an administrator password for a one-time bootstrap, reaches a module
that cannot prompt: the command takes the variable's name as a flag and reads
the value from the environment, never the value as a flag. An empty variable
is not passed at all.

## Asking for access

```go
type AccessRequest struct {
	Audience string
	Scopes   []string
}

type Access struct {
	Token     string
	ExpiresAt time.Time
}

Acquire(ctx context.Context, request AccessRequest) (Access, error)
```

The shell intersects the request with what the installed module's receipt
declares, finds the selected context and account, obtains or reuses the
product's session, and returns short-lived access for this one invocation. An
undeclared audience is refused with `auth.audience_not_declared`, and a scope
neither the receipt nor the account's product entry for this namespace names
is refused with `auth.scope_not_declared`, rather than narrowed away: a module
silently granted less than it asked for would proceed believing it holds access
it does not.

`Scopes` may be empty, and ordinarily is. An empty list asks for exactly the
scopes recorded on the account's product entry for this namespace, which the
user or the product's `connect` wrote down when the product was recorded, and
those recorded scopes are the ceiling for every request whichever side named
them. So a module declares every scope its commands can need once, in
`module.json` and `Options`, and a handler names scopes only when one command
should hold fewer than the entry allows.

The token is opaque. Do not parse it, log it, return it, persist it, or pass it
in command-line arguments. `ExpiresAt` lets a module fail early; the audience
enforces expiry regardless.

A denial arrives as a typed problem and should be returned unchanged.

## Returning a result

```go
type Result struct {
	Schema  string
	Fields  []Field
	Columns []Column
	Rows    []Row
}

type Field struct {
	Name  string
	Label string
	Value string
}

type Column struct {
	Name  string
	Label string
}

type Row struct {
	Values []string
}
```

`Schema` identifies the semantic shape, such as `api.status/v1`, so a consumer
of JSON output knows what the fields mean without interpreting them. `Fields`
are in presentation order, and that order is part of the answer: it is what a
table shows and the order JSON follows.

`Name` is the stable machine name and the JSON key. `Label` is what a person
reads, falling back to the name when empty. Build a result with `result.New` and
`With`, which do not mutate the receiver.

By convention the last field is named `next` and says what a user most likely
runs next; the shell renders it as a trailing line under the table. The
generated module follows the convention and its generated test checks it.

Every `Value` is a string, so a module formats its own times and numbers. This
is a deliberate limit of the architecture proof rather than a lasting design:
giving values their own types is a protocol change and belongs to a slice that
can carry one.

### Returning a listing

Fields answer for a result that reports one thing: the shell renders them as a
header row and one value row, so a command that returned one field per item
would produce one column per item and a line no terminal can show.

A listing declares its columns once and adds a row per item, with `WithColumn`
and `WithRow`:

```go
report := result.New("identity.resourceServers/v1").
	With("count", "Resource servers", strconv.Itoa(len(found))).
	WithColumn("name", "Name").
	WithColumn("identifier", "Identifier")
for _, server := range found {
	report = report.WithRow(server.Name, server.Identifier)
}
return report.With("next", "Next", "Record one with wso2 account add-product."), nil
```

The columns are declared once rather than restated by every row, which is what
makes "every row has the same columns" something the shell checks instead of
something two rows could disagree about. A row carrying the wrong number of
values is refused rather than padded or trimmed, because either would put a
value under a header it does not belong to and the reader could not tell.

A result may carry both. The fields describe the listing — how many there are,
what was filtered — and the rows are the answer, so the shell renders the
fields as label-and-value lines above the table rather than folding both into
one table. In JSON the rows are an array of objects under `rows`, keyed by each
column's `Name`: the label may be reworded without a schema change, and the
name is what a script depends on.

`Validate` rejects a result the shell could not render: no schema, no fields, a
field with no name, the same field name twice, rows with no columns declared, a
column with no name, the same column name twice, or a row whose value count
differs from the declared columns.

## Returning a failure

```go
type Problem struct {
	Category Category `json:"category"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Recovery string   `json:"recovery,omitempty"`
}
```

A `Problem` is an ordinary Go error, so a handler returns one directly. Any
other error is reported as a module process failure, so returning a problem is
how a module gets a stable category and code instead.

`Message` and `Recovery` are rendered verbatim and must never carry credential
material. `Code` is stable and machine-readable, conventionally prefixed with
the namespace, such as `api.status_unavailable`.

### The codes the SDK produces for you

Four problems come from the SDK rather than from a handler, each prefixed with
the module's own namespace. `unknown_command` is `usage`, and the rest are
`module_process`: `handler_failed` when a handler returns an error that is not a
`Problem`, `handler_panicked` when it panics, and `invalid_result` when what it
returned fails `Validate`.

Returning a `Problem` is how a handler avoids `handler_failed` and gets a
category and code of its own choosing.

### The five categories, and the exit codes they map to

| Category | Value | Exit code | Covers |
| --- | --- | --- | --- |
| `CategoryUsage` | `usage` | 64 | Invalid arguments, flags, or configuration |
| `CategoryModuleTrust` | `module_trust` | 69 | Module integrity or compatibility failures |
| `CategoryModuleProcess` | `module_process` | 70 | Protocol or module process failures |
| `CategoryProductService` | `product_service` | 75 | Failures reported by a product service |
| `CategoryAuthPolicy` | `auth_policy` | 77 | Authentication and broker policy failures |

Success is 0. An unrecognized category maps to 70, the module process class, so
an unclassified failure is never mistaken for success.

Choosing the category is the module author's one exit-code decision, and it is
made by naming the class of failure rather than by picking a number. A product
API that answered with an error is `product_service`; a user who typed the wrong
flag is `usage`.

## Testing a handler

```go
func Run(ctx context.Context, options module.Options, commands []module.Command,
	invocation Invocation) Outcome
```

`sdk/testkit` drives a module through the real protocol framing in process, so a
test covers the handler and its contract rather than the handler alone. An
`Invocation` carries the `Command` path, the `Arguments` after it, the
`OutputMode`, the `Context` the handler will see (name, organization, and the
product endpoint, which a test points at a fake deployment), and optionally an
`InvocationID`, a `ProtocolVersion` to negotiate, and the `Namespace` the shell
side claims. Access is scripted through `Invocation.Access`, which is a
`*testkit.Access`:

```go
type Access struct {
	Token     string
	ExpiresAt time.Time
	Deny      *problem.Problem
}
```

Setting `Deny` answers the request with that refusal; anything else is a grant.
Leaving `Invocation.Access` nil denies every request with
`testkit.access_not_scripted`, so a handler that asks for access it was never
given fails loudly rather than silently. Nothing here needs a real account
provider.

```go
type Outcome struct {
	Hello          *contractv1.Hello
	Result         *result.Result
	Problem        *problem.Problem
	AccessRequests []module.AccessRequest
	Err            error
}
```

Check `Err` first. It reports that the exchange itself failed, meaning the
module wrote no terminal message, wrote an unexpected one, or its serve loop
returned an error. `Result` and `Problem` are both nil when it is set, so
reading the result first panics instead of reporting what went wrong. Exactly
one of `Result` and `Problem` is set otherwise. `AccessRequests` records what
the module asked the broker for, in order.

The test kit is a conforming peer, not the shell. It performs no receipt
resolution, no integrity check, and no rendering, so a module that satisfies it
is not thereby proven to satisfy the shell.

One consequence is worth stating plainly, because it is the gap module authors
actually fall into. The test kit never intersects a request with declared
capabilities the way the broker does, so a handler asking for an audience that
`module.json` does not declare passes its tests and is refused on a user's
machine with `auth.audience_not_declared`. Install the module and run it under
a real shell before tagging: see the guide's
[Run it under the real shell](../guides/building-product-modules.md#run-it-under-the-real-shell-before-you-tag).

## Naming your own commands

```go
func OwnNamespaceViolations(namespace string, tree commandtree.Tree, dir string) ([]string, error)
```

`sdk/testkit` also guards against a fault no handler test catches: a "next"
field or a recovery that names one of this module's own commands under a
namespace other than its own, or that tells the reader to record this product
under the wrong name. It shipped once — a repository-wide rename swept a
module's own namespace out of its user-facing text, so a command that worked
told the reader to run one that did not exist — and the module still compiled,
its handlers still returned the right fields, and the shell still rendered them
faithfully. Only a person following the instruction found out.

`OwnNamespaceViolations` reads `dir` for its own non-test `.go` files rather
than driving the module through the contract, because a handler test only ever
exercises the paths it scripts, and the damage this exists to catch can sit
behind a branch nothing scripts. `tree` is the module's own declared
tree — `commands().Declare()` — and is where the commands it protects come
from: a command added to the tree is a command protected here, with no second
list to keep in step. `namespace` is the module's own, exactly what
`moduleOptions()` already declares.

```go
func TestEveryCommandThisModuleNamesIsItsOwn(t *testing.T) {
	violations, err := testkit.OwnNamespaceViolations(Namespace, commands().Declare(), ".")
	if err != nil {
		t.Fatalf("checking that %s names its own commands: %v", Namespace, err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}
```

A newly scaffolded module carries this test already; nothing about it needs
editing as the module grows past its first command. The check follows the tree
wherever it goes, with one addition that no tree carries: `connect`, which
every product namespace answers to because the shell serves it itself, named
once here rather than by every module separately.

## The rule the SDK cannot enforce

Standard output carries protocol frames. A handler that calls `fmt.Println`
corrupts the stream, and no adapter can prevent it. Diagnostics go to standard
error, and everything the user should see is returned as result fields.
