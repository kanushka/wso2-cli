# ADR 0006: Product Modules in One Repository with a Generated Catalog

**Status:** Accepted

Product module source moves into this repository. Modules keep independent
releases: each is tagged, built, and published on its own schedule, and the
shell still resolves and launches an out-of-process executable exactly as
before. What the move deletes is the cross-repository trust plumbing, not the
runtime. The three install, update, and invoke flows described in
`docs/architecture.md` §6.1 are unchanged by this decision, and §6.1 had
already rejected a registry service in favour of a static catalog served over
HTTPS. This ADR does not make independent module releases possible — that was
always the design. It makes them cheaper to operate.

The catalog stops being a curated artifact and becomes a build output. A
release job generates it from the tags that exist: `index.json` carries one
entry per namespace with the latest version per channel, sized to serve every
update check in a single request, and `modules/<namespace>.json` carries the
full version history with each version's channel, protocol range, shell range,
and per-platform URL, size, and digest. Selection still honours channel and
pin policy rather than maximum version, preserving the rule that the
numerically newest release is not assumed usable. With one repository and one
CODEOWNERS file, the question the old catalog existed to answer — is this
publisher authorized for this namespace? — is no longer asked, so `publisher`,
`signature`, `provenance`, `sbom`, and revocation leave the manifest along
with the catalog repository, its CI, and per-publisher signing keys.

What the single repository does not buy is atomicity of rollout. Everything
builds against the SDK at HEAD and continuous integration goes green, but that
proves only the HEAD-shell against HEAD-module cell. The cell that breaks a
user is an old installed shell against a new module — someone on an earlier
shell running an update. Source-level atomicity is not rollout-level
atomicity, and a green build describes this repository rather than the
population of installed shells. In separate repositories that skew is
unavoidable and therefore visible; here it is invisible until it reaches a
user. Two mechanisms replace the visibility that was lost. The shell declares
a protocol window of the current version and its predecessor, which
`Hello.protocol_versions` and `sdk/protocol.ParseVersions` already support
without new code, giving users a protocol generation of slack in which to
update. A release gate refuses to publish a module whose minimum protocol
exceeds what the current shell release speaks, which enforces the ordering
that the shell ships first. Alongside them, the shell must never compare a
module's version against its own: the launch gate is protocol range
intersected with platform, and nothing else. That is already how resolution
behaves, and this decision is what makes it load-bearing rather than
incidental.

The workspace rule that modules require the SDK by version, and never through
a committed `replace` directive, becomes load-bearing for the same reason. It
is what lets the conformance job drop the workspace and build a module against
a genuinely older SDK. Wiring the modules together with `replace` would make
that job unbuildable and would leave the protocol window unverified.

Two costs are accepted. Serving the catalog from the same origin as the
installer concentrates trust: whoever can publish there controls the update
channel for the shell and for every module. The exposure already exists for
the install script, but its blast radius grows, and it is mitigated by branch
protection and required review on the release and deployment workflows rather
than by cryptography. Artifact digests should not be read as more than they
are — a digest proves that an archive matches its manifest entry, not that the
manifest is authentic. Manifest signing remains a tracked follow-up. The
second cost is organizational and is not a technical decision at all: moving
product CLI source into this repository converts repository-boundary autonomy
into CODEOWNERS entries, a shared queue, and a shared merge process, which
requires the agreement of the teams that own those CLIs.

`wso2 bundle` and third-party module publishing are deferred, not cancelled.
Both would reintroduce a trust boundary this decision removes, and neither has
a consumer today.

## Considered Options

- Keeping product modules and the catalog in separate repositories preserves
  the ability to publish third-party and non-Go modules, and keeps each
  product team's release process behind its own repository boundary. It pays
  for that with a cross-repository trust boundary — publisher authority,
  per-publisher keys, an admission pipeline — that no module in scope needs,
  since every CLI being migrated is Go and Cobra and every one of them is
  WSO2-owned. The capability is worth buying when there is a publisher outside
  the organization to trust; there is not one yet.
- Linking the product modules into the shell binary would remove the catalog,
  the protocol, and the skew problem entirely, and would make every build
  genuinely atomic. It forfeits independent releases, which is the property
  the product teams are least willing to give up, and welds every product CLI
  into a single download that grows with each migration.
