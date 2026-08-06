# Context/identity model feasibility: prior art and WSO2 topology

**Status:** Research
**Research date:** 2026-08-04
**Scope:** Two independent questions raised against
[architecture §4.6-4.7](../architecture.md) and the "one identity, one
reusable login session; a context carries targeting only and names exactly
one identity; one identity may back several contexts; a context never mixes
authentication methods across products" model it codifies:

- **Question A** — does prior art from other multi-product/multi-service CLIs
  (AWS CLI v2, kubectl, gcloud, Azure CLI) support "one login backs many named
  targets," or does each of them tie a login 1:1 to a named target?
- **Question B** — does WSO2's real product topology support the mixed
  on-prem/cloud scenario the team is worried about ("IdP in on-prem, API
  Manager in on-prem, integration deployed in WSO2 Cloud using WSO2 Cloud's
  IdP"), or does that scenario architecturally force multiple identities no
  matter how the deployment is configured?

This document does not re-derive per-product/per-backend grant support,
which [wso2-authentication-landscape.md](wso2-authentication-landscape.md)
and [product-authentication-compatibility.md](product-authentication-compatibility.md)
already establish and which this document cites rather than repeats. It also
does not duplicate the general cloud-CLI architecture comparison in
[cloud-cli-comparison.md](cloud-cli-comparison.md), which covers auth/config
ownership at a higher level; this document goes one level deeper, into the
exact session-to-target binding mechanics that comparison did not need to
resolve.

**Source policy:** Only public primary sources are used: official product
documentation and public source code. Claims that could not be traced to a
primary source are marked "unknown from public sources." This is research
only; it makes no design recommendation and proposes no schema change.

## Question A: prior art for "one login, many named targets"

### A.1 AWS CLI v2 SSO sessions — SUPPORTS the model

An `[sso-session <name>]` block in `~/.aws/config` holds only
authentication-source facts (`sso_start_url`, `sso_region`,
`sso_registration_scopes`); any number of `[profile <name>]` blocks reference
it by `sso_session = <name>` and add only `sso_account_id` and
`sso_role_name` (targeting). AWS's own manual-configuration doc gives this
exact pattern:

```
[profile dev]
sso_session = my-sso
sso_account_id = 111122223333
sso_role_name = SampleRole

[profile prod]
sso_session = my-sso
sso_account_id = 111122223333
sso_role_name = SampleRole2

[sso-session my-sso]
sso_region = us-east-1
sso_start_url = https://my-sso-portal.awsapps.com/start
```

and states plainly: "This also allows `sso-session` configurations to be
reused across multiple profiles."
[Configuring IAM Identity Center authentication with the AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html)

One `aws sso login --profile dev` (or `--sso-session my-sso`) opens exactly
one browser/PKCE (or device-code, via `--use-device-code`) authentication;
the resulting IAM Identity Center session token is cached to disk under
`~/.aws/sso/cache/` keyed by the start URL/session name, and every profile
that names that `sso_session` draws temporary AWS credentials from it without
a further login: "As long as you are signed in to IAM Identity Center and
those cached credentials are not expired, the AWS CLI automatically renews
expired AWS credentials when needed." Only when the *IAM Identity Center*
session itself expires does the user need to `aws sso login` again — a
per-profile AWS credential expiring does not require re-login, it is silently
re-derived. `aws sso logout` "Successfully signed out of all SSO profiles"
in one action, confirming the session, not the profile, is the unit of login.
[Configuring IAM Identity Center authentication with the AWS CLI §"Sign in"](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html)

Mechanically, on the botocore side, `botocore/tokens.py` implements
`SSOTokenProvider`/`SSOTokenLoader`: the cache key is derived from the
`sso_session`'s `sso_start_url`/session name (not from the profile), tokens
are wrapped in a `DeferredRefreshableToken`, and refresh is time-window based
(`_advisory_refresh_timeout = 15 * 60`, `_mandatory_refresh_timeout = 10 * 60`)
— i.e. the token cache and its refresh logic are keyed on the session, and
every profile that names that session is just a different (account, role)
lookup performed against the same cached/refreshed session token.
[botocore/tokens.py](https://github.com/boto/botocore/blob/develop/botocore/tokens.py)

**Verdict: one login, many named targets**, and it is the closest existing
precedent to wso2-cli's identity/context split: `sso-session` ≈ identity,
`profile` ≈ context, `sso_account_id`/`sso_role_name` ≈ the context's
targeting fields. The legacy (pre-`sso-session`) profile-only SSO
configuration is explicitly deprecated for exactly the reason wso2-cli's
model cares about: "Automated token refresh isn't supported using the legacy
non-refreshable configuration. We recommend using the SSO token
configuration" — i.e. AWS's own history moved *toward* separating the
session from the target, not away from it.
[Configuring IAM Identity Center authentication with the AWS CLI §"Legacy IAM Identity Center configuration file"](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html)

### A.2 kubectl contexts — SUPPORTS the model

The kubeconfig schema keeps `clusters`, `users`, and `contexts` as three
independent named lists; a `Context` is a named reference triple, not an
inline credential:

```
Context.cluster: string (Required) — "Cluster is the name of the cluster for this context"
Context.user:    string (Required) — "AuthInfo is the name of the authInfo for this context"
Context.namespace: string           — "Namespace is the default namespace..."
```
[kubeconfig (v1) reference](https://kubernetes.io/docs/reference/config-api/kubeconfig.v1/)

The official multi-cluster-access tutorial demonstrates the identity/context
split directly, not merely permits it structurally: it creates one
`developer` user (one credential) and two contexts that both reference it —

```
kubectl config --kubeconfig=config-demo set-credentials developer \
  --client-certificate=fake-cert-file --client-key=fake-key-file

kubectl config --kubeconfig=config-demo set-context dev-frontend \
  --cluster=development --namespace=frontend --user=developer
kubectl config --kubeconfig=config-demo set-context dev-storage \
  --cluster=development --namespace=storage --user=developer
```

— the same `--user=developer` credential backing two named contexts that
differ only in targeting (here, namespace).
[Configure Access to Multiple Clusters](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)

The general kubeconfig concepts page defines a context only as the grouping
of "cluster, namespace, and user" and does not itself restate the
credential-reuse point with a second worked example, but the tutorial above
is unambiguous and is Kubernetes' own reference walkthrough for "multiple
clusters."
[Organize Cluster Access Using kubeconfig Files](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)

**Verdict: one login (one `user`/credential entry), many named targets
(`context` entries)** — structurally and by worked example. This is a weaker
precedent than AWS's for wso2-cli specifically because kubectl's "user" is a
static credential (cert, token, exec plugin), not an interactive login
session with its own refresh lifecycle — kubectl has no login command or
session concept at all, only credential *storage* reused across contexts.
It validates the shape of the split (identity separate from targeting,
n:1 from contexts to identity) but not the session/refresh mechanics that
wso2-cli also needs to validate against AWS.

### A.3 gcloud CLI configurations — SUPPORTS the model, with a caveat

A gcloud named configuration is "a named set of Google Cloud CLI properties,"
holding, among others, the active account and project as two independent
properties, not a fused identity. The configurations guide's own example
table shows one account reused across three configurations pointed at three
different projects:

```
NAME         IS_ACTIVE     ACCOUNT            PROJECT
default      False         user@gmail.com     example-project-1
project-1    False         user@gmail.com     example-project-2
project-2    True          user@gmail.com     example-project-3
```

and states configurations are useful to "use multiple projects: You can
create a separate configuration for each project and switch between them as
required," as a separate use case from "use multiple authorization
accounts" — i.e. the guide itself treats account-sharing-across-configs and
account-switching as two different, both-supported capabilities.
[gcloud CLI configurations](https://cloud.google.com/sdk/docs/configurations)

`gcloud auth login` establishes the credential (the account) independently
of `gcloud config configurations create/activate`, which is why the same
`user@gmail.com` account can appear in many configurations: authentication
and configuration are deliberately separate subsystems in gcloud, joined only
by the `account` property that a configuration happens to set.
[Authorize the gcloud CLI](https://cloud.google.com/sdk/docs/authenticate)

**Caveat, not a contradiction:** unlike AWS's `sso-session` block, gcloud's
`account` property is a plain reference to whichever credentialed account is
already cached locally (`gcloud auth login` populates a shared credential
store, not a per-configuration one); there is no dedicated
"session object with its own name" that configurations point at the way
`[sso-session]` blocks work. The n:1 (many configurations, one account) shape
is real and documented, but the underlying mechanism is closer to "every
configuration can name any already-authenticated account" than to AWS's
"named session block, multiple profiles reference it by name."

**Verdict: one login, many named targets**, confirmed by gcloud's own
generated example output, with a structurally looser identity/target
separation than AWS's.

### A.4 Azure CLI — SUPPORTS the model

`az login` authenticates once against a tenant and populates the list of
subscriptions ("Users are those accounts that sign in to Azure ... A user
might have access to several tenants and subscriptions") reachable from that
session; `az account set --subscription <id-or-name>` changes only the active
target and requires no further authentication:

> "Most Azure CLI commands act within a subscription. You can specify which
> subscription to work in using the `--subscription` parameter... If you
> don't specify a subscription, the command uses your current, active
> subscription."
>
> "Azure subscriptions have both a name and an ID. You can switch to a
> different subscription using `az account set`."

Switching subscriptions only changes tenant as a side effect when the target
subscription lives in a different tenant than the currently active one — an
explicit acknowledgment that tenant (≈ identity/authentication boundary) and
subscription (≈ context/target) are related but distinct axes: "If you
change to a subscription that's in a different tenant, you also change the
active tenant."
[How to manage Azure subscriptions with the Azure CLI](https://learn.microsoft.com/en-us/cli/azure/manage-azure-subscriptions-azure-cli)

**Verdict: one login, many named targets** (subscriptions), with the
documented exception that a subscription belonging to a foreign tenant
implicitly changes which tenant-level session is "active" — which is
consistent with, not contrary to, wso2-cli's rule that authentication-tenant
and target-organization are different fields: Azure's own docs draw exactly
that distinction between the tenant a login belongs to and the subscription
a command targets.

### A.5 Question A summary

| CLI | Session/credential unit | Target unit | n:1 (many targets, one login)? | Primary evidence |
|---|---|---|---|---|
| AWS CLI v2 | `sso-session` (named, cached, auto-refreshed) | `profile` (`sso_account_id`+`sso_role_name`) | **Yes**, explicit doc statement + worked example + botocore session-keyed cache | [cli-configure-sso.html](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html), [tokens.py](https://github.com/boto/botocore/blob/develop/botocore/tokens.py) |
| kubectl | `user` (static credential entry) | `context` (`cluster`+`user`+`namespace`) | **Yes**, worked tutorial example reusing one `user` across two `context` names | [kubeconfig.v1](https://kubernetes.io/docs/reference/config-api/kubeconfig.v1/), [multi-cluster tutorial](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/) |
| gcloud CLI | authenticated `account` (shared credential store) | `configuration` (`account`+`project`+…) | **Yes**, doc's own example table, looser binding mechanism | [gcloud configurations](https://cloud.google.com/sdk/docs/configurations) |
| Azure CLI | tenant-scoped login (`az login`) | `subscription` (`az account set`) | **Yes**, explicit doc statement, tenant/subscription distinction acknowledged | [manage-azure-subscriptions-azure-cli](https://learn.microsoft.com/en-us/cli/azure/manage-azure-subscriptions-azure-cli) |

**All four primary-source-verified CLIs validate "one login, many named
targets," none contradicts it.** None was found to tie a login 1:1 to a
named target as its primary/recommended model (AWS explicitly deprecated the
one-profile-one-credential legacy shape in favor of the shared-session
shape). The strongest and most mechanically-analogous precedent is AWS's
`sso-session`/`profile` split, which maps field-for-field onto wso2-cli's
identity/context split (named session with authentication facts; named
profile/context with only targeting facts and a reference to the session).
kubectl's precedent is real but weaker, because kubectl has no login/session
concept at all — its "identity" is a static, non-refreshing credential
record, closer to wso2-cli's `pat`/`client-credentials` adapter tier than to
its interactive-OIDC identity concept.

## Question B: does WSO2's real topology support cross-environment single-identity coverage?

### B.1 On-prem API Manager validating a different (cloud) IdP's tokens

WSO2 API Manager's Key Manager framework is explicitly multi-issuer: the
Gateway "provides an admin functionality for admins/tenant admins to
configure different authorization servers as Key Managers... supporting
multiple Key Managers for a given API," and for JWT access tokens
specifically, "it retrieves the Issuer details from the JWT and obtains the
relevant Key Manager. If the Key Manager is not enabled for the API, token
validation fails" — i.e. issuer-based routing to whichever configured Key
Manager matches, not validation against only the resident issuer.
[Key Manager overview](https://apim.docs.wso2.com/en/4.5.0/administer/key-managers/overview/)

APIM ships **named connectors** for WSO2 IS, WSO2 IS 7.x, Keycloak, Okta,
Auth0, Azure AD, PingFederate, and ForgeRock
([Configure WSO2 IS as a Key Manager](https://apim.docs.wso2.com/en/latest/administer/key-managers/configure-wso2is-connector/),
[Configure Okta as a Key Manager](https://apim.docs.wso2.com/en/latest/administer/key-managers/configure-okta-connector/),
[Configure Auth0 as a Key Manager](https://apim.docs.wso2.com/en/latest/administer/key-managers/configure-auth0-connector/)),
plus a generic escape hatch: **"Configure a Custom Key Manager"** requires
writing and deploying a Java connector implementing
`KeyManagerConnectorConfiguration`/extending `AbstractKeyManager`, then
registering it in the Admin Portal with standard OAuth/OIDC endpoint fields
(well-known URL, issuer, token, introspection, JWKS, revoke, userinfo, scope
management).
[Configure a Custom Key Manager](https://apim.docs.wso2.com/en/4.4.0/administer/key-managers/configure-custom-connector/)
This is generic enough in principle to describe any OIDC-compliant issuer,
Asgardeo included, but it is **not** point-and-click — it requires code, unlike
the named connectors.

**Asgardeo specifically has no dedicated Key Manager connector today.** The
WSO2 community's own IAM answer is direct on this point: "Currently, it is
not supported yet to set up Asgardeo as the Key Manager directly by using the
same approach that is used to set up Identity Server as Key Manager. However,
you can setup Asgardeo as IdP" — the documented workaround registers Asgardeo
as an **Identity Provider** (a different APIM concept from Key Manager, used
for federated login) with its JWKS endpoint and issuer, then uses **OAuth 2.0
Token Exchange** to convert an Asgardeo-issued access token into a token from
APIM's **resident** Key Manager, which is what the Gateway actually accepts
for API invocation.
[Can we setup Asgardeo as an external Key-manager for APIM?](https://iam4devs.wso2.com/all-discussions-45/can-we-setup-asgardeo-as-an-external-key-manager-for-apim-134)
This token-exchange grant is enabled by default on the APIM side "from
4.1.0 onwards," with the explicit caveat that "the corresponding grant type
is currently not supported by the WSO2 Identity Server" (i.e. this is an
APIM-side exchange mechanism the client calls, not IS/Asgardeo's own RFC 8693
implementation, which the existing
[product-authentication-compatibility.md §1.5](product-authentication-compatibility.md)
research already found to be inbound-federation-only on the Asgardeo/IS side).

**Bottom line for B.1:** an on-prem APIM instance *can* be configured to
accept access derived from a different (including cloud/Asgardeo) IdP's
login — either directly, via a named or custom Key Manager connector for any
IdP that issues standard JWTs with a discoverable JWKS, or, specifically for
Asgardeo today, via the documented Identity-Provider-plus-token-exchange
workaround. Neither path is automatic or assumption-safe: it requires an
operator to have deliberately registered the cloud IdP in APIM's Key Manager
(or Identity Provider) configuration, which is exactly the "property of the
running deployment, not of a configuration file" caveat architecture §4.6
already states. Where that registration has not been done — the common
case for an unmodified on-prem APIM install — APIM validates only its
resident/configured Key Managers, and a CLI cannot assume otherwise.

### B.2 On-prem Integration (Micro Integrator) validating a different (cloud) IdP's tokens

Two distinct surfaces exist and must not be conflated:

1. **MI's Management API** (what a CLI like `mi`/wso2-cli's `integration`
   module would call to manage/deploy artifacts). This research reconfirms
   the existing finding in
   [product-authentication-compatibility.md §2.4](product-authentication-compatibility.md):
   the Management API issues and validates its own internal JWT from MI's
   internal token store via basic-auth login at `/management/login`; the
   security-configuration doc for this API describes toggling authentication
   and authorization on/off and updating the internal token store, with no
   external-IdP acceptance option documented.
   [Secure the Management API](https://mi.docs.wso2.com/en/latest/install-and-setup/setup/security/securing-management-api/)

2. **MI-hosted integration artifacts** (the REST APIs/proxy services a
   deployed integration exposes at runtime — distinct from the CLI-facing
   Management API). Here the public documentation found in this round is
   thinner than for APIM: the Enterprise Integrator/Micro Integrator "Securing
   a REST API" example documents only a Basic Auth handler
   (`RESTBasicAuthHandler`) validating against MI's own connected user store
   by default, with a note that "you can use a custom basic auth handler or
   other security implementations."
   [Securing a REST API (EI/MI examples)](https://ei.docs.wso2.com/en/7.0.0/micro-integrator/use-cases/examples/rest_api_examples/securing-rest-apis/)
   No official MI documentation describing an OAuth2/JWT handler that
   validates tokens against an external, non-WSO2-resident issuer (JWKS-based
   or otherwise) for artifact-level security was found in this round.
   Whether such a handler exists and is simply undocumented at the page
   reached, or genuinely does not exist as a built-in option (as opposed to
   custom mediation code an integration developer would write themselves), is
   **unknown from public sources** — this needs either a source-code check of
   the MI/Synapse handler set or vendor confirmation, not more doc search.

**Bottom line for B.2:** no evidence was found, in either direction, that
on-prem Micro Integrator can be pointed at a cloud IdP's issuer as a built-in,
documented configuration option — for the Management API this is
affirmatively ruled out (internal-only, confirmed both in the prior research
round and here); for MI-hosted integration artifacts, the public docs
default to Basic Auth against MI's own user store and do not document (while
not explicitly ruling out) external-JWT validation as a first-class handler.
Integration is the weakest link in any cross-environment single-identity
story among the three products examined.

### B.3 Identity federation between on-prem IS and Asgardeo/WSO2 Cloud

Federation in the sense of "one IdP trusts another as an upstream login
option" is real and documented on both sides, but it answers a different
question than the one the team is worried about.

- **Asgardeo → external IdP (including an on-prem IS):** Asgardeo supports
  registering a "Standard-Based Identity Provider" (OIDC or SAML) as a
  federated login option — an application's login flow can redirect to that
  external IdP, and Asgardeo issues its own token after the federated login
  completes.
  [Add login with OIDC IdP](https://wso2.com/asgardeo/docs/guides/authentication/standard-based-login/add-oidc-idp-login/)
- **On-prem IS → Asgardeo:** symmetric capability exists on the IS side —
  IS's "Identity Providers" console lets an admin register Asgardeo (or any
  OIDC IdP) by JWKS endpoint and issuer, with "Federated Authenticators"
  enabling OAuth2/OIDC configuration for that connection, so an on-prem IS
  deployment can equally treat Asgardeo as its upstream login source.
  This pattern (IS registering an external OIDC IdP via JWKS endpoint under
  Identity Providers → Federated Authenticators) is corroborated by
  practitioner write-ups walking through the IS admin console steps, though
  a dedicated top-level WSO2 doc page titled exactly for "Asgardeo as a
  federated IdP in on-prem IS" was not separately located in this round; the
  general federated-authenticator mechanism itself is IS's own long-standing,
  officially documented capability
  ([Identity Federation with WSO2 Identity Server](https://wso2.com/identity-and-access-management/identity-federation/)).

**The critical distinction, and why federation does not by itself collapse
the identity count:** identity federation changes *where the human types
their password*, not *which issuer's token a resource server accepts
afterward*. If Asgardeo is configured to federate login through an on-prem
IS, a user who authenticates that way still receives an **Asgardeo-issued**
token as the end result — Asgardeo is still the token issuer the client
holds. An on-prem product that validates only its own resident IS issuer
does not thereby start accepting Asgardeo-issued tokens; that is a wholly
separate question, answered by B.1 (Key Manager / token-exchange
configuration), not by B.3 (federated login). The two mechanisms compose —
an operator could federate login *and* register the resulting IdP as a
trusted Key Manager on the on-prem product — but neither implies the other,
and neither is a wso2-cli-side capability: both are deployment-side
configuration the CLI can only discover, never assume.

### B.4 Bottom line for Question B

The scenario the team described — "IdP in on-prem, API Manager in on-prem,
integration deployed in WSO2 Cloud using WSO2 Cloud's IdP" — decomposes into
two sub-cases that this research treats separately, because they have
different answers:

1. **The cloud-deployed integration reaching WSO2 Cloud's own IdP** is not
   actually a cross-environment problem: a cloud-hosted product validating
   the cloud platform's own issuer is the ordinary, single-identity case
   (architecture §4.7's "everything in WSO2 Cloud" shape) — no federation or
   token exchange is needed for that leg by construction, because issuer and
   validator already agree.
2. **Whether that same cloud identity can *also* reach the on-prem API
   Manager (and, separately, on-prem IS/integration)** is the genuine
   cross-environment question, and the evidence is deployment-dependent, not
   categorically impossible:
   - **API Manager:** achievable today, but only through deliberate operator
     configuration — a named Key Manager connector (for IdPs with one),
     a custom Key Manager connector (for any OIDC-compliant issuer,
     including Asgardeo, at the cost of writing a connector), or, for
     Asgardeo specifically, the documented Identity-Provider-plus-
     token-exchange workaround. Absent that configuration, APIM validates
     only its own configured Key Managers and the cloud identity cannot
     reach it.
   - **Integration (Micro Integrator):** no built-in, documented path found
     either way; the Management API is affirmatively resident-only, and
     artifact-level security defaults to Basic Auth with no documented
     external-JWT option. Absent contrary evidence, on-prem MI should be
     assumed to need its own identity.
   - **Identity federation** (Asgardeo ⇄ on-prem IS) is real and documented
     in both directions but changes only where the login page redirects, not
     which issuer's tokens a downstream resource server accepts — it does
     not, by itself, let one identity reach products that still validate
     only the other environment's issuer.

**This is not a case where the CLI is architecturally forced into multiple
identities no matter what, nor one where a single identity trivially covers
it by default.** It is a case where the *deployment's own configuration*
decides the identity count, exactly as architecture §4.6 already states
("Whether a session can derive access for a product is a property of the
running deployment, not of a configuration file"). The evidence in this
round makes that statement concrete rather than aspirational: WSO2 API
Manager's Key Manager framework is real, general-purpose,
issuer-routed infrastructure built for exactly this kind of cross-issuer
acceptance — it is not hypothetical — while Micro Integrator currently
offers no comparable, documented mechanism. A wso2-cli deployment
description therefore cannot assume the mixed on-prem/cloud estate collapses
to one identity, cannot assume it stays at three, and must discover the
actual count per deployment the way the architecture already requires.

## Synthesis

**Question A is answered cleanly: yes, prior art supports the model.** Every
CLI examined — AWS CLI v2, kubectl, gcloud, Azure CLI — separates a
login/credential unit from a named-target unit and lets many named targets
share one login, and none of them was found to do the opposite as its
primary model. AWS CLI v2's `sso-session`/`profile` split is a
field-for-field precedent for wso2-cli's identity/context split (a named
session holding only authentication facts; named targets holding only
targeting facts plus a reference to the session), down to the
"legacy-shape-deprecated-in-favor-of-shared-session-shape" history that
mirrors wso2-cli's own reasoning for why the split matters. kubectl offers
the same shape with a weaker mechanism (a static, non-refreshing credential
rather than a session), which if anything argues that wso2-cli's richer,
session-based identity concept is the more sophisticated and more
appropriate version of a pattern every one of these tools converges on, not
a departure from precedent.

**Question B is answered as "deployment-dependent, with real infrastructure
on one side and a documentation gap on the other," not as a flat yes or
no.** The mixed on-prem/cloud scenario the team is worried about does not
categorically force multiple identities the way, say, a product with no
external-IdP acceptance mechanism at all would. WSO2 API Manager's Key
Manager framework is genuine, general infrastructure for exactly this
cross-issuer problem, with a real (if code-requiring) generic path and a
real (if indirect) documented Asgardeo-specific path. That means the
architecture's own framing — configuration records an operator's assertion
about what a session can reach, and a wrong assertion surfaces as a typed
authentication failure rather than a schema error — is not just a safe
hedge, it is the only correct framing: this research found genuine,
documented cases on both sides of "can one login reach it," differing by
product (APIM: yes-if-configured; MI: no evidence either way, default
posture is no) and by how much operator effort the cross-environment
configuration costs. **Nothing in this research argues for changing the
"one identity, many contexts" model itself** — if anything, both questions
reinforce that the model's key move (making reachability a per-deployment,
per-product fact that the broker discovers and the config merely asserts,
rather than something the schema hard-codes) is exactly the right amount of
flexibility for an estate where API Manager already ships general-purpose
cross-issuer infrastructure and Micro Integrator, today, does not.
