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

**Integrity-checked module**:
A module whose executable matches its local receipt but whose publisher and
release metadata have not necessarily passed the production trust chain.
_Avoid_: Verified module

**Verified module**:
A module whose publisher, release metadata, artifact, compatibility, and
revocation status have passed the production trust policy.
_Avoid_: Integrity-checked module

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
