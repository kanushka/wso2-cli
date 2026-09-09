# Module manifest

**Status:** Proposed reference
**Related:** [Building a product module](../guides/building-product-modules.md),
[module catalog](module-catalog.md),
[release artifacts](release-artifacts.md),
[troubleshooting a module](../guides/troubleshooting-modules.md)
**Last reviewed:** 2026-09-07

`module.json` is what a product module declares about itself. It sits at
`modules/<namespace>/module.json`, `make new-module` writes it, and a module
author edits it by hand afterwards.

It is small, and every field in it is load-bearing. The release gate decides
over one of them, the catalog publishes three, and installation copies two into
the local receipt the shell reads on every launch. This page states what each
one means and what refuses when it is wrong.

```json
{
  "schemaVersion": 1,
  "namespace": "api",
  "compatibility": {
    "shell": ">=0.1.0 <2.0.0",
    "protocolVersions": [2]
  },
  "capabilities": {
    "authAudiences": ["api.example.com"],
    "authScopes": ["api:read"],
    "product": {
      "provider": "thunder",
      "clientId": "wso2-cli",
      "audience": "resource",
      "defaultAudience": "https://localhost:8090/mcp",
      "scopes": ["system"],
      "machine": ["inline"]
    }
  }
}
```

## Who reads it

Discovery reads every `modules/*/module.json` in the checkout. Catalog
generation copies `compatibility` and `capabilities` into the published entry
for each released version. Installation writes those same two into the module's
receipt, and from then on the shell answers from the receipt and never consults
the manifest again.

That last point is what makes the manifest a release-time document rather than
a runtime one. Editing it on an installed module changes nothing until the
module is released and reinstalled.

## `schemaVersion`

The manifest format. It is `1`, and generation refuses any other value naming
the namespace that carried it. It is not the module's version, the shell's, the
protocol's, or the SDK's.

## `namespace`

The top-level command the module owns, and the first word a user types. It must
match `^[a-z][a-z0-9-]{0,31}$`: a lowercase letter, then up to 31 more lowercase
letters, digits, or hyphens.

The same rule is applied by the shell and by catalog generation, so a namespace
the shell would refuse cannot be published. `make new-module` applies a
deliberately tighter one, refusing hyphens, and also refuses a namespace another
module declares, a namespace a shell command owns, and the reserved `reference`
namespace. Those extra refusals belong to the generator rather than to the
format: a hyphenated namespace in a hand-written manifest is valid here.

The namespace appears in five places and must agree in all of them: this field,
`module.Options`, the executable name, the directory under `modules/`, and the
release tag prefix.

## `compatibility.shell`

The shell versions the module supports, as a whitespace-separated conjunction of
comparators. Every comparator must hold. The operators are `>=`, `<=`, `>`, `<`,
and `=`, and a bare version means exact equality.

```text
">=0.1.0 <2.0.0"
```

This is a policy statement, and the only field whose value is genuinely the
author's choice. `make new-module` writes `>=0.1.0 <2.0.0` because that is what
a new module is expected to support, not because anything measured it.

The shell checks this on **every launch**, against its own version, and refuses
with `modules.incompatible_shell` when it does not hold. Catalog selection does
not check it, so a module can install successfully and then refuse to launch.
That gap is why a shell built from a checkout, which reports `0.0.0-dev`, cannot
launch a module declaring `>=0.1.0`: a prerelease sorts below its own release.
See [troubleshooting](../guides/troubleshooting-modules.md).

The release gate refuses a range it cannot parse, naming the module, rather than
letting the failure surface later as an unreadable catalog document.

## `compatibility.protocolVersions`

The module-contract versions the module speaks. This is the field that decides
whether a shell can launch the module at all, and it is not a matter of opinion:
it is what the SDK the module was built against speaks, which `make new-module`
reads from the checkout with `protocol.Supported()`.

Do not invent a value here, and do not widen it by hand. Widening the window is
the shell's job, not a module's.

Two things check it. The release gate refuses a tag whose declared protocol does
not intersect the window of an already released shell, naming both sides, so a
module that no shell could launch is never published. Then every invocation
negotiates: the shell picks the newest version both speak, and refuses with
`modules.incompatible_protocol` when the sets are disjoint.

An empty list is refused by the gate, because nothing would then state which
shells can launch the module.

## `capabilities`

What the module needs of the shell's authentication, stated before the module
runs. All three members are optional and absent when empty.

- `authAudiences` are the audiences a handler may name.
- `authScopes` are the scopes it may ask for.
- `product` is the product descriptor `wso2 <namespace> connect` writes a
  product record from; see below.

`authAudiences` and `authScopes` are a ceiling, not a request. A newly
scaffolded module carries empty lists because it asks the shell for nothing yet.

Installation records them in the receipt, and the broker intersects every
runtime request with what the receipt authorized. So an audience a handler
requests but the manifest does not declare is refused at runtime, on a real
user's machine, rather than at build time.

**Keep these equal to the `module.Options` the executable serves.** They are two
declarations of one fact, and nothing at build time compares them for you except
the repository's own boundary test, which does exactly that for every module
under `modules/`. A mismatch builds clean, tests clean, releases clean, and
fails the first user.

Declare a logical audience, meaning the stable name your API is known by,
identical against every deployment. It must never be a client ID, a tenant URL,
or anything else that differs between customers. The operator records the
concrete value their identity provider stamps into a token in their own context
document, and the shell proves the issued token is bound to it.

This is not a style preference. The deployments the shell supports each bind a
token's audience differently, so a module that compiled one deployment's value in
would be installable only against the single tenant it was built for.

A scope a handler asks for must be declared here or recorded on the identity's
product entry for the namespace, and the entry's scopes are the ceiling either
way. A request naming no scopes asks for exactly the entry's recorded scopes,
so a module ordinarily declares here every scope its commands can ever need and
its handlers then name none.

## `capabilities.product`

The **product descriptor**: what the module declares about reaching its
product, so that `wso2 <namespace> connect <url>` can write the identity's
product record from the URL alone. It travels with the other capabilities
through the catalog into the receipt, and the shell reads it from the receipt
before the module is launched; the module never sees `connect`. Everything in
it is public configuration, and it names no credential.

```json
"product": {
  "provider": "thunder",
  "issuerPath": "",
  "clientId": "wso2-cli",
  "audience": "resource",
  "defaultAudience": "https://localhost:8090/mcp",
  "scopes": ["system"],
  "grant": "",
  "machine": ["inline"]
}
```

| Field | Meaning | Refused when |
| --- | --- | --- |
| `provider` | The identity provider this product *is*, when a login can run against it: `asgardeo`, `identity-server`, or `thunder`. Empty for a product that is not a login provider. | It names a provider this shell does not read. |
| `issuerPath` | Appended to the connect URL to name the issuer; `/oauth2/token` for API Manager. Empty when the URL is the issuer. | It does not start with `/`. |
| `clientId` | The public client the shell presents at the issuer, when the product's bootstrap registers a fixed one. Empty when the deployment assigns one and `connect` must be told it with `--client-id`. | Never; it is optional. |
| `audience` | How the deployment binds a token's audience: `resource`, a resource-server URI sent as an RFC 8707 resource indicator (ThunderID), or `client`, the client id the shell presents (API Manager, Asgardeo). | It is neither. |
| `defaultAudience` | The resource-server URI a `resource` audience defaults to, when the deployment seeds one. `connect --audience` overrides it, and is required when it is empty. | Never; it is optional. |
| `scopes` | The scopes the product's commands need. `connect` records them on the product entry, and they become the ceiling for every request. | It is empty. |
| `grant` | How the product is reached when it is not the login provider: `federated` (a public client at the product's own issuer, through the same browser sign-on) or `jwt-bearer`. Empty for a product only its own provider serves. | It names a grant this shell does not implement. |
| `machine` | The strategies a client-credentials identity may use: `inline` (the identity's own machine client, minted per product) and/or `credential` (a credential of the product's own, given to `connect` as variable names). | It names a strategy this shell does not implement. |

The refusals above are made when the receipt is read, as `modules.receipt_malformed`,
naming the field. A manifest carrying them builds and tests clean, so read the
table before tagging.

Two shapes occur. A product that is itself the login provider declares
`provider`, so `wso2 <namespace> connect <url>` creates the identity, the
context, and pins the login product. A product reached through a login
provider declares `issuerPath`, `audience`, `grant: federated` and its
`machine` strategies, so `wso2 <namespace> connect <url> --client-id <id>`
attaches it to the identity already logged in, and a pipeline hands it a
credential of the product's own.

### When not to declare one

Declare a descriptor only when the product's issuer can be named from the
product's URL. A product whose issuer has to be discovered from the product,
Agent Manager's bundled ThunderID at a host of its own, found through RFC 9728
protected-resource metadata, cannot be described here today, and a module for
it declares none. The shell then refuses `wso2 <namespace> connect` with
`shell.connect_unsupported`, naming `wso2 identity add-product`, and the module's
`status` should name that command too.

## What is deliberately absent

There is no module version here. A module's version comes from its release tag
and is injected into the executable at build time, so a manifest cannot
disagree with the tag that published it.

There is no SDK version. What the module compiled against is recorded by the
build, and it says nothing about which shells can launch the module.

There is no artifact URL, size, or digest. Those are generated from the release
that published a version, and no one hand-authors a catalog entry.
