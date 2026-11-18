# ADR 0024: Sign-In and Status Report What the Provider Issued

**Status:** Accepted

This amends ADR 0022 for WSO2 Cloud, and records four decisions about what
`ws login`, `ws whoami`, `ws org` and `ws doctor` say, and what the broker
grants, once the identity provider has answered. Each one replaces a report
built from what the shell asked for with one built from what the provider
issued.

## The organization is read from the token

A ThunderID token is issued in one organization, which the person chooses
in the browser at sign-in, and names it in the `ouId`, `ouName` and
`ouHandle` claims. `ws login`, `ws whoami` and `ws org current` read the
organization from the stored login session's identity token, else its access
token, and fall back to the context's own `organization` only when no token
names one. The payload is read without checking the signature: the tokens
came from the provider in answer to this shell, and the value only describes
the session to the person who signed in. In JSON, `organization` is the
handle (else the id), beside `organizationId` and `organizationName`.

## Switching organization on WSO2 Cloud is signing in again

On a WSO2 Cloud context, `organization` names the ThunderID organization unit
its sessions are signed in to, by id. Every authorization for the context is
then sent to that organization's own endpoints: the discovered authorization
and token endpoints with `/ou/<id>` ahead of their paths. A session whose token names
another organization is refused with `auth.organization_mismatch` before it is
stored, and a session from the device secret that answers in another
organization is not stored, and the product is authorized in the browser
instead. The broker's home-tenant check, written for Asgardeo, does not apply.

`ws org use <organization>` on such a context records the organization, ends
every session signed in to the previous one, and runs the login. The handle,
name or id of the organization the stored session was issued in resolves to
its id; any other value is taken as an id. Under `--no-input` or
`WSO2_NO_INPUT` it records the organization, ends the sessions, and names
`ws login` as the step that finishes the switch. On any other ThunderID or
Identity Server context it is still refused with
`auth.organization_switch_unsupported`.

`ws org list` is refused with `shell.organization_list_unavailable`: nothing
lists the organizations a user belongs to. Whether the provider supports the
organization endpoint, and whether it will publish such a list, are open
questions to the platform team; the refusal goes when a list exists.

## The broker serves what the sign-in was granted

A product session records, beside the scopes its authorization asked for, the
scopes the provider issued (`granted`). ThunderID leaves out a scope the
user's roles do not carry and answers 200, and a refresh cannot add it. On a
resource-bound session established for its product's own resource (`direct`
or `sibling`):

- A module request naming no scope asks for whatever the record allows, and
  is narrowed to what was granted rather than refused for the scopes that
  were not.
- A request naming a scope the sign-in asked for and was not granted is
  refused with `auth.scope_not_granted`, naming the scope and an
  administrator who can give the user a role carrying it. The broker does not
  authorize the product again first, since a second sign-in returns the same
  answer, and the refusal says that signing in again will not help until the
  role is granted.
- A request naming a scope the record does not carry is refused, as before,
  with `auth.product_not_configured`, now naming the scope and the sign-in
  that asks for it once it is recorded: there, signing in again does help.

A session stored before `granted` existed is renewed and proved as before.
`ws login` lists the ready products, marking each with the scopes it went
without, and ends with a `Next` line.

## Doctor fails when nothing can run

`ws doctor` fails the session check, with `auth.login_required` and exit class
77, when the selected context has no login session. This reverses the
decision that a logged-out context is reported as `none` and exits 0: no
product command can run without the login session, and a status that passes
then is not one a script can trust. Product records without a session of
their own, while the login session stands, are still `none`, because each is
established when a command first needs it.

The `defaults` finding stops being permanent where the difference is
deliberate. A scope list stated empty (`scopes: []`) is kept apart from an
absent one in the context document and is not drift, and the grant a WSO2
Cloud context sets aside (ADR 0022) is not drift. The finding's commands move
to its recovery, which is renamed to the binary's name; its detail is data and
is not.

## Considered Options

- **Keep refusing a scopeless request when the provider withheld a scope.**
  It is what the narrowing proof did before, and it made every command fail
  for a user missing any one of a product's scopes, after opening a browser
  to ask again.
- **Fill in the missing scope with a second authorization.** ThunderID
  answers it the same way, so it only costs a browser tab.
- **Record the Cloud organization in a new login member.** The context
  already has `organization`, and on WSO2 Cloud it means exactly the
  organization the context acts in; a second member would let the two
  disagree.
- **Keep the organization's handle on the context.** The authorization
  endpoint takes the id, and nothing resolves a handle the shell has not
  already seen in a token.
