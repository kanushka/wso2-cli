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
  graph that deliberately carries four direct dependencies. `golang.org/x/sys`
  was already present as an indirect dependency at the version required, so
  promoting it to direct adds nothing to the graph and costs roughly thirty
  lines per platform.
- A blocking acquire would be simpler than a non-blocking attempt on a retry
  loop, but it discards the bounded wait: the shell reports a busy session as a
  typed, recoverable refusal rather than hanging on a peer that may never
  finish.

## Consequences

The waiting deadline moved from 5 to 30 seconds. Under the previous
implementation a waiter never truly blocked on a healthy holder, so 5 seconds
was never tested against a real critical section; that section now spans a
token refresh round trip to the issuer, and a slow deployment would otherwise
turn an ordinary wait into a refusal.

Advisory locks are unreliable on NFS-mounted filesystems. The state root is a
local user directory and a networked home directory is out of scope.
