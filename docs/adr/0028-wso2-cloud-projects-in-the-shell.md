# ADR 0028: WSO2 Cloud Projects in the Shell

**Status:** Accepted

A WSO2 Cloud project groups an organization's work across products: an API
in API Management, an integration in Integration and an agent in Agent
Management all belong to one. Before this, each product module carried its own
project commands (`apim project`, `intg project`), each calling its product's
copy of the project list, and a project selected in the context reached only
the modules that chose to read it. This records five decisions.

## Projects are a platform concept the shell owns

`ws project list`, `use`, `current`, `create` and `delete` are shell
commands, and only on a WSO2 Cloud context; any other context refuses them
as a usage error, since no other deployment has a project service.

Projects belong to WSO2 Cloud's platform API service, the console's own
backend: it creates and deletes them, and every product only reads them. So
the shell calls that service itself, at `/admin/projects` under the
environment's product gateway, at a host derived from the environment file
like every product's. This is a deliberate exception to "the shell calls no
product API": the platform service is not a product, and a project spans
them all. A first-party `cloud` module was the alternative. It keeps the
boundary, but adds a module that must always be installed and a process hop
to every project command, for a concept the user already sees as the shell's.

## The platform access is derived, not recorded

A WSO2 Cloud context gets a platform access beside its products. It is the
resource server `urn:wso2:cloud` with the scopes `cloud:project:read`,
`cloud:project:create` and `cloud:project:delete`. It is held as a sibling
session under the key `cloud`, which is reserved: no module may take it as
its namespace.

Nothing is written to the context file: the access is derived from the
context's login whenever the context is WSO2 Cloud. It joins the context's
sessions, so `ws logout`, `ws org use` and `ws context delete` end it with
the rest. It is established the first time a project command needs it, from
the device secret when the sign-on issued one (ADR 0022), so no browser
opens. It is not authorized during `ws login`, so a user who never touches
projects never pays for it; a full `ws login` ends it instead, so the next
project command acts as the user, and with the role, that login signed in.

A role that lacks a scope is not refused at sign-in: ThunderID leaves the
scope out and still answers. The shell then refuses the command before
calling the service, or the service refuses the call, and either is an
authentication refusal (exit 77) saying the user's role does not allow the
action. A delete is refused this way before anyone is asked to confirm it. Today a Developer may do everything
but delete, and a Viewer may only read.

## The context records the selected project's id

The context's `project` field holds the id: the project's handle, which every
product API accepts. SDK v0.5.0 passes it to modules.

- `ws project list` pages through every project, 100 a page. It shows Name,
  ID, Pipeline and Description, marking the selected project.
- `ws project use <project>` takes an id or a display name and checks it
  against the list, so it needs the network. It stores the id. An unknown
  project is not found (exit 66), with `ws project list` in `next`.
- `ws project current` answers from the context alone.
- `ws project create <display-name>` derives the id the way the console
  does, unless `--id` names one. It takes `--description` and `--pipeline`
  (default `default`). It does not select the new project.
- `ws project delete <project>` asks at a terminal and takes `--yes`
  otherwise (ADR 0021). A project that still holds anything is refused by
  the service; that is a conflict (exit 73) carrying the service's reason.
  An accepted delete finishes in the background, so the command reports it
  as deleting without waiting. Deleting the selected project clears the
  context's `project` and says so in one line on standard error.

`ws org use` on a WSO2 Cloud context clears the selected project when the
organization changes, with one line saying so, since a project belongs to one
organization. With no project selected, each module
keeps its own behaviour.

## The product project groups give way

`apim project` and `intg project` are hidden for one release. They keep
working, but print one line on standard error naming the `ws project`
command, and are then removed. Integration's link from a project to a
repository (`intg project create --repo`) moves to `intg integration create`,
which already takes `--repo`. The `intg` module is maintained apart, so its
part is tracked in #296; the `apim` part lands on the preview branch.

## Environments wait

The platform service also manages environments and deployment pipelines.
They reuse the same client and access, but need decisions of their own (data
planes, quota, the protected default environment, a pipeline's references),
so they are a separate roadmap item. Members and invitations come after
them.
