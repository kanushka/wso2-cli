# Research index

**Status:** Research index
**Last reviewed:** 2026-07-27

These files preserve the source material and research that informed the current
[product requirements](../product-requirements.md) and
[architecture](../architecture.md).

| File | Purpose |
| --- | --- |
| [Archived original proposal](archive/original-proposal.md) | Historical proposal retained for context; it is not authoritative. |
| [Public WSO2 CLI inventory](public-wso2-cli-inventory.md) | Inventory based only on publicly accessible WSO2 repositories and documentation. |
| [kubectl-krew.md](kubectl-krew.md) | Research into kubectl dispatch and Krew package management, including lessons adopted and gaps to improve. |
| [cloud-cli-comparison.md](cloud-cli-comparison.md) | Primary-source comparison of Azure CLI, AWS CLI, and Google Cloud CLI. |
| [module-architecture-options.md](module-architecture-options.md) | Evaluation of module-extension models and the recommended subprocess contract. |
| [root-cli-installation-distribution.md](root-cli-installation-distribution.md) | Evaluation of root CLI installation, update, and offline distribution options. |
| [shell-command-framework.md](shell-command-framework.md) | Comparison of the shell's own command dispatcher with Cobra, including the unknown-flag passthrough constraint. |

Research describes evidence and alternatives. Decisions and requirements belong
in `docs/architecture.md` and `docs/product-requirements.md`; when they differ,
those authoritative documents control. The current architecture proof is
bounded by the [first CLI vertical-slice plan](../plans/first-cli-vertical-slice.md).
