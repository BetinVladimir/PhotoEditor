"""Authored static SQL contract cases; execution deferred to test phase."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def test_copy_inventory_digest_has_own_operation_and_display_roots():
    sql = (ROOT / 'backend/db/migrations/02210_v27_shown_exposure_inventory_digest.sql').read_text()
    assert '"operation":"semantic.adopt_shown_exposure"' in sql
    for contract in ('ApprovedSemanticEdit', 'ExposurePreviewPresentation',
                     'ExposurePreviewNumericalEvidence', 'EncodedImage'):
        assert "'" + contract + "'" in sql
    assert "aid<=prior" in sql
    assert "params_count=1" in sql


def test_copy_params_bind_exact_canonical_receipt_without_granting_worker_authority():
    sql = (ROOT / 'backend/db/migrations/02211_v27_shown_exposure_params_binding.sql').read_text()
    for binding in ('preview_task_id', 'preview_result_id', 'preview_attempt',
                    'preview_fence', 'working_image', 'pixels', 'numerical_evidence',
                    'presentation', 'approved_edit', 'model_lock_sha', 'runtime_lock_sha',
                    'device_profile_sha'):
        assert "'" + binding + "'" in sql
    assert '2147483647::numeric' in sql
    assert '9223372036854775807::numeric' in sql
    assert 'aid=ANY(ids)' in sql
    assert 'RETURN coalesce(' in sql
    assert 'GRANT EXECUTE' not in sql
    assert 'INSERT INTO' not in sql
    assert 'UPDATE photo.' not in sql


def test_copy_inventory_is_exact_and_includes_presented_media():
    sql = (ROOT / 'backend/db/migrations/02212_v27_shown_exposure_exact_inventory.sql').read_text()
    for key in ('approved_edit', 'presentation', 'numerical_evidence', 'working_image',
                'pixels', 'before', 'after', 'affected', 'protected'):
        assert "->'" + key + "'" in sql
    assert "p_presentation->'params' IS DISTINCT FROM p_params" in sql
    assert "expected IS DISTINCT FROM p_inventory->'entries'" in sql
    assert 'count_entries<>unique_ids' in sql
    assert 'GRANT EXECUTE' not in sql


def test_copy_input_readiness_is_tenant_and_version_bound_with_media_limits():
    sql = (ROOT / 'backend/db/migrations/02213_v27_shown_exposure_input_objects_ready.sql').read_text()
    assert 'WHERE tenant_id=p_tenant AND id=aid' in sql
    assert "a.sha256 IS DISTINCT FROM ref->>'sha256'" in sql
    assert "a.object_version IN('','null')" in sql
    assert "a.media_type IS DISTINCT FROM 'image/png'" in sql
    assert '33882112' in sql and '2400016384' in sql
    assert 'aid<=prior' in sql
    assert 'GRANT EXECUTE' not in sql


def test_copy_transport_rebuilds_deterministic_manifest_and_never_accepts_preview_namespace():
    sql = (ROOT / 'backend/db/migrations/02214_v27_shown_exposure_transport_binding.sql').read_text()
    assert "p_params->>'child_run_id' IS DISTINCT FROM p_child::text" in sql
    assert "semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'" in sql
    assert 'semantic_shown_exposure_inventory_digest_ready' in sql
    assert "entries) IS DISTINCT FROM true" in sql
    assert "p_transport->'manifest' IS DISTINCT FROM manifest" in sql
    assert "p_transport->'chunks' IS DISTINCT FROM jsonb_build_array(chunk)" in sql
    assert "p_transport->'direct_roots' IS DISTINCT FROM roots" in sql
    assert 'semantic.preview_local_exposure' not in sql


def test_copy_transport_documents_bind_durable_publication_owner_and_scope():
    sql = (ROOT / 'backend/db/migrations/02215_v27_shown_exposure_transport_documents.sql').read_text()
    assert 'semantic_shown_exposure_transport_ready' in sql
    assert 'semantic_shown_exposure_input_objects_ready' in sql
    assert 'owner_id=task AND purpose=item.purpose' in sql
    assert 'run_id=p_document_run' in sql
    assert 'd.object_version IS DISTINCT FROM a.object_version' in sql
    assert 'd.bytes IS DISTINCT FROM a.bytes' in sql
    assert 'GRANT EXECUTE' not in sql


def test_copy_profile_binds_actual_pixels_and_cpu_costs():
    sql = (ROOT / 'backend/db/migrations/02216_v27_shown_exposure_profile_binding.sql').read_text()
    assert "p_image->'pixels' IS DISTINCT FROM p_params->'pixels'" in sql
    assert "p_profile->>'backend' IS DISTINCT FROM 'cpu'" in sql
    assert "(estimate->>'pixel_count')::bigint=pixels" in sql
    assert "(estimate->>'vram_bytes')::bigint=0" in sql
    assert '24*pixels+32768+16777216' in sql
    assert '12*pixels+16384+1048576' in sql
    assert '67108864+1' in sql
    assert '9007199254740991' in sql
    assert 'GRANT EXECUTE' not in sql


def test_copy_staging_cannot_alias_preserved_alpha():
    source = (ROOT / 'workers/photo_worker/operations/semantic/preview_adopt_stage.py').read_text()
    # Input pixels, copied pixels and new WorkingImage each reject an alias with
    # the preserved alpha child; a new stage ID does not imply distinctness.
    assert source.count('image.alpha.artifact_id if image.alpha else ref.artifact_id') == 3


def test_locked_copy_params_reauthorize_selected_preview_before_binding():
    sql = (ROOT / 'backend/db/migrations/02217_v27_shown_exposure_locked_params.sql').read_text()
    assert 'lock_semantic_preapply_preview_selection' in sql
    assert 'semantic_shown_exposure_params_bound' in sql
    assert "convert_to(p_body,'UTF8')" in sql
    assert 'FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=selection.preview_id' in sql
    assert 'TO photo_semantic_admission' in sql
    assert 'INSERT INTO' not in sql
    assert 'UPDATE photo.' not in sql


def test_copy_approved_edit_keeps_exact_intent_and_mask():
    sql = (ROOT / 'backend/db/migrations/02218_v27_shown_exposure_approved_edit_binding.sql').read_text()
    for field in ('ev', 'strength', 'mask', 'transform', 'source', 'candidate', 'intent'):
        assert "e->'" + field + "' IS DISTINCT FROM" in sql
    assert "convert_to(p_body,'UTF8')" in sql
    assert "p_intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb" in sql
    assert 'GRANT EXECUTE' not in sql


def test_copy_admission_documents_join_locked_receipt_hashes_and_live_qualification():
    sql = (ROOT / 'backend/db/migrations/02219_v27_shown_exposure_admission_documents.sql').read_text()
    for gate in ('lock_semantic_shown_exposure_params', 'semantic_shown_exposure_approved_edit_bound',
                 'semantic_shown_exposure_inventory_exact', 'semantic_shown_exposure_profile_bound',
                 'semantic_shown_exposure_transport_documents_ready', 'v27_downstream_profile_ready'):
        assert 'photo.' + gate in sql
    assert "convert_to(raw,'UTF8')" in sql
    assert 'a.bytes IS DISTINCT FROM octet_length(raw)::bigint' in sql
    assert "(image-'pixels') IS DISTINCT FROM (source-'pixels')" in sql
    assert "'semantic.adopt_shown_exposure','1','cpu'" in sql
    assert 'TO photo_semantic_admission' in sql
    assert 'INSERT INTO' not in sql
    assert 'UPDATE photo.' not in sql


def test_shown_decision_gate_preserves_original_publication_checks_and_locks_selection():
    sql = (ROOT / 'backend/db/migrations/02220_v27_shown_decision_batch_lock.sql').read_text()
    for gate in ('initialize_semantic_review', 'lock_semantic_decision_targets',
                 'semantic_decision_document_owner', 'lock_semantic_preapply_preview_selection'):
        assert 'photo.' + gate in sql
    assert "q->'preview' IS NOT DISTINCT FROM d->'preview'" in sql
    assert "IF selected ? 'preview'" in sql
    assert "ORDER BY value->>'target_id'" in sql
    assert "document.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex')" in sql
    assert 'CREATE FUNCTION photo.lock_semantic_shown_decision_batch' in sql
    assert 'CREATE OR REPLACE FUNCTION photo.lock_semantic_decision_batch' not in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_batch_nodes_require_exact_approval_cardinality_and_selection():
    sql = (ROOT / 'backend/db/migrations/02221_v27_shown_batch_nodes_binding.sql').read_text()
    assert "count(DISTINCT t->>'target_id')" in sql
    assert "WHERE d->>'decision'='approve'" in sql
    assert "e->'working_image' IS DISTINCT FROM decision->'preview'->'render'" in sql
    assert "e->'presentation' IS DISTINCT FROM decision->'preview'->'evidence'" in sql
    assert 'semantic_shown_exposure_transport_ready' in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_apply_shell_checks_full_batch_before_any_mutation_and_never_dispatches():
    sql = (ROOT / 'backend/db/migrations/02222_v27_shown_apply_draft_shell.sql').read_text()
    assert sql.index('semantic_shown_exposure_admission_documents_ready') < sql.index('INSERT INTO photo.runs')
    assert 'lock_semantic_shown_decision_batch' in sql
    assert 'semantic_shown_batch_nodes_bound' in sql
    assert '"shown_semantic"' in sql
    assert "'blocked','[]'" in sql
    assert 'changed<>jsonb_array_length(decisions)' in sql
    assert 'GRANT EXECUTE' not in sql
    assert 'INSERT INTO photo.outbox' not in sql


def test_shown_persisted_draft_rechecks_exact_config_tasks_and_decision_batch():
    sql = (ROOT / 'backend/db/migrations/02223_v27_shown_apply_persisted_draft.sql').read_text()
    assert "r.status IS DISTINCT FROM 'draft'" in sql
    assert 'r.config_snapshot IS DISTINCT FROM config_raw::jsonb' in sql
    assert 't.execution_profile IS DISTINCT FROM' in sql
    assert "t.operation IS DISTINCT FROM 'semantic.adopt_shown_exposure'" in sql
    assert 'JOIN photo.v2_review_batches batch' in sql
    assert 'batch.actor_id=p_actor' in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_quota_uses_persisted_profiles_and_never_renews_replay():
    sql = (ROOT / 'backend/db/migrations/02224_v27_shown_apply_quota.sql').read_text()
    assert 'semantic_shown_apply_draft_ready' in sql
    assert 'semantic_shown_exposure_profile_bound' in sql
    assert 'v27_downstream_profile_ready' in sql
    assert 'q.bytes IS DISTINCT FROM amount::bigint' in sql
    assert 'q.expires_at<=clock_timestamp()' in sql
    assert 'usage.storage_bytes::numeric+usage.reserved_bytes+amount' in sql
    assert 'UPDATE photo.quota_reservations' not in sql
    assert sql.index('IF FOUND THEN\n IF q.principal_type') < sql.index('INSERT INTO photo.quota_reservations')
    assert 'GRANT EXECUTE' not in sql


def test_shown_reserved_draft_cannot_commit_shell_when_quota_fails():
    sql = (ROOT / 'backend/db/migrations/02225_v27_shown_apply_reserved_draft.sql').read_text()
    assert sql.index('create_semantic_shown_apply_draft_shell') < sql.index('reserve_semantic_shown_apply_quota')
    assert 'reserved IS NULL OR reserved<1' in sql
    assert 'EXCEPTION WHEN' not in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_input_scope_preserves_other_link_scopes_and_requires_reserved_batch():
    sql = (ROOT / 'backend/db/migrations/02226_v27_shown_apply_input_scope.sql').read_text()
    for scope in ('semantic_apply_input_scope', 'semantic_preapply_preview_input_scope', 'semantic_shown_apply_input_scope'):
        assert 'photo.' + scope in sql
    assert "q.state='reserved'" in sql
    assert 'b.actor_id=r.created_by' in sql
    assert 'd.run_id=w.run_id AND d.owner_id=t.id' in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_input_registration_rejects_partial_replay_and_has_transport_ordinals():
    sql = (ROOT / 'backend/db/migrations/02227_v27_shown_apply_register_inputs.sql').read_text()
    assert 'reserve_semantic_shown_apply_quota' in sql
    assert 'semantic_shown_exposure_transport_documents_ready' in sql
    assert 'actual IS DISTINCT FROM expected' in sql
    assert "'InputClosureChunk',1000000" in sql
    assert "'InputClosureManifest',1000001" in sql
    assert 'DELETE FROM' not in sql
    assert 'ON CONFLICT' not in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_sealed_inputs_compare_registered_rows_profiles_and_ready_objects():
    sql = (ROOT / 'backend/db/migrations/02228_v27_shown_apply_sealed_inputs.sql').read_text()
    assert 'actual IS DISTINCT FROM expected' in sql
    assert "'v2_input_manifest',row->'transport'->'manifest'" in sql
    assert 'semantic_shown_exposure_input_objects_ready' in sql
    assert 'i.project_id IS DISTINCT FROM p_project' in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_sealer_requires_complete_registration_and_leaves_tasks_blocked():
    sql = (ROOT / 'backend/db/migrations/02229_v27_shown_apply_seal_inputs.sql').read_text()
    assert 'registered IS DISTINCT FROM expected' in sql
    assert 'SET inputs=sealed_inputs,execution_profile=sealed_profile' in sql
    assert "state='blocked' AND attempt=0" in sql
    assert 'semantic_shown_apply_sealed_inputs_ready' in sql
    assert "SET state='ready'" not in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_activation_requires_complete_seal_live_qualification_and_exact_quota():
    sql = (ROOT / 'backend/db/migrations/02230_v27_shown_apply_activation.sql').read_text()
    assert 'semantic_shown_apply_sealed_inputs_ready' in sql
    assert 'v27_downstream_profile_ready' in sql
    assert 'q.bytes IS DISTINCT FROM expected::bigint' in sql
    assert 'q.expires_at IS NULL' in sql
    assert 'changed<>jsonb_array_length(p_tasks)' in sql
    assert "graph_sealed=true,status='queued'" in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_finish_commits_activation_parent_receipt_and_event_together():
    sql = (ROOT / 'backend/db/migrations/02231_v27_shown_apply_review_finish.sql').read_text()
    assert "route='SemanticIntent.Apply'" in sql
    assert 'semantic_shown_batch_nodes_bound' in sql
    assert 'activate_semantic_shown_apply_child' in sql
    assert '9007199254740990' in sql
    for table in ('photo.idempotency_records', 'photo.audit_log', 'photo.run_events', 'photo.outbox'):
        assert table in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_atomic_commit_locks_receipt_before_mutation_and_grants_only_complete_entry():
    sql = (ROOT / 'backend/db/migrations/02232_v27_shown_apply_atomic_commit.sql').read_text()
    order = [sql.index('SELECT * INTO idem'), sql.index('batch:=photo.create_semantic_shown_apply_reserved_draft'),
             sql.index('sealed:=photo.seal_semantic_shown_apply_inputs'), sql.index('response:=photo.finish_semantic_shown_apply_review')]
    assert order == sorted(order)
    assert 'TO photo_semantic_admission' in sql
    assert 'EXCEPTION WHEN' not in sql
    source = (ROOT / 'backend/internal/semanticreview/preview_adoption_review_commit.go').read_text()
    assert source.index('idempotency.Use') < source.index('PrepareShownBatchCommitInputs')
    assert 'photo.commit_semantic_shown_apply_review' in source
    assert 'CreateSourceVerifiedExposureDraftInTransaction' not in source


def test_copy_runtime_uses_committed_batch_and_producer_fence_not_proposal_revision():
    sql = (ROOT / 'backend/db/migrations/02234_v27_shown_exposure_lease_admission.sql').read_text()
    for gate in ('semantic_shown_exposure_params_bound', 'semantic_shown_exposure_closure_ready',
                 'worker_authorized', 'v2_editor_authorized', 'v27_downstream_profile_ready'):
        assert 'photo.' + gate in sql
    assert "target.state='queued'" in sql
    assert 'producer.canonical_result_id::text IS DISTINCT FROM' in sql
    assert 't.worker_id IS DISTINCT FROM p_worker' in sql
    assert 'e->field IS DISTINCT FROM t.execution_profile->field' in sql
    assert 'semantic_preapply_preview_current' not in sql
    assert 'TO photo_worker' in sql


def test_copy_runtime_closure_rebuilds_sealed_direct_inventory_and_transport():
    sql = (ROOT / 'backend/db/migrations/02233_v27_shown_exposure_persisted_closure.sql').read_text()
    assert 'semantic_shown_exposure_transport_ready' in sql
    assert "i.project_id::text IS DISTINCT FROM r.config_snapshot->>'project_id'" in sql
    assert "'shown_semantic'->'nodes'" in sql
    assert 'direct.ordinal<>direct.expected' in sql
    assert "a.object_version IN('','null')" in sql


def test_copy_report_evidence_requires_own_operation_and_live_lease():
    sql = (ROOT / 'backend/db/migrations/02235_v27_shown_exposure_report_evidence.sql').read_text()
    assert 'photo.admit_semantic_shown_exposure' in sql
    assert "'semantic.adopt_shown_exposure','1','cpu'" in sql
    assert "'object_version',report.object_version" in sql
    source = (ROOT / 'workers/photo_worker/v2/shown_exposure_qualification.py').read_text()
    assert "operation='semantic.adopt_shown_exposure'" in source
    assert 'type(pin) is not ShownExposureQualificationPin' in source
