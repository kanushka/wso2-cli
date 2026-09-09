# WSO2 CLI

The WSO2 CLI provides one shell for independently released, WSO2-owned product
command modules.

## Language

**Shell**:
The user-facing `wso2` command that owns shared policy and dispatches product
commands.
_Avoid_: Root CLI, host CLI

**Product module**:
An independently released executable that owns one WSO2 product namespace and
implements that product's commands through the module contract.
_Avoid_: Plugin, extension

**Product namespace**:
The unique top-level command name assigned to one product module.
_Avoid_: Module name, command prefix

**Reference module**:
A non-product module used only to prove and test the shell, SDK, and module
contract before a real product is migrated.
_Avoid_: Pilot module, Agent module

**Product descriptor**:
What a product module declares in its manifest about reaching its product:
whether the product is an identity provider, how its issuer is named from a
URL, how tokens are bound, the scopes its commands need, and the grant and
machine strategies it accepts. `wso2 <namespace> connect` writes a product
record from it; a module without one is recorded with `wso2 identity
add-product` instead.
_Avoid_: Product config, connect metadata

**Module contract**:
The mandatory versioned interaction between the shell and a product module.
_Avoid_: Plugin API

**Module receipt**:
Shell-owned local metadata that identifies an installed module executable and
the compatibility and integrity facts needed to resolve it without execution.
_Avoid_: Manifest

**Managed module store**:
The shell-owned local installation area from which module versions and receipts
are resolved.
_Avoid_: Plugin directory, PATH

**Module catalog**:
The two files generated from the tags that exist and served over HTTPS, from
which the shell discovers what module versions were published and where their
artifacts are. It is a build output, not curated metadata.
_Avoid_: Registry, index

**Catalog index**:
The single catalog file naming the latest version on each channel for every
product namespace, whose size is bounded by namespaces and channels rather than
by release history.
_Avoid_: Manifest, listing

**Development origin**:
A catalog origin serving locally built module archives, from which a developer
installs a module that has never been published. It is read by the same client,
and produces the same installation, as the published origin.
_Avoid_: Local registry, fake catalog

**Module version**:
A module's own release version, moving independently of the shell version, the
protocol version, and the SDK version. A module tag carries it, and the catalog
publishes it per channel.
_Avoid_: Version, release number

**SDK version**:
The Go module version of the public SDK a module compiles against. It is the
version a module's `go.mod` names, and it says nothing about which shells can
launch the module: the protocol version alone decides that.
_Avoid_: Contract version, API version

**Release channel**:
The track a module version is published on, derived from its version: a version
carrying a prerelease identifier is a prerelease and every other version is
stable.
_Avoid_: Stream, ring

**Integrity-checked module**:
A module whose executable still matches the digest in its local receipt, and
whose archive matched the digest the catalog published for it at install time.
Nothing attests to the authenticity of the catalog entry itself.
_Avoid_: Verified module

**Architecture proof**:
A non-production vertical slice that validates the riskiest architectural
boundaries without claiming user-ready product value.
_Avoid_: Pilot release, minimum viable product

**Login mode**:
How one interactive identity's session is established on the machine at hand —
through a browser on this machine, or through a code approved on another
device. It is a property of the machine and the moment, not of the identity's
credentials, so the same identity may be established either way.
_Avoid_: Login type, authentication kind

**Sign-on**:
The identity provider's own browser session, held by the browser rather than
by the shell. One sign-on answers every authorization the shell runs for that
identity, so a person enters credentials once however many products follow.
_Avoid_: SSO session, browser login, auto sign-in

**Product session**:
The authorization one interactive identity holds on this machine for one
product namespace, kept in the OS secure store under that identity's
credential reference. It is bound to one issuer, one client and one scope
set, so no product session carries another product's authority.
_Avoid_: Session, login, credential, token

**Login session**:
The product session of an identity's login product. Its authorization is the
one that establishes the sign-on every other product session is obtained
through.
_Avoid_: Master session, primary session, parent session

**Login product**:
The product an identity logs in through, fixed when the identity first
records one so that a product recorded later cannot displace it.
_Avoid_: Default product, primary product

**Acquisition strategy**:
How one product's session is obtained for an identity: direct, sibling,
derived, or federated. It follows from what the identity records about the
product, not from a choice made at the command line.
_Avoid_: Auth method, grant type, flow
