# Set up the example module locally

The `example` module lets you try the CLI module contract without publishing
a release or connecting to a product service. Its source is in
`modules/example/`. It is excluded from the public product catalog;
`ws product install example` does not install it. Use the local installation
commands below.

You need Go 1.25.13 or later and a clone of this repository. Run these commands
from the repository root.

```sh
(
  export WSO2_HOME="$(mktemp -d)"
  trap 'rm -rf "$WSO2_HOME"' EXIT
  make install-module NAMESPACE=example &&
    ./bin/ws version &&
    ./bin/ws example --help &&
    ./bin/ws example status
)
```

`make install-module` builds `./bin/ws` and installs the module from this
checkout as a development version. Use `./bin/ws` for the commands above; a
CLI already on your `PATH` may use a different installation. `WSO2_HOME`
keeps this test separate from your usual CLI setup. The subshell restores your
terminal's environment when it ends, and the trap removes the temporary store.

`example status` runs without a context or service. With no context selected,
its report shows context `(none)` and access `refused`, and the command exits
successfully. That is expected. `./bin/ws version` lists `example` at
`v0.0.0-dev`; the installation is pinned, so `ws product update` will not
replace it with a published release. To test module changes, repeat the block
above to rebuild and install into a fresh temporary store.
For the module's tests, run:

```sh
make test-module NAMESPACE=example
```

The `ws example call` and `ws example whoami` commands contact an example
status service and need a configured context, authentication, and a running
service. Start with `ws example status` when you only want to check the local
setup.
