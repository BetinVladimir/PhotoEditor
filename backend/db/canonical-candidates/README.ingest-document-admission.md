# Scoped native2639 ingest repair — installed in named native test deployment

Current candidate: `02639_ingest_document_admission.sql`. `ingest_document_admission.forward.sql` remains the underlying unnumbered body patch and rollback-test artifact. Both are outside the embedded migrations set. The named local native test database is now2639 through Goose; general fresh/legacy installation remains unaccepted. The original2637 reference and rehearsal databases are retained.

R8 allocation (`work/canonical-migration-lineage-allocation-root-r8/plan.json`) supersedes R7: compatibility2638 stays; scoped ingest repair owns2639; the **uninstalled** broad billing/retention/purge integration moves to2640; additional33 remains an unnumbered suffix after2640. No native28/billing12 or applied ledger row is changed. All broad integration requirements remain required. Earlier partial2639 test receipts are historical; they did not accept the broad integration.

Migration00090 replaced backend-document functions and lost the ingest owner alternative from00049. Actual planning failed with document reservation stale after registering three immutable plans. The repair restores only unsealed ingest/same tenant/run/owner/params SHA admission, preserving all quota, fence, cancellation, status and other owner rules.

The SQL patches reserve_backend_document and inner complete_backend_document_pre_v28_donor. The public complete and pre_glare wrappers remain byte-identical. Native2638 exact ledger triples, PostgreSQL18, full body+metadata hashes of all four functions, raw ACL and effective EXECUTE for every existing role, and restricted NOLOGIN function owner are mandatory. Unknown lineage/role/ABI/body/ACL/ledger fails closed. Full pg_proc postconditions preserve every target field except the intended body changes. Down refuses with55000 INGEST_DOCUMENT_ADMISSION_FORWARD_ONLY.

Actual checks: original relational owner/fence matrix15PASS (`work/ingest-document-admission-fix-r1/rollback-result.json`); numbered guard matrix20PASS (`numbered-guard-result.json`); real GooseUp/noop/Down refusal on a new isolated2638 clone (`work/ingest-document-goose-numbered-r1/result.json`). Old ledger rows, unrelated functions and target metadata are preserved. Synthetic relational document version exists only inside rollback; no physical object or qualification is claimed.

Before main rollout: independently classify current native2637 against accepted provenance, review dependencies and source authority, drain all Expected producers/verifiers, coordinate compatibility2638 with Go nil omission and restart matching binaries, then apply2639 through Goose and resume the same ingest plans. General fresh/legacy installer and broad2640 acceptance remain separate open work. Matching these bounded guards is not general installation authority.


## Scoped rollout completed

`work/scoped-native2639-rollout-r1/result.json`: accepted native2637 re-observed after Expected producers/verifiers stopped, exact independent catalog/ledger match; independent globals audit and pinned-image binary comparison passed. Goose installed2638 and2639, preserved old ledger and unrelated functions, verified target metadata/bodies, repeated Up no-op. Down was not attempted on main. All deployed Go backend binaries were rebuilt from the promoted nil-MPS omission source, all seven running backend services use image sha256:d565a1e197fb07595f280772452fb92211d70e5e01589cd5b6e0f054331b23cc. API live/ready200. Scheduler remains stopped at mandatory genuine Render calibration gate. Source manifest and quiescence evidence are in the same directory.

R9 allocation records the named scoped installation, leaving broad2640 and fresh/legacy/general installer open. Actual resumed planning succeeded for the same6runs/12inspection tasks,6activation confirmations (`work/ingest-planning-live-r2/result.json`). This does not itself accept worker execution, catalog adoption, Render or the full photographer journey.
