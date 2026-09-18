# Build a product module

This guide creates a product module, runs it in the shell, tests it, and
releases it. The example namespace is `abc`; use your own.

A product module is a separate program. The shell starts it, handles login,
tokens, output, and exit codes, and passes each command to the module. For
details, see the [module SDK](../reference/module-sdk.md),
[module manifest](../reference/module-manifest.md), and
[module catalog](../reference/module-catalog.md) references.

You need Go and a clone of `wso2/wso2-cli`. Run every command from the
repository root.

## Rules

1. Never write to stdout. It carries the protocol, so one `fmt.Println` breaks
   the module. Return a result from the handler, and write debug output to
   stderr.
2. Import only `github.com/wso2/wso2-cli/sdk/...`.
3. Never log, print, or save an access token.

## 1. Create the module

```sh
make new-module NAMESPACE=abc
```

This creates `modules/abc/` with `go.mod`, `module.json`, `README.md`, and
`cmd/wso2-module-abc/main.go` plus tests. Always use the generator: it fills in
the SDK and protocol versions from your checkout.

The namespace is the command users type (`wso2 abc`), the tag prefix
(`abc/v1.0.0`), and the program name (`wso2-module-abc`). Renaming it later is
a migration, so choose carefully. The generator refuses shell command names,
namespaces already in use, `reference`, and anything other than lowercase
letters and digits starting with a letter.

## 2. Add commands

Commands are [Cobra](https://github.com/spf13/cobra) commands registered in
`commands()` in `main.go`. A handler returns a `result.Result` or a
`problem.Problem` instead of printing:

```go
tree.Handle(cmd, func(ctx context.Context, request module.Request) (result.Result, error) {
	return result.New("abc.greet/v1").
		With("message", "Message", "Hello!"), nil
})
```

To call a protected API, ask the shell for a token with
`request.Access.Acquire`, and send requests to `request.Context.Endpoint`,
which is the product's `url` from the selected context. Declare every
audience and scope in both `module.json` (`capabilities`) and
`module.Options` (`AuthAudiences`, `AuthScopes`), or the shell refuses the
token request. Use a logical audience name such as `abc-api`; users map it to
their deployment in the context.

See [module SDK](../reference/module-sdk.md) for results, tables, error
categories, and access requests, and `modules/reference` for a module that
calls a protected API.

## 3. Install it into a local shell

```sh
export WSO2_HOME=$(mktemp -d)   # optional: keeps your real setup untouched
make install-module NAMESPACE=abc
```

This builds a shell at `./bin/ws` and installs your module into it as a pinned
development version. Use `./bin/ws`, not the CLI on your `PATH`. Set
`CLI_NAME=wso2` to build `./bin/wso2` instead.

## 4. Run it

```sh
./bin/ws abc --help
./bin/ws abc status
```

To call your product, create a context as in
[Set up with Asgardeo](setup-asgardeo.md), then record the product on it:

```sh
./bin/ws context product add abc --url https://abc.example.com \
  --audience <audience> --scopes <scope>
```

The URL is stored as `products.abc.url`.

## 5. Test it

```sh
make build-module NAMESPACE=abc   # compile only
make test-module NAMESPACE=abc    # unit tests, with the race detector
./scripts/acceptance.sh           # before review
```

Unit tests use `sdk/testkit`, which runs a command
through the real protocol with no build or login. `testkit.Access` fakes the
token broker, so it accepts an audience or scope `module.json` does not
declare; the shell's broker refuses one at run time with
`auth.audience_not_declared`, and
`internal/boundaries` holds a test that compares every module's declaration
against what it serves.

After you edit the code, repeat steps 3 and 4. To remove the module:

```sh
./bin/ws product remove abc
```

## 6. Release

Check that a released shell can run the module:

```sh
make gate-module NAMESPACE=abc VERSION=v0.1.0-rc.1
```

Then push a tag:

```sh
git tag abc/v0.1.0-rc.1
git push origin abc/v0.1.0-rc.1
```

The release workflow builds every platform, publishes the archives and
checksums, and regenerates the catalog. Don't edit the catalog by hand.

- A tag with a prerelease suffix such as `-rc.1` publishes to the
  **prerelease** channel. Use one for your first release.
- The release sets `moduleVersion` from the tag. Leave `0.0.0-dev` in the
  source.
- To build the archives locally without publishing, run
  `go run ./cmd/wso2-module-release -tag abc/v0.1.0-rc.1 -out dist`. Don't
  commit `dist/`.

Users then install it:

```sh
wso2 product install abc --channel prerelease
wso2 product install abc@0.1.0-rc.1   # pin an exact version
wso2 product update abc
```

Without `--channel`, `install` uses the stable channel only.

## Before review

- [ ] Created with `make new-module`
- [ ] Same namespace in `module.json`, `module.Options`, the program path, and the tag
- [ ] Only `sdk/...` imports, and no `replace` in `go.mod`
- [ ] Every audience and scope declared in both `module.json` and `module.Options`
- [ ] Handlers never write to stdout
- [ ] `make test-module NAMESPACE=abc` and `./scripts/acceptance.sh` pass

If something fails, see [Troubleshoot a module](troubleshoot-module.md).
