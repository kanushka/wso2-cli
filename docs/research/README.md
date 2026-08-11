# Research index

**Status:** Research index
**Last reviewed:** 2026-08-06

These files preserve the source material and research that informed the current
[product requirements](../product-requirements.md) and
[architecture](../architecture.md).

| File | Purpose |
| --- | --- |
| [Archived original proposal](archive/original-proposal.md) | Historical proposal retained for context; it is not authoritative. |
| [Public WSO2 CLI inventory](public-wso2-cli-inventory.md) | Inventory based only on publicly accessible WSO2 repositories and documentation. |
| [choreo-cli-installation-distribution.md](choreo-cli-installation-distribution.md) | Primary-source research into Choreo CLI's install-script mechanics, release artifact conventions, Windows support, and the shared distribution setup it turned out to have with `wdp-cli`. |
| [kubectl-krew.md](kubectl-krew.md) | Research into kubectl dispatch and Krew package management, including lessons adopted and gaps to improve. |
| [cloud-cli-comparison.md](cloud-cli-comparison.md) | Primary-source comparison of Azure CLI, AWS CLI, and Google Cloud CLI. |
| [module-architecture-options.md](module-architecture-options.md) | Evaluation of module-extension models and the recommended subprocess contract. |
| [root-cli-installation-distribution.md](root-cli-installation-distribution.md) | Evaluation of root CLI installation, update, and offline distribution options. |
| [shell-command-framework.md](shell-command-framework.md) | Comparison of the shell's own command dispatcher with Cobra, including the unknown-flag passthrough constraint. |
| [wso2-authentication-landscape.md](wso2-authentication-landscape.md) | Primary-source survey of Asgardeo, WSO2 Identity Server, and Thunder authentication capabilities, the `apictl` and `amctl` login implementations, and a gap analysis against the planned login methods. |
| [product-authentication-compatibility.md](product-authentication-compatibility.md) | Sufficiency verdicts for each planned login method with gap ownership, per-product authentication paths across cloud and on-premises deployments, whether one login session can serve multiple modules in one context, and the standing backend ask for a seeded `wso2cli` public client. |
| [context-identity-model-feasibility.md](context-identity-model-feasibility.md) | Prior art and WSO2 topology behind the one-identity-per-context model that architecture §4.6-4.7 codifies. |
| [asgardeo-redirect-uri-and-scope-narrowing.md](asgardeo-redirect-uri-and-scope-narrowing.md) | Whether Asgardeo accepts any-port loopback redirect URIs and honors a narrower scope on the refresh grant. Section 3 carries the empirical verdict cells, section 3.1 the same questions against Identity Server 7.3.0, and section 4 says how a live run produces and records them. |

Research describes evidence and alternatives. Decisions and requirements belong
in `docs/architecture.md` and `docs/product-requirements.md`; when they differ,
those authoritative documents control. The current architecture proof is
bounded by the [first CLI vertical-slice plan](../plans/first-cli-vertical-slice.md).
