# ADR 0005: OS Advisory Lock for Session Rotation

**Status:** Accepted

Refresh-token rotation must stay single-writer across concurrent `wso2`
invocations: a rotating issuer retires the token it was presented, so two
processes refreshing one session leave the store holding a token the issuer has
already replaced, and the next command refuses `auth.login_required`. The lock
that guarantees this is an OS advisory lock — `flock(2)` on Unix,
`LockFileEx` on Windows, reached through `golang.org/x/sys` behind a
`tryLock` seam. The kernel ties ownership to an open file descriptor and
releases it when the process exits however it exits, so an abandoned lock is
impossible by construction and there is no staleness to detect.

Two consequences look like defects and are not.

**The lock file is created once and never unlinked** — releasing closes the
descriptor and leaves the file in place. Removing it is the original bug in a
new costume: a waiter that already holds a descriptor to that inode would lock
a file no longer at the path, and a second process creating a fresh inode there
would lock successfully alongside it. The files are empty and bounded by the
number of credential references, so nothing accumulates that matters. Do not
add cleanup.

**A held lock reports `auth.login_required`**, not a distinct busy code,
because the stable problem-code list is closed and the recovery — retry the
command — is identical. Whether that list may grow is a separate open question
and not a property of the locking mechanism.

## Considered Options

- The original design made the lock the *existence* of a file, with an
  `O_EXCL` create to claim it and an age-based takeover so a crashed process
  could not block the reference forever. `O_EXCL` is atomic, but the takeover
  was `Stat` then `Remove` then create, with nothing atomic across the three:
  two processes that both saw the same abandoned file could each evict the
  other's live lock and both enter the critical section. No amount of
  repair fixes a heuristic whose job disappears entirely under a kernel lock.
- `github.com/gofrs/flock` covers both platforms behind one API and would have
  avoided a build-tagged seam, but it would add a genuinely new module to a
  graph that deliberately carried four direct dependencies at the time of this
  decision. `golang.org/x/sys` was already present as an indirect dependency at
  the version required, so promoting it to direct adds nothing to the graph and
  costs roughly thirty lines per platform.
- A blocking acquire would be simpler than a non-blocking attempt on a retry
  loop, but it discards the bounded wait: the shell reports a busy session as a
  typed, recoverable refusal rather than hanging on a peer that may never
  finish.

## Consequences

The waiting deadline moved from 5 to 45 seconds. Under the previous
implementation a waiter never truly blocked on a healthy holder, so 5 seconds
was never tested against a real critical section. That section now spans a
token refresh round trip to the issuer, which the `auth` package bounds at 30
seconds, so the waiter's patience has to stay strictly greater than that bound:
at equal values a holder that is merely slow outlives the waiter, and an
ordinary wait becomes a spurious refusal. The two constants live in packages
that cannot import each other, so the coupling is carried by a comment on each
— raising one means raising the other.

During an upgrade, an old binary and a new one do not interlock. The old one
takes the lock by creating the file and releases it by removing the file, which
the new one is holding a kernel lock on; once that file is gone, the next new
invocation creates a fresh inode at the same path and locks it successfully
alongside the first. The window is any machine where two CLI versions coexist —
a pinned install, a CI image mid-rollout — and it closes once the old binary is
gone. Nothing in the design can prevent it, because the old binary's behavior is
already fixed; it is recorded here so the double-entry is recognised as the
rollout artifact it is rather than a defect in this lock.

Advisory locks are unreliable on NFS-mounted filesystems. The state root is a
local user directory and a networked home directory is out of scope.
