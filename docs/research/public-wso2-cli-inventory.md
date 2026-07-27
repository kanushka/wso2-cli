# Public WSO2 product CLI inventory

**Status:** Research
**Research date:** 2026-07-23
**Source policy:** Public repositories, public release metadata, and public
product documentation only

## Purpose

This inventory records publicly verifiable WSO2 command-line tools that are
relevant to the `wso2` command. It supports architectural
analysis without relying on private repositories, unpublished source trees, or
internal implementation information.

The inventory excludes build scripts, operators, language tooling, internal
services, and products for which sufficient public CLI evidence is unavailable.
Omission does not imply that a product lacks command-line tooling.

## Current public product CLIs

### Choreo CLI

- **Public repository:** [`wso2/choreo-cli`](https://github.com/wso2/choreo-cli)
- **Command:** `choreo`
- **Public evidence:** The repository publishes installation and release
  material. Its
  [README](https://github.com/wso2/choreo-cli/blob/main/README.md) documents
  commands including login, list, and describe.
- **Inventory note:** Public materials are sufficient to assess the user-facing
  command and distribution model. This document makes no claim about
  unpublished implementation details.

### WSO2 Developer Platform CLI

- **Public repository:** [`wso2/wdp-cli`](https://github.com/wso2/wdp-cli)
- **Command:** `wdp`
- **Public evidence:** The
  [README](https://github.com/wso2/wdp-cli/blob/main/README.md) documents the
  public command surface, and the
  [release page](https://github.com/wso2/wdp-cli/releases) publishes platform
  binaries.
- **Inventory note:** The public repository is evaluated as a distribution
  surface only.

### API Platform CLI

- **Public repository:** [`wso2/api-platform`](https://github.com/wso2/api-platform)
- **Command:** `ap`
- **Public source location:** [`cli/`](https://github.com/wso2/api-platform/tree/main/cli)
- **Implementation evidence:** The public CLI source uses Go and Cobra, as shown
  by its [`go.mod`](https://github.com/wso2/api-platform/blob/main/cli/src/go.mod)
  and [root command](https://github.com/wso2/api-platform/blob/main/cli/src/cmd/root.go).

### Agent Manager CLI

- **Public repository:** [`wso2/agent-manager`](https://github.com/wso2/agent-manager)
- **Command:** `amctl`
- **Public source location:** [`cli/`](https://github.com/wso2/agent-manager/tree/main/cli)
- **Implementation evidence:** The public source uses Go and Cobra. Its
  [factory-injected root command](https://github.com/wso2/agent-manager/blob/main/cli/pkg/cmd/root.go)
  provides a useful public example for evaluating a module adapter or SDK
  integration.

### API Manager CLI

- **Public repository:** [`wso2/product-apim-tooling`](https://github.com/wso2/product-apim-tooling)
- **Command:** `apictl`
- **Public source location:** [`import-export-cli/`](https://github.com/wso2/product-apim-tooling/tree/master/import-export-cli)
- **Implementation evidence:** The public source uses Go and Cobra. The
  [CLI README](https://github.com/wso2/product-apim-tooling/blob/master/import-export-cli/README.md)
  documents API Manager administration and deployment workflows.

### WSO2 Integrator: MI CLI

- **Public repository:** [`wso2/product-mi-tooling`](https://github.com/wso2/product-mi-tooling)
- **Command:** `mi`
- **Public source location:** [`cmd/`](https://github.com/wso2/product-mi-tooling/tree/master/cmd)
- **Implementation evidence:** The public source uses Go and Cobra. The
  [CLI README](https://github.com/wso2/product-mi-tooling/blob/master/cmd/README.md)
  and [public releases](https://github.com/wso2/product-mi-tooling/releases)
  identify the product and executable.

## Related public CLI

The Identity Server configuration CLI, `iamctl`, is maintained in the public
[`wso2-extensions/identity-tools-cli`](https://github.com/wso2-extensions/identity-tools-cli)
repository. Its
[README](https://github.com/wso2-extensions/identity-tools-cli/blob/master/README.md)
documents the command and supported Identity Server versions. Its location
demonstrates that public CLI discovery should not be limited to a single GitHub
organization.

## Secondary or specialized public tools

- [`wso2/cellery`](https://github.com/wso2/cellery) is an archived public
  platform and SDK that included the `cellery` CLI. It provides migration
  history rather than a current first-wave integration target.
- [`wso2/arazzo-mcp-generator`](https://github.com/wso2/arazzo-mcp-generator)
  is a public Go and Cobra developer tool. It is specialized build tooling
  rather than a product control-plane CLI.

## Publicly supported design implications

1. Public source confirms that Go and Cobra are established implementation
   choices across several relevant WSO2 CLIs.
2. Public repository placement and distribution models vary across products.
3. A migration study should cover both a factory-injected command tree and a
   legacy command tree with greater initialization coupling.
4. Namespace assignment requires product-owner agreement; repository location
   alone is not a sufficient ownership rule.
5. Claims about non-public implementation details must remain outside the
   public repository.
