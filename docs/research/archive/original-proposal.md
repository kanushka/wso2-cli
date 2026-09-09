# Archived original WSO2 CLI proposal

**Status:** Archived and non-authoritative
**Original role:** Initial experience proposal
**Superseded by:** [Product requirements](../../product-requirements.md) and
[architecture](../../architecture.md)

> [!CAUTION]
> This document preserves the intent of the initial proposal for historical
> context. Its command names, ownership boundaries, authentication guidance,
> configuration shapes, and installation behavior are not current decisions.

## Historical problem statement

WSO2 products have provided separate command-line interfaces with differences
in:

- binary and command names;
- resource and action ordering;
- version and status behavior;
- login and credential handling;
- structured output; and
- non-interactive operation.

The proposal observed that these differences increase the learning and
automation cost for developers, CI systems, and coding agents that work across
multiple WSO2 products.

## Original solution direction

The proposal introduced one `wso2` entry point with product-specific commands
under product or platform namespaces:

```text
wso2 <product> <resource> <action> [flags]
```

Illustrative commands included:

```shell
wso2 version
wso2 login
wso2 context list
wso2 doctor --output json

wso2 api gateway list
wso2 identity apps list
wso2 integration component deploy --file integration.yaml
wso2 agent projects list
```

The root command was expected to provide common authentication, context,
configuration, output, error, version, and update behavior. Product teams would
retain responsibility for product workflows through separately delivered
modules.

## Target selection

The proposal used contexts to distinguish WSO2 Cloud from on-premises
deployments.

### Cloud

Cloud was the default target. A user could select a region, organization, and
project when required:

```shell
wso2 login --region eu --org acme --project retail-dev
wso2 context current
```

### On-premises deployments

On-premises targeting was explicit. A context could contain multiple product
endpoints, and each product could use an authentication method supported by its
deployment:

```shell
wso2 context create customer-dev --type onprem \
  --api-url https://api.dev.example.com \
  --identity-url https://id.dev.example.com \
  --agent-url https://agent.dev.example.com

wso2 context use customer-dev
wso2 api gateway list
```

The current architecture refines this model by requiring the context to declare
the authentication method for each applicable endpoint and by prohibiting any
assumption that an on-premises deployment provides shared WSO2 authentication.

## Credential-handling intent

The original proposal established the following principles:

- secrets should not be supplied as ordinary command-line argument values;
- context files should contain target information, not credentials;
- interactive long-lived credentials should use an operating-system secure
  store; and
- automation should obtain secrets from an external secret store and use
  non-interactive operation.

The current product requirements and architecture define the normative
credential rules. In particular, CI secrets are read directly from an approved
secret source into process memory and are not persisted in context files,
credential files, module environments, or command arguments.

## Context example

The original proposal used a conceptual configuration similar to:

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: acme-dev

contexts:
  - name: acme-dev
    type: cloud
    cloud:
      region: eu
      org: acme
      project: retail-dev

  - name: customer-dev
    type: onprem
    products:
      api:
        endpoint: https://api.dev.example.com
      identity:
        endpoint: https://id.dev.example.com
```

This shape is illustrative only. Current examples are maintained in
[authentication context examples](../../examples/authentication-contexts.md).

## Module concept

The proposal anticipated:

- a small root CLI for shared behavior;
- separately delivered product modules;
- consistent structured output and machine-readable errors;
- versioned and signed module releases; and
- pre-installation of modules for restricted or air-gapped environments.

The current architecture replaces the proposal's informal module-loading
description with a managed local store, signed catalog metadata, a mandatory
versioned subprocess contract, a public Go SDK, verified receipts, and explicit
online and offline installation flows.

## Historical example scenario

The proposal illustrated a retail application in which:

- an identity product configures web application single sign-on;
- an integration product deploys web and backend components; and
- an API product configures and publishes a protected API.

All names and domains in that scenario were illustrative. The example
demonstrated cross-product command consistency; it did not define a committed
product command surface.

## Superseded or unresolved elements

The following aspects of the original proposal must not be treated as current
decisions:

- whether project selection belongs to the root shell or product modules;
- whether first use may install a module automatically;
- final product namespace names;
- exact module lifecycle command names;
- context schema field names;
- product-specific login aliases;
- module packaging and trust metadata; and
- the division between catalog refresh, module update, and shell update.

Current decisions and open questions are recorded only in the product
requirements, architecture, and accepted architecture decision records.
