# The amp product module (Agent Manager)

This is the WSO2 CLI product module for WSO2 Agent Manager, and the worked
example the module-author guide follows. It is a separate program: the `wso2`
shell resolves it from the managed module store and launches it, and the two
speak the module contract over the module's standard input and output.

It is deliberately small and read-only. Agent Manager's own `amctl` remains
the tool that creates and deploys agents; this module shows the complete path
from a scaffolded module, through brokered access, to a typed result against a
real product API, in two commands.

```sh
# From the repository root.
make build-module NAMESPACE=amp
make test-module NAMESPACE=amp
make install-module NAMESPACE=amp   # then ./bin/wso2 amp status
```

It must not import anything under the shell's `internal` tree, and it must
not print to standard output. Both are asserted by `internal/boundaries`.

## Commands

| Command | What it does |
| --- | --- |
| `wso2 amp status` | This module's version and endpoint, and what to run first. |
| `wso2 amp projects list [--org <organization>] [--limit <n>] [--offset <n>]` | The projects an organization holds, from `GET /api/v1/orgs/<org>/projects`. |
| `wso2 amp agents list --project <name> [--org <organization>] [--limit <n>] [--offset <n>]` | The agents a project deploys, from `GET /api/v1/orgs/<org>/projects/<project>/agents`. |

The organization is `--org` when given, otherwise the one the selected context
runs within (`wso2 org use`). Every result ends with the command to run next.

## How it is reached

Agent Manager is a resource server protected by a ThunderID the deployment
bundles at its own host (`http://thunder.amp.localhost:8080` in the local
compose deployment, beside an API at `http://localhost:9000`). The issuer is
therefore not derivable from the product URL, so this module declares no
product descriptor and `wso2 amp connect` is refused; the product is recorded
on an identity that logs in at that ThunderID:

```sh
wso2 iam connect http://thunder.amp.localhost:8080 --identity amp-dev \
  --audience http://localhost:9000
wso2 identity add-product amp-dev amp --endpoint http://localhost:9000 \
  --audience http://localhost:9000 --scopes amp:project:read,amp:agent:read
wso2 login
wso2 org use <organization>
wso2 amp projects list
```

Commands ask the shell for the `amp-api` audience and no scopes, so the shell
answers with exactly the scopes the `amp` product entry records, and that
entry is the ceiling. The audience value, the public client, and the scopes
above are read from the `wso2/agent-manager` repository and its local compose
deployment; they have not yet been measured against a running deployment,
and the module's tests drive it against a fake one.
