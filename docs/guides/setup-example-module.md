# Set up the reference module locally

The `reference` module lets you try the CLI module contract without publishing
a release or connecting to a product service. You need Go and a clone of this
repository. Run these commands from the repository root.

```sh
export WSO2_HOME=$(mktemp -d)
make install-module NAMESPACE=reference
./bin/ws reference --help
./bin/ws reference status
```

`make install-module` builds `./bin/ws` and installs the module from this
checkout as a development version. Use `./bin/ws` for the commands above; a
CLI already on your `PATH` may use a different installation. `WSO2_HOME`
keeps this test separate from your usual CLI setup.

`reference status` runs without a context or service. With no context selected,
its report says access was refused; that is expected. To test module changes,
run `make install-module NAMESPACE=reference` again, then repeat the command.
For the module's tests, run:

```sh
make test-module NAMESPACE=reference
```

The `reference call` and `reference whoami` commands contact a reference status
service and need a configured context. Start with `reference status` when you
only want to check the local setup.
