# ADR 0025: A WSO2 Cloud Service Account Signs In Only When Named

**Status:** Accepted

Automation cannot complete a browser sign-in, and WSO2 Cloud's identity
provider, ThunderID, does not yet grant the shell's public client the device
code grant. Until it does, CI reaches WSO2 Cloud as a service account: a
client-credentials application created in the WSO2 Cloud console, which holds
roles of its own. This records how the shell signs in as one.

## Named on the command line, read from the environment

`ws login --service-account` signs in as the account `WSO2_CLIENT_ID` and
`WSO2_CLIENT_SECRET` hold, in the environment `WSO2_CLOUD_ENV` names. A plain
`ws login` never does, with or without a terminal, even when the variables are
set; it says on standard error that they were ignored. A developer's machine
that carries CI's variables would otherwise sign in as the wrong identity, and
nothing the shell switches to on its own is something a person can see coming.

The secret is never taken on the command line, where process listings and
shell history keep it. The context records the name of its variable, as every
`client-credentials` context does, and the client id, which is not a secret.

## One token per product, nothing stored

Each product is reached by a client-credentials grant of its own, naming the
product's resource server and asking for its own scopes, as ADR 0022 has each
product authorized on WSO2 Cloud. One token for API Management, Integration
and Agent Management, which share `urn:wso2:cloud`, would hand each module the
others' authority. One token exchanged per product is ruled out by ADR 0022's
measurement: an exchanged token carries no permission scope.

The grant needs no person, so nothing is stored: each command gets its own
tokens, as any `client-credentials` context does, and there is nothing to
renew or revoke at logout.

## The login checks the account and records what it holds

The login gets every product's token before it writes anything. ThunderID
leaves out, with a success, the scopes the account's roles do not carry, so
the login records each product's granted scopes, and names on standard error
what was left out: a later command asking for more is refused there, naming
what it lacks. An account granted nothing for any product, or whose tokens
name no organization (the `ouId` claim), is refused and nothing is written.

The organization the tokens name is recorded as the context's organization
and as its login's home tenant: the account is registered in that
organization and every token it is issued is issued there.

## A person sets it up with the same command

On a terminal, `ws login --service-account` asks for whatever is not set,
the secret without echo, checks it the same way, and prints the variables CI
needs, the secret's as a name only, and the one command CI runs:
`ws login --service-account --no-input`. When it asked for anything it writes
no context, because one naming variables this machine does not set would
serve no command here.

## Considered Options

- **Picking the service account up from the environment on any login.** It
  saves CI one flag, and signs a developer's machine in as CI.
- **A token per command shared across products at `urn:wso2:cloud`.** Fewer
  requests, at the cost of every module carrying every product's authority.
- **Registering the account from the shell.** ThunderID's client registration
  requires the deployment-wide `system` permission, which a person's token
  does not carry.
- **The secret in a file.** Useful where secrets are mounted as files; left
  for later, since every CI system can set a variable.
