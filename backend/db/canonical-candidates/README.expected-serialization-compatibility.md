# Canonical 02638 Expected serialization compatibility — CANDIDATE ONLY

Installation is **not permitted**. This file is deliberately outside `migrations/*.sql`, the sole pattern embedded by `backend/db/migrations.go`. No installer imports this directory. No Go serialization change is promoted by this candidate.

The candidate is restricted to the separately approved native 2637 lineage. It is not a fresh-install or legacy-billing migration. Allocation is described by `work/canonical-migration-lineage-allocation-root-r6/plan.json`: native 28 migrations and billing 12 migrations retain their allocation; compatibility is 2638 and integration is 2639. A version ledger alone cannot establish full schema lineage. The complete semantic before/after classifier and installation authority remain pending; matching this candidate's bounded guards does not grant installation approval.

## Exact predecessor evidence

`work/native2637-ledger-readonly-witness-r1/ledger.json` (SHA256 `9fc110ca4fdfe840361ac1bbfa2d63e39b68ed7fa60d846e7893e1d9adc6d456`) is the root's read-only witness from the restored, accepted native2637 snapshot. Its companion result binds the accepted export, dump and postreview receipts. It contains 2636 unique applied rows, versions 0 through 2637 **excluding 196 and 2499**. The source build replays the original accepted2625 version prefix and independently matches the actual2637 post-upgrade exact triples. It does not infer a contiguous migration prefix.

The SQL takes a SHARE lock on `public.goose_db_version`, then compares all `[id,version_id,is_applied]` triples ordered by id, with NULL-safe `IS DISTINCT FROM`. Missing, duplicate, unapplied, changed-id and additional rows fail with `EXPECTED_SERIALIZATION_NATIVE2637_LEDGER_MISMATCH` / SQLSTATE 55000. It never inserts, edits or adopts ledger rows. Fixed row ids intentionally restrict the candidate to the witnessed lineage; they are not a portable general classifier.

## Function contract

The exact reviewed forward patch SHA256 is `538bfe698eebe7b996b0bcf18625a99c3521f83087fc54794fb0b535dcb2d3db`. Its function-body patch and guard predicates are retained unchanged after the ledger gate. The R2 source successor contains its search-path change inside the rollout DO block and restores the caller path before returning; the exact source inverse is recorded in the controls packet. It verifies the exact predecessor function body (`c2bfc9613ecf1b7077511d38c18ffced9a5785ad048415dea0800f52393f2d8b`), symbolic argument/return types, complete declared ABI attributes, owner NOLOGIN, SECURITY DEFINER, configuration, raw owner-only ACL and lack of effective execution for all eight required runtime roles. Replacement uses the server's complete function definition, then verifies every `pg_proc` field except `prosrc` unchanged and the exact replacement body (`275490ace08a797fa5a909b0e278bedc8789603ac8d4c3495dd7be9aeb086a78`). Full global effective-ACL comparison is not claimed.

The sole body change removes the `MPSResources` expected key when the source policy is absent or JSON null. Non-null MPS validation and exact expected-object equality stay intact. It is coordinated with a future, separately approved Go nil-field omission rollout.

## Execution boundary and rollback

Goose Up is one ordinary transaction, with StatementBegin/StatementEnd. The rollout DO captures the caller search_path, sets a transaction-local protected `pg_catalog, pg_temp` path, and restores the captured value before return, so Goose can subsequently write its unqualified ledger relation. Exceptions abort the migration transaction and its local settings. Do not use NO TRANSACTION or run extracted statements in autocommit. The normal Goose ledger insertion, if a future approved installer executes this candidate, belongs to that transaction; this source does not perform one. Down always raises `EXPECTED_SERIALIZATION_FORWARD_ONLY` / SQLSTATE 55000, without changing the body or ledger. Recovery requires a separately reviewed forward migration.

Offline parser and source controls are in `work/qualification-compatibility-candidate-source-controls-r2`. They are not PostgreSQL runtime, fresh-chain acceptance, SQL behavior, deployment or complete portability evidence.
