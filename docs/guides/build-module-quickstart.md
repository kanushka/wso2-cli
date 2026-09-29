# Build a WSO2 CLI product module

Start from scratch and build a complete, released `ws abc` command tree.
The `abc` namespace is used as a placeholder throughout this guide.

You need Go and a clone of `wso2/wso2-cli`. Run every command from the
repository root.

## Build with an agent

Use the [new-product-module skill](../../.agents/skills/new-product-module/SKILL.md)
to plan and implement a product module. For example, give your agent this prompt:

```text
Use $new-product-module to implement the <namespace> product module from issue #<number>.
Run its tests and verify the commands through ./bin/ws.
```

The steps below show how to build a module manually.

## What you're building

A product module is a separate program. The shell resolves it from the local
module store, launches it, and speaks a Protobuf contract over its standard
input and output. The shell owns login, tokens, rendering, and the process
exit status. Your module owns the commands and what they mean.

That split has one practical consequence worth internalizing before you write
a line: a handler returns values, it does not print. `fmt.Println` in a
handler writes into the protocol stream and breaks the module. Diagnostics go
to standard error.

## 1. Generate it

```sh
make new-module NAMESPACE=abc
```

This writes `modules/abc/` with `go.mod`, `module.json`, a README, a
`cmd/wso2-module-abc/main.go` that answers `status`, and tests that pass with
nothing edited. It also composes the module into the Go workspace.

Use the generator rather than copying another module. It reads the SDK version
and the protocol versions from your checkout, so a generated module is never
built against something this repository doesn't build against.

The namespace is four things at once: the command users type (`ws abc`), the
tag prefix (`abc/v1.0.0`), the program name (`wso2-module-abc`), and the
environment variable prefix (`WSO2_ABC_*`). Renaming it later is a migration.
The generator refuses names a shell command already owns, names another module
declares, `example`, and anything that isn't lowercase letters and digits
starting with a letter.

Checkpoint:

```sh
make test-module NAMESPACE=abc
```

## 2. Add a command

Commands are Cobra commands, built in `commands()` and bound to handlers:

```go
func commands() *cobratree.Tree {
	root := &cobra.Command{Use: Namespace, Short: "ABC commands for the WSO2 CLI."}
	listCommand := &cobra.Command{Use: "list", Short: "List the things."}
	root.AddCommand(listCommand)

	return cobratree.New(root).Handle(listCommand, list)
}
```

`cobratree.Serve` declares the tree to the shell as well as serving it. The
declaration is what lets the shell answer `ws abc --help`, suggest a
correction for a typo, and parse your flags before your program has started.
Generate it from the Cobra tree; never hand-write a second schema.

A handler returns a result:

```go
func list(ctx context.Context, request module.Request) (result.Result, error) {
	return result.New("abc.list/v1").
		With("count", "Things", strconv.Itoa(len(found))).
		WithColumn("name", "Name").
		WithColumn("id", "ID").
		WithRow("first", "1").
		With("next", "Next", "Run ws abc show <id>."), nil
}
```

Fields render as label-and-value lines. Columns and rows render as a table
under them. The field order is part of the answer: it's what the table shows
and the order JSON follows. By convention the last field is named `next` and
says what to run next; the generated test checks that every result has one.

For a failure, return a `problem.Problem` rather than a bare error:

```go
return result.Result{}, problem.New(problem.CategoryProductService,
	"abc.list_unavailable", "The ABC service did not answer.").
	WithRecovery("Check the product URL recorded on this context.")
```

The category is your one exit-code decision, and you make it by naming the
kind of failure rather than picking a number. The shell maps `usage` to 64,
`module_trust` to 69, `module_process` to 70, `product_service` to 75, and
`auth_policy` to 77. Any error that isn't a `Problem` becomes
`abc.handler_failed`, a module process failure, which tells your user nothing.

## 3. Call your product

Your handler needs an access token. It asks the shell:

```go
access, err := request.Access.Acquire(ctx, module.AccessRequest{Audience: "abc-api"})
if err != nil {
	return result.Result{}, err   // typed denials pass through; other errors become abc.handler_failed
}
```

Call `request.Context.Endpoint`, which is the product URL from the selected
context. The token is opaque: don't parse it, log it, persist it, or put it on
a command line.

Declare the audience and scopes in two places, and keep them equal:

```json
{
  "capabilities": {
    "authAudiences": ["abc-api"],
    "authScopes": ["abc:things:read"]
  }
}
```

```go
func moduleOptions() module.Options {
	return module.Options{
		Namespace:     Namespace,
		Version:       moduleVersion,
		AuthAudiences: []string{"abc-api"},
		AuthScopes:    []string{"abc:things:read"},
	}
}
```

The broker enforces the manifest's copy at run time, and a mismatch builds,
tests, and releases clean before failing your first user. A repository test in
`internal/boundaries` compares the two for every module here.

Use a logical audience name like `abc-api`: the stable name your API is known
by, identical against every deployment. Never a client ID or a tenant URL.
Operators map it to whatever their provider stamps into a token, in their own
context.

Leave `Scopes` empty in most requests. An empty list asks for exactly the
scopes the user recorded for your product, which is what lets your commands
stop carrying a `--scope` flag.

## 4. Run it in a real shell

```sh
export WSO2_HOME=$(mktemp -d)    # keeps your own setup untouched
make install-module NAMESPACE=abc
./bin/ws abc --help
./bin/ws abc status
```

`install-module` builds a shell at `./bin/ws` and installs your module into it
as a pinned development version. Use that binary, not the CLI on your `PATH`.

This step is not optional polish. The test kit is a conforming peer, not the
shell: it performs no receipt resolution, no integrity check, and no
capability intersection. If your handler asks for an audience that
`module.json` does not declare, every test you wrote still passes, but the real
shell refuses the request with `auth.audience_not_declared`. Installing locally is how you find that
while you can still fix it.

To exercise a real product, create a context and record yours:

```sh
./bin/ws context create local --issuer https://idp.example/oauth2/token \
  --client-id <client-id> --use
./bin/ws context product add abc --url https://abc.example.com \
  --audience <audience> --scopes abc:things:read
./bin/ws login
./bin/ws abc list
```

Replace the issuer, client ID, product URL, audience, and scopes with values
from your deployment.

## 5. Test it

```sh
make build-module NAMESPACE=abc   # compile
make test-module NAMESPACE=abc    # unit tests with the race detector
./scripts/acceptance.sh           # everything, before review
```

Tests drive the module through the real protocol framing in process:

```go
outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(),
	testkit.Invocation{
		Command: []string{"list"},
		Context: module.Context{Endpoint: fakeServer.URL},
		Access:  &testkit.Access{Token: "test-token"},
	})
if outcome.Err != nil {
	t.Fatalf("the invocation failed: %v", outcome.Err)
}
```

Check `outcome.Err` first: `Result` and `Problem` are both nil when the
exchange itself failed. Leaving `Access` nil denies every request, so a
handler that asks for access it was never given fails loudly.

Point `Context.Endpoint` at an `httptest` server and you can cover your
product client with no deployment and no account.

## 6. Release it

Check that a shell that actually exists can launch your module:

```sh
make gate-module NAMESPACE=abc VERSION=v0.1.0-rc.1
```

Then tag:

```sh
git tag abc/v0.1.0-rc.1
git push origin abc/v0.1.0-rc.1
```

The workflow builds every platform, publishes archives and checksums, and
regenerates the catalog. Don't edit the catalog by hand. A prerelease suffix
publishes to the prerelease channel; use one for a first release. The release
injects `moduleVersion` from the tag, so leave `0.0.0-dev` in the source.

Users then install it:

```sh
ws product install abc --channel prerelease
ws product install abc@0.1.0-rc.1
ws product update abc
```

## Before you open the pull request

- [ ] Created with `make new-module`
- [ ] The same namespace in `module.json`, `module.Options`, the program path,
      and the tag
- [ ] Only `github.com/wso2/wso2-cli/sdk/...` imports, and no `replace` in
      `go.mod`
- [ ] Every audience and scope in both `module.json` and `module.Options`
- [ ] No handler writes to standard output
- [ ] Failures return a `problem.Problem` with a category you chose
- [ ] Installed locally and run under `./bin/ws`
- [ ] `make test-module NAMESPACE=abc` and `./scripts/acceptance.sh` pass

When something is refused, [troubleshoot a
module](troubleshoot-module.md) lists every code the shell prints and what
causes it. The full API is in the [module SDK](../reference/module-sdk.md)
and [module manifest](../reference/module-manifest.md) references. See
[set up the example module](setup-example-module.md) to run a working module
locally.
