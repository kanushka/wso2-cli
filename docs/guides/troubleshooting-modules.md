# Troubleshooting a product module

**Status:** Working draft
**Related:** [Building a product module](building-product-modules.md),
[module manifest](../reference/module-manifest.md),
[module SDK](../reference/module-sdk.md)
**Last reviewed:** 2026-08-30

The failures a module author meets have a particular character: the module
builds, its tests pass, and something refuses it anyway. That is by design.
Almost everything the shell checks about a module is checked without executing
it, so a mistake in a declaration survives every test the module has and
surfaces on a machine the author is not sitting at.

This page maps what you see to what is wrong. Each entry names the code the
shell prints in parentheses, which is stable and worth searching for.

The single most useful habit: install your module locally and run it before you
tag anything. `make install-module NAMESPACE=<namespace>` installs an
unpublished build through the ordinary installer, so every check below runs
against your module while you can still change it.

## The module builds and tests pass, but output is garbled or the shell reports a protocol failure

Something in the module wrote to standard output.

Standard output carries protocol frames and nothing else. A single `fmt.Println`
in a handler corrupts the stream, and what the shell reports afterwards is a
protocol failure, because from its side that is what happened. No adapter can
prevent this, and the SDK cannot detect it.

Send diagnostics to standard error, which the shell captures as bounded
diagnostics, and return everything the user should see as result fields. The
repository asserts this for every module under `modules/` with a boundary test,
so a module that lands here is caught before release.

## `auth.audience_not_declared` or `auth.scope_not_declared`

> the "api" module asked for access its installation does not declare

The manifest and the executable disagree about what the module may request.

A module states its access twice: `capabilities` in `module.json`, and
`AuthAudiences` and `AuthScopes` in the `module.Options` it serves.
Installation copies the manifest's values into the receipt, and the broker
intersects every runtime request with the receipt. So an audience added to the
Go source but not the manifest is refused, and an audience added to the manifest
but never requested simply does nothing.

The recovery text says to reinstall the module, which is right for a user
holding a broken installation and incomplete for the author. The fix is to make
the two declarations agree, release, and reinstall.

Your tests passed because the test kit does not check this. It is a conforming
peer rather than the shell, and it never intersects a request with declared
capabilities the way the broker does, so `testkit.Access` grants whatever a
handler asks for. That is the gap this failure lives in.

An empty audience is refused by the same code, so a handler that leaves
`AccessRequest.Audience` unset reports as asking for something undeclared.

Add both values in both places. The repository's boundary test compares them for
every module under `modules/`, in either direction, so this is caught by the
ordinary test run once the module is in the tree.

## `modules.incompatible_shell`

> the "api" module requires a WSO2 CLI shell matching ">=0.1.0 <2.0.0", and this shell is 0.0.0-dev

The shell running the command is outside the range `compatibility.shell`
declares.

The confusing case is a shell you built yourself. An uninjected build reports
`0.0.0-dev`, and `>=0.1.0` does not contain it, because a prerelease sorts below
its own release. So a checkout-built shell cannot launch a scaffolded module at
all.

What makes this hard to diagnose is the ordering. Catalog selection checks the
protocol and the platform but **not** the shell range, so the install succeeds
and the refusal arrives later, at the first command, with nothing connecting the
two events.

`make build-shell` builds a shell reporting a version every scaffolded module's
range contains, and `make install-module` builds one and installs for exactly
that version, so the two cannot drift. If you are running a released `wso2`, any
version inside your declared range works.

## `modules.incompatible_protocol`

> the "api" module supports module-contract protocol v2, and this shell speaks v3, v4

The module and the shell share no module-contract version.

There are two forms of this. At launch, the message compares the installed
module against the running shell, as above. At install time, catalog selection
reports instead that no published version speaks a protocol this shell speaks,
and tells the user to update the WSO2 CLI. The first is your module against one
shell; the second is your whole published history against it.

`compatibility.protocolVersions` is not a choice. It is what the SDK the module
was built against speaks, and `make new-module` reads it from the checkout.
Rebuild the module against an SDK whose protocol the shell speaks; do not widen
the list by hand, because widening the window is the shell's job.

Run the release gate before tagging and this cannot reach a user:

```sh
make gate-module NAMESPACE=api VERSION=v1.2.0-rc.1
```

The gate refuses a tag whose declared protocol does not intersect the window of
an already released shell, and names both sides so you can tell whether to wait
for a shell release or rebuild.

## `modules.incompatible_platform`

> the installed "api" module targets linux/amd64, and this shell runs on darwin/arm64

The installed executable was built for a different operating system or
architecture. Reinstall for the machine in hand. A release builds every
supported platform, so this normally means an installation was copied between
machines rather than performed on each.

## `modules.executable_digest_mismatch`

> the executable of the "api" module does not match the digest recorded in its receipt

The executable changed after installation. The shell recomputes the digest on
every launch and refuses when it moved.

For an author this usually means a build was copied over an installed version to
avoid reinstalling. Reinstall instead: `wso2 module remove api`, then install
again. Removal leaves no receipt or version directory behind, so the next
install resolves cleanly.

## `api.handler_failed`, `api.handler_panicked`, or `api.invalid_result`

These carry your own namespace because the SDK raised them on your behalf.

`handler_failed` means a handler returned an error that was not a
`problem.Problem`, so the shell had nothing to classify it with and reported a
module process failure. Return a typed problem instead and you choose the
category, the code, and the recovery text.

`handler_panicked` is what it says. `invalid_result` means the result failed
validation: no schema, no fields, a field with no name, or the same field name
twice.

## The commands never reach the module

Every invocation lands on a shell command instead.

The namespace is one the shell owns. The shell resolves its own commands before
it consults an installed module, so a module in a shadowed namespace builds,
releases, installs, and then never runs. `make new-module` refuses these
outright, naming the shell's commands, which is the refusal worth reading rather
than working around.

Changing a namespace afterwards is a migration rather than a rename: it is the
user's command, the tag prefix, the catalog identity, the executable name, and
the installed-store key.

## `shell.module_not_installed`

> no api module is installed

Nothing by that namespace is installed on this machine. It is a refusal rather
than a silent success so a typo is distinguishable from a no-op. `wso2 module
list` shows what is actually there.

## `auth.context_not_selected`

> the "api" module needs access, and no WSO2 CLI context is selected

The module asked the broker for access and there is no context to act in. This
is configuration rather than a module defect, and it is what a correctly written
module does on a machine that has not been set up yet.

## `catalog.unknown_module`

> no module named "api" is published in the module catalog

The catalog was read and names no such module. This is explicitly not a network
failure, and the message says so, because an outage and a mistake need different
answers.

For an author, the ordinary cause is that the module has not been released yet,
or has been released only as a prerelease while the install is asking for the
stable channel. Ask for the channel:

```sh
wso2 module install api --channel prerelease
```

To run a build that has never been released at all, install it locally rather
than reaching for the catalog.

## The release is refused before anything is published

> release refused: the api module at v1.2.0 requires module-contract protocol v3, and the released shell speaks v2, v1

The gate answers the one question a tag cannot take back, which is whether any
shell that exists can launch what you are about to publish. It runs before the
build, so a refusal has published nothing.

It also refuses a module declaring no protocol version at all, and one whose
`compatibility.shell` cannot be parsed. Both are refused here, where the message
can name the module, rather than several steps later where it would name a
generated document.
