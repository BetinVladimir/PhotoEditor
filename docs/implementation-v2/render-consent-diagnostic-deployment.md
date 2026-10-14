# Render consent diagnostic installation

The render admission service requires forward-only migration `01590_v26_render_consent_diagnostic.sql`. It is authored but has not been applied or tested in the current implementation phase.

`photo.v26_render_model_consent_revoked` accepts tenant, project, source asset version, frozen root ID/SHA, learning version, model ID/SHA, actor and session. It returns a diagnostic boolean; it neither returns artifact metadata nor grants render admission. The function uses the existing authorized frozen-root scope and exact authored draft/model-use lineage before evaluating the private live-consent predicate. Unknown scope returns false. A false result does not authorize processing: the complete existing catalogue scope still runs.

The owner is `photo_fn_owner`, without superuser or BYPASSRLS privileges. The function fixes `search_path` to `pg_catalog,photo`. Only `photo_api` receives EXECUTE; PUBLIC, worker and scheduler must not inherit execution rights. Do not grant API execution on `v23_learning_version_consents_live` as a workaround.

Render startup checks the exact function signature, boolean return, owner, SECURITY DEFINER, search path and execution rights. An otherwise installed render service refuses startup if this diagnostic contract is missing or invalid. Deploy the migration before the updated API. Migration rollback is unsupported.

The catalogue maps a proven nonlive consent to HTTP409 `PROFILE_CONSENT_REVOKED`. Restore batch probes require that specific response. Generic422 `VALIDATION_FAILED` is rejected as evidence of consent refusal. Native SQL authority tests extend `tests/integration/render_model_scope_fixture.py`, invoked by `test_v23_training_freeze`; Go tests cover startup metadata and unknown scope, loader propagation and HTTP error mapping. None of these tests has been executed in this phase.

This diagnostic does not close concurrent revoke acceptance. A worker already running during revoke must still prove cancellation/adoption exclusion under current consent and exact attempt/fence, and the restore acceptance report must include that independent evidence before writers can be authorized.
