# V24 capture source reconciliation — 2026-10-14

This is a current-source classification, not a test pass or V24 acceptance.
No unit suite, migrated SQL, object-store drill or camera fixture was run.

`apps/studio-agent/studio_agent/watchfolder.py` already traverses every
canonical watch-root component using `O_DIRECTORY|O_NOFOLLOW`, pins the root
device/inode at construction and returns `ESTALE` if it changes. Discovery and
file reading use descriptor-relative traversal; a single-link regular file is
required. `apps/studio-agent/tests/test_watchfolder.py` includes ancestor
symlink and root-replacement scenarios. The older statement in the 2026-10-10
recount that ancestor replacement remains open was superseded later in that
same recount. The tests are authored, not executed.

`backend/internal/upload/device.go` commits `device_begin_token` under the
locked upload row before `BeginCaptureMultipart`. A second Begin for the same
initializing upload returns `CAPTURE_BEGIN_RECONCILIATION_REQUIRED` without
another object-store call. The production object-store adapter binds the
tenant/upload staging key and the read-only discovery helper observes exactly
that key. Migrations 01960 and 01961 provide the claim column and privileged
settlement function; the SQL-backed lost-response and replay cases are
authored, not run. The operator must still establish quiescence of the original
Create before settlement; an empty listing alone is insufficient.

The remaining V24 completion work is current-build execution: migration/ACL
checks, SQL/object-store ambiguous-Begin and restart drill, real camera or
watch-folder fixture, credential rotation/revocation, bounded queue and
capture-to-QA client lifecycle. Code presence for the two boundaries above
must not be counted as any of those acceptance results.
