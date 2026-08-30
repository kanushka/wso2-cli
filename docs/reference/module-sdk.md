# Module SDK

**Status:** Proposed reference
**Related:** [Building a product module](../guides/building-product-modules.md),
[module manifest](module-manifest.md),
[troubleshooting a module](../guides/troubleshooting-modules.md)
**Last reviewed:** 2026-08-30

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
}
```

The author supplies these four. The SDK supplies the protocol versions and the
SDK version itself, so neither can drift from the SDK actually linked.

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
declares, finds the selected context, and returns short-lived access for this
one invocation. An undeclared audience or scope is refused rather than narrowed
away, because a module silently granted less than it asked for would proceed
believing it holds access it does not.

The token is opaque. Do not parse it, log it, return it, persist it, or pass it
in command-line arguments. `ExpiresAt` lets a module fail early; the audience
enforces expiry regardless.

A denial arrives as a typed problem and should be returned unchanged.

## Returning a result

```go
type Result struct {
	Schema string
	Fields []Field
}

type Field struct {
	Name  string
	Label string
	Value string
}
```

`Schema` identifies the semantic shape, such as `api.status/v1`, so a consumer
of JSON output knows what the fields mean without interpreting them. `Fields`
are in presentation order, and that order is part of the answer: it is what a
table shows and the order JSON follows.

`Name` is the stable machine name and the JSON key. `Label` is what a person
reads, falling back to the name when empty. Build a result with `result.New` and
`With`, which do not mutate the receiver.

Every `Value` is a string, so a module formats its own times and numbers. This
is a deliberate limit of the architecture proof rather than a lasting design:
giving values their own types is a protocol change and belongs to a slice that
can carry one.

`Validate` rejects a result the shell could not render: no schema, no fields, a
field with no name, or the same field name twice.

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
test covers the handler and its contract rather than the handler alone. Access is
scripted through `Invocation.Access`, which is a `*testkit.Access`:

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
given fails loudly rather than silently. Nothing here needs a real identity
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

## The rule the SDK cannot enforce

Standard output carries protocol frames. A handler that calls `fmt.Println`
corrupts the stream, and no adapter can prevent it. Diagnostics go to standard
error, and everything the user should see is returned as result fields.
