# Module quickstart

Create a product module, try it in the `wso2` shell, and run its tests. The
steps take about five minutes. The example namespace is `abc`, so replace it
with your own.

For the full walkthrough, see [Building a product module](building-product-modules.md).

## Before you start

- Go, installed
- A clone of `wso2/wso2-cli`. Run all commands from the repository root.

## 1. Create the module

```sh
make new-module NAMESPACE=abc
```

This creates `modules/abc/`. Your commands go in
`modules/abc/cmd/wso2-module-abc/main.go`.

The namespace becomes the command users type (`wso2 abc`). Use lowercase
letters and digits, and choose it carefully because renaming it later is hard.

## 2. Build and install it into the shell

```sh
export WSO2_HOME=$(mktemp -d)   # optional: keeps your real wso2 setup untouched
make install-module NAMESPACE=abc
```

This builds `./bin/wso2` and installs your module into it.

## 3. Run it

```sh
./bin/wso2 abc --help
./bin/wso2 abc status
```

Use `./bin/wso2`, not the `wso2` on your `PATH`.

## 4. Run unit tests (optional)

```sh
make test-module NAMESPACE=abc
```

This runs the module's Go tests only. It doesn't build or install anything,
and it doesn't need the shell or a login.

## 5. Change the code and repeat

Edit `main.go`, then run steps 2 to 4 again.

To uninstall the module:

```sh
./bin/wso2 product remove abc
```

## Rules

1. **Never print to stdout.** Return a result from your handler instead. Write
   debug output to stderr.
2. Import only `github.com/wso2/wso2-cli/sdk/...`.
3. Never log, print, or save access tokens.

## Next steps

- [Add commands and call a protected API](building-product-modules.md#2-add-a-command)
- [Release your module](building-product-modules.md#6-release)
- [Troubleshooting](troubleshooting-modules.md)
