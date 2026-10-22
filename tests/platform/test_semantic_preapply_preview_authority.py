from pathlib import Path


def test_preapply_preview_current_gate_preserves_live_scope_without_apply_mutations():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/02169_v27_preapply_preview_current_authority.sql').read_text()
    for clause in ["SECURITY DEFINER SET search_path=pg_catalog,photo",'semantic_review_identity',"r.status='waiting_review'",'r.revision=p_revision','target.revision=p_target_revision',"target.state='proposed'",'target.child_run_id IS NULL','target.batch_id IS NULL','semantic_review_plan_sources','plan_count=1 AND candidate_count=1','TO photo_api']:
        assert clause in sql
    assert 'INSERT INTO' not in sql and 'UPDATE photo.' not in sql and 'DELETE FROM' not in sql
    assert '-- +goose Down' not in sql


def test_preview_worker_admission_requires_separate_lease_and_live_editor_scope():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/02170_v27_preapply_preview_worker_admission.sql').read_text()
    for clause in ["r.kind<>'preview'",'semantic.preview_local_exposure',"t.state<>'running'",'t.fencing_token IS DISTINCT FROM p_fence','db_principal=session_user',"config->'params' IS DISTINCT FROM e",'semantic_preapply_preview_current','semantic_review_plan_sources','v27_downstream_qualification_pin_ready','r.created_by IS DISTINCT FROM actor','TO photo_worker']:
        assert clause in sql
    assert 'INSERT INTO' not in sql and 'UPDATE photo.' not in sql and '-- +goose Down' not in sql


def test_preview_protection_origin_uses_proposal_not_approved_child():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/02171_v27_preapply_preview_protection_origin.sql').read_text()
    for clause in ['admit_semantic_exposure_preview',"edit->'protection' IS DISTINCT FROM",'producer.canonical_result_id',"producer.operation='semantic.transfer'",'target_protection',"input.contract='MaskRef'",'semantic_protection_producer_ready','TO photo_worker']:
        assert clause in sql
    assert 'admit_semantic_local_exposure' not in sql and '-- +goose Down' not in sql


def test_preview_report_evidence_requires_live_preview_admission_and_cpu_policy():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/02172_v27_preapply_preview_report_evidence.sql').read_text()
    for clause in ['admit_semantic_exposure_preview','v27_downstream_profile_ready',"'semantic.local_exposure','1','cpu'",'v2_qualification_policies',"'object_version',report.object_version",'TO photo_worker']:
        assert clause in sql
    assert '-- +goose Down' not in sql


def test_preview_publication_lock_is_private_and_source_fenced():
    from pathlib import Path
    body=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02173_v27_preapply_preview_publication_lock.sql').read_text()
    for clause in ('SECURITY DEFINER SET search_path=pg_catalog,photo',
        'photo.lock_semantic_decision_publication', 'photo.semantic_preapply_preview_current',
        'FOR UPDATE', "preview.status<>'draft'", 'preview.graph_sealed',
        "config->'params' IS DISTINCT FROM e", "config->>'params_sha' IS DISTINCT FROM p_sha",
        "config->>'session_id' IS DISTINCT FROM p_session::text", 'intent.intent_sha',
        'source.artifact_id::text', 'OWNER TO photo_fn_owner'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body
    assert 'INSERT INTO' not in body and 'UPDATE photo.' not in body


def test_preview_document_tickets_reauthorize_and_account_quota():
    from pathlib import Path
    body=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02174_v27_preapply_preview_documents.sql').read_text()
    assert body.count('p_run:=photo.lock_semantic_preapply_preview_publication(')==2
    for clause in ("'semantic.preview_local_exposure:'", "THEN 'Params' ELSE p_purpose END",
        "p_sha IS DISTINCT FROM p_params_sha", 'octet_length(p_body)::bigint',
        'SET token=fence', 'd.token IS DISTINCT FROM p_token',
        'used+p_bytes>capacity', 'reserved_bytes=reserved_bytes+p_bytes',
        'reserved_bytes=reserved_bytes-d.bytes', 'storage_bytes=storage_bytes+d.bytes',
        "'replayed'", "'conflict'", "'quota'", "a.object_version NOT IN('','null')"):
        assert clause in body
    assert body.count('TO photo_api;')==2
    assert 'TO photo_worker;' not in body and 'TO photo_scheduler;' not in body
    assert 'UPDATE photo.semantic_targets' not in body and 'INSERT INTO photo.tasks' not in body


def test_saved_preview_is_authenticated_frozen_draft_and_replayable():
    from pathlib import Path
    body=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02175_v27_preapply_preview_saved_run.sql').read_text()
    for clause in ('photo.lock_semantic_decision_publication', 'photo.semantic_preapply_preview_current',
        'photo.semantic_review_identity', 'source.artifact_id::text', 'intent.intent_sha',
        "'semantic-preapply-preview:'", "':'||p_sha||':'||p_actor::text||':'||p_session::text",
        "'params',e,'params_sha',p_sha,'actor_id',p_actor,'session_id',p_session",
        "preview.config_snapshot=config", "'preview','draft'", 'OWNER TO photo_fn_owner', 'TO photo_api;'):
        assert clause in body
    assert 'INSERT INTO photo.tasks' not in body and 'UPDATE photo.semantic_targets' not in body
    assert 'TO photo_worker;' not in body and 'TO photo_scheduler;' not in body


def test_preview_catalog_is_separate_and_not_enabled_without_adoption():
    from pathlib import Path
    body=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02176_v27_preapply_preview_operation_catalog.sql').read_text()
    assert "'semantic.preview_local_exposure','1','cpu','cpu.semantic_preview_local_exposure',NULL,1800,'[\"cpu\"]',false" in body
    for role in ('preview_evidence','working_image','pixels'):
        assert f"'semantic.preview_local_exposure','1','{role}'" in body
    assert 'UPDATE photo.operation_registry' not in body
    assert "'semantic.local_exposure'" not in body


def test_preview_quota_binds_source_profile_and_reserved_result_budget():
    from pathlib import Path
    body=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02177_v27_preapply_preview_quota.sql').read_text()
    for clause in ('photo.lock_semantic_preapply_preview_publication',
        "e->'source'->'source'->>'sha256'", 'bytes=octet_length(p_source)',
        "image->>'dtype' IS DISTINCT FROM 'float32'", 'pixels NOT BETWEEN 1 AND 200000000',
        '512*pixels+67108864+1', '32*pixels+4*16384+16777216', '12*pixels+16384+2*1048576',
        'photo.v27_downstream_profile_ready', 'frozen IS DISTINCT FROM p_profile',
        "old.state='reserved'", 'old.bytes=budget', 'old.expires_at>clock_timestamp()',
        'usage.storage_bytes::numeric+usage.reserved_bytes+budget>capacity',
        'SET reserved_bytes=reserved_bytes+budget', 'SET v27_preapply_preview_profile=p_profile'):
        assert clause in body
    assert 'INSERT INTO photo.tasks' not in body
    assert 'TO photo_api;' in body and 'TO photo_worker;' not in body


def test_preview_quota_release_waits_for_all_live_owners():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02529_semantic_preview_lifecycle_quota.sql').read_text()
    for clause in ('r.v27_preapply_preview_profile IS NULL',
        "r.status NOT IN('completed','completed_with_errors','failed','cancelled')",
        "t.state NOT IN('succeeded','failed','cancelled','skipped')",
        "u.state NOT IN('ready','cleaned')", "d.state NOT IN('ready','cleaned')",
        'q.principal_id IS DISTINCT FROM r.created_by', 'reserved_bytes>=q.bytes',
        "SET state='released',bytes=0", 'ORDER BY r.created_at,r.id LIMIT 1'):
        assert clause in body
    assert 'TO photo_scheduler;' in body and 'TO photo_api;' not in body
    assert 'DELETE ' not in body
    runtime=(root/'backend/internal/workflow/render_derivative_cleanup.go').read_text()
    startup=(root/'backend/internal/workflow/service.go').read_text()
    assert 'photo.release_next_semantic_preapply_preview_quota()' in runtime
    assert 'photo.release_next_semantic_preapply_preview_quota()' in startup


def test_stale_preview_cancellation_rechecks_live_fences_before_mutation():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02179_v27_preapply_preview_stale.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_current',
        'NOT photo.semantic_preapply_preview_run_current(x.tenant_id,x.id)',
        "x.status IN('draft','queued','running')", "interval '1 hour'",
        'q.expires_at<=clock_timestamp()', 'ORDER BY x.created_at,x.id LIMIT 1',
        'IF photo.semantic_preapply_preview_run_current(r.tenant_id,r.id)',
        "SET cancel_requested=true,status='cancelling',revision=revision+1",
        "state NOT IN('succeeded','failed','cancelled','skipped')"):
        assert clause in body
    assert body.index('id=parent FOR UPDATE') < body.index("id=(e->>'target_id')::uuid FOR UPDATE") < body.index('id=candidate.id FOR UPDATE')
    assert 'DELETE ' not in body and 'reserved_bytes=' not in body
    runtime=(root/'backend/internal/workflow/render_derivative_cleanup.go').read_text()
    assert runtime.index('photo.cancel_stale_semantic_preapply_preview()') < runtime.index('photo.release_next_semantic_preapply_preview_quota()')


def test_preview_preparation_composes_existing_authority_and_publication():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/internal/semanticreview/preview_preparation.go').read_text()
    ordered=('CompileExposurePreviewParams(', 'ResolveExposurePreviewDocuments(',
        'CompileExposurePreviewResources(', 'CheckCurrentPreviewAuthority(',
        'SaveExposurePreview(', 'CompileExposurePreviewTask(', 'ReserveExposurePreviewQuota(',
        'NewExposurePreviewPublisher(', 'PublishExposurePreviewTask(', 'VerifyExposurePreviewPublication(')
    positions=[body.index(value) for value in ordered]
    assert positions==sorted(positions)
    assert body.count('CheckCurrentPreviewAuthority(')==2
    assert 'ApprovedEdit{' not in body
    assert 'append([]byte(nil), input.Source.Documents.Source...)' in body


def test_preview_inventory_digest_is_operation_specific_sorted_and_bounded():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02180_v27_preapply_preview_inventory_digest.sql').read_text()
    for clause in ('BETWEEN 1 AND 512', "'SemanticPlan'", 'aid<=prior',
        'params_count=1', 'ref->>\'sha256\'<>p_params_sha',
        '"operation":"semantic.preview_local_exposure"',
        "encode(sha256(convert_to(body,'UTF8')),'hex')=p_entries_sha", 'OWNER TO photo_fn_owner'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body
    assert 'INSERT INTO' not in body and 'UPDATE photo.' not in body
    assert '"operation":"semantic.local_exposure"' not in body


def test_preview_transport_reconstructs_chunk_and_manifest_and_requires_roots():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02181_v27_preapply_preview_transport_consistency.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_inventory_digest_ready',
        "p_transport->'direct_roots' IS DISTINCT FROM roots", "jsonb_array_length(p_transport->'chunks')<>1",
        "photo.semantic_uuid_v5(task,'v2_input_chunk_0')", "photo.semantic_uuid_v5(task,'v2_input_manifest')",
        "p_transport->'manifest' IS DISTINCT FROM manifest", "(p_params->'plan','SemanticPlan')",
        "(p_params->'candidate','TransferCandidate')", "(p_params->'protection','MaskRef')",
        'IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_document_readiness_checks_exact_published_physical_versions():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02182_v27_preapply_preview_ready_documents.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_transport_ready',
        "r.v27_preapply_preview_profile=p_node->'execution_profile'", 'artifact.verified_at IS NULL',
        "artifact.object_version IN('','null')", 'artifact.expires_at<=clock_timestamp()',
        "d.run_id=p_preview AND d.owner_id=task", 'document.object_version IS DISTINCT FROM artifact.object_version',
        'document.bucket IS DISTINCT FROM artifact.bucket', 'artifact.output_role IS DISTINCT FROM expected_role',
        "'Params','InputClosureChunk','InputClosureManifest'"):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_prelease_protection_is_canonical_and_source_bound():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02183_v27_preapply_preview_protection_provenance.sql').read_text()
    for clause in ("w.id::text=edit->>'workflow_id'", "i.id::text=edit->>'intent_id'",
        'result.id=producer.canonical_result_id', 'result.attempt=producer.attempt',
        'result.fencing_token=producer.fencing_token', "producer.operation='semantic.transfer'",
        "n->'target_protection'=jsonb_build_object", "input.contract='MaskRef'",
        'photo.semantic_protection_producer_ready', "edit->'source'->'source'->>'artifact_id'"):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body
    ready=(root/'backend/db/migrations/02182_v27_preapply_preview_ready_documents.sql').read_text()
    assert "coalesce(artifact.media_type,'') NOT IN('application/x-npy','application/octet-stream')" in ready


def test_preview_sidecars_require_exact_candidate_bytes_and_same_producer():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02184_v27_preapply_preview_sidecar_provenance.sql').read_text()
    for clause in ('photo.lock_semantic_preapply_preview_publication',
        "(p_candidate_body::jsonb)->'target' IS DISTINCT FROM e->'source'",
        'photo.semantic_approved_candidate_child', 'photo.semantic_approved_mask_binary',
        'transform.bytes<>octet_length(p_transform_body)',
        "convert_to(p_transform_body,'UTF8')", 'mask.artifact_id=mask_binary.artifact_id',
        "'MaskRef'", "'ResolvedTransform'", "'NpyArray'", "p_inventory->'entries'"):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'UPDATE photo.semantic_targets' not in body


def test_preview_images_use_authenticated_versions_and_exact_children():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02185_v27_preapply_preview_image_provenance.sql').read_text()
    for clause in ('photo.lock_semantic_preapply_preview_publication',
        "convert_to(p_intent_body,'UTF8')", "intent->>'kind' IS DISTINCT FROM 'local_exposure'",
        'FOREACH is_reference IN ARRAY ARRAY[false,true]', 'photo.semantic_approved_image_object',
        'root.bytes<>octet_length(raw)', 'photo.semantic_approved_image_binary',
        "SELECT 'pixels'::text", "SELECT 'alpha',-1", "SELECT 'inverse_field'",
        "jsonb_array_length(image->'transform_chain'->'steps')>32", "item->>'contract'='NpyArray'"):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_inventory_excludes_extra_artifacts_and_conflicting_aliases():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02186_v27_preapply_preview_exact_inventory.sql').read_text()
    for clause in ("(p_params->'plan','SemanticPlan')", "(p_candidate->'mask','MaskRef')",
        "(p_candidate->'transform','ResolvedTransform')", "(p_protection->'artifact','NpyArray')",
        "SELECT p_source->'alpha'", "SELECT p_reference->'alpha'", "step->'inverse_field'",
        'SELECT DISTINCT ref,contract', "ORDER BY (ref->>'artifact_id')::uuid",
        'count_entries<>unique_ids', "expected IS DISTINCT FROM p_inventory->'entries'", 'BETWEEN 1 AND 512'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_aggregate_admission_keeps_provenance_and_live_quota_together():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02187_v27_preapply_preview_admission_inputs.sql').read_text()
    for clause in ('photo.lock_semantic_preapply_preview_publication',
        'photo.semantic_preapply_preview_documents_ready',
        'photo.semantic_preapply_preview_sidecars_ready',
        'photo.semantic_preapply_preview_images_ready',
        'photo.semantic_preapply_preview_inventory_exact',
        'photo.semantic_preapply_preview_protection_ready',
        'photo.v27_downstream_profile_ready',
        "convert_to(p_protection_body,'UTF8')", 'bytes=octet_length(p_protection_body)',
        'quota.principal_id=p_actor', 'quota.expires_at>clock_timestamp()',
        'octet_length(raw) NOT BETWEEN 2 AND 1048576', 'FOR UPDATE'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_aggregate_gate_rejects_unknown_boolean_results():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02187_v27_preapply_preview_admission_inputs.sql').read_text()
    # SQL NOT NULL is NULL and does not enter an IF rejection branch.
    # Every subordinate admission predicate must explicitly require true.
    assert body.count('IS DISTINCT FROM true') == 6
    assert 'IF NOT photo.' not in body and 'OR NOT photo.' not in body
    assert "RETURN coalesce(quota.state='reserved'" in body
    assert 'AND quota.expires_at>clock_timestamp(),false)' in body
    for field in ('p_node', 'p_transport', 'p_inventory'):
        assert f"jsonb_typeof({field}) IS DISTINCT FROM 'object'" in body


def test_preview_draft_task_requires_full_admission_and_exact_blocked_replay():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02188_v27_preapply_preview_draft_task.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_admission_inputs_ready',
        'IS DISTINCT FROM true', "existing.state IS DISTINCT FROM 'blocked'",
        'existing.attempt IS DISTINCT FROM 0', 'existing.tenant_id IS DISTINCT FROM p_tenant',
        "existing.execution_profile IS DISTINCT FROM p_node->'execution_profile'",
        "'blocked','[]'::jsonb", 'photo.task_edges', 'id<>task_id',
        'SEMANTIC_PREVIEW_TASK_CONFLICT', 'SEMANTIC_PREVIEW_GRAPH_CONFLICT'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body
    assert 'UPDATE photo.semantic_targets' not in body
    assert "'queued'" not in body and 'graph_sealed=true' not in body


def test_preview_input_scope_preserves_existing_scopes_and_requires_live_draft():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02189_v27_preapply_preview_input_scope.sql').read_text()
    for clause in ('photo.semantic_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)',
        'photo.semantic_preapply_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)',
        'photo.semantic_preapply_preview_run_current(p_tenant,r.id) IS TRUE',
        "target.state='proposed'", 'target.child_run_id IS NULL', 'target.batch_id IS NULL',
        "r.status='draft'", "t.state='blocked'", 't.attempt=0',
        't.execution_profile=r.v27_preapply_preview_profile', 'd.object_version=params.object_version',
        'IF parent_ok IS DISTINCT FROM true THEN'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'INSERT INTO' not in body


def test_preview_registration_requires_admission_and_exact_all_or_nothing_replay():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02190_v27_preapply_preview_register_inputs.sql').read_text()
    for clause in ('photo.create_semantic_preapply_preview_draft_task',
        'photo.semantic_preapply_preview_input_scope', 'IS DISTINCT FROM true',
        "'InputClosureChunk',1000000", "'InputClosureManifest',1000001",
        'actual IS DISTINCT FROM expected', 'i.project_id IS DISTINCT FROM project',
        'SEMANTIC_PREVIEW_INPUT_REPLAY_CONFLICT', 'INSERT INTO photo.v2_task_inputs'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'DELETE FROM' not in body
    assert 'UPDATE photo.tasks' not in body and 'graph_sealed=true' not in body


def test_preview_input_seal_preserves_blocked_state_and_checks_persisted_projection():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02191_v27_preapply_preview_seal_inputs.sql').read_text()
    for clause in ('photo.register_semantic_preapply_preview_inputs',
        "jsonb_array_length(p_inventory->'entries')+2",
        "jsonb_build_array(p_transport->'manifest')||(p_transport->'direct_roots')",
        "'v2_input_manifest',p_transport->'manifest'", "'v2_entries_sha'",
        "AND state='blocked' AND attempt=0 AND NOT cancel_requested",
        'inputs=sealed_inputs AND execution_profile=sealed_profile',
        'SEMANTIC_PREVIEW_SEAL_STALE', 'SEMANTIC_PREVIEW_SEAL_INCOMPLETE'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and "state='ready'" not in body
    assert 'UPDATE photo.runs' not in body


def test_preview_persisted_closure_reconstructs_transport_and_rejects_extra_rows():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02192_v27_preapply_preview_persisted_closure.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_transport_ready',
        'IS DISTINCT FROM jsonb_array_length(entries)+2', 'direct.ordinal<>direct.expected',
        'intent.project_id=i.project_id', 'profile IS DISTINCT FROM r.v27_preapply_preview_profile',
        "a.object_version IN('','null')", 'a.expires_at<=clock_timestamp()',
        "t.execution_profile->'v2_input_manifest'=manifest", 'ordinal=1000000', 'ordinal=1000001'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'UPDATE photo.' not in body


def test_preview_activation_seals_and_queues_only_under_current_authority():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02193_v27_preapply_preview_activation.sql').read_text()
    for clause in ('photo.seal_semantic_preapply_preview_inputs',
        'photo.semantic_preapply_preview_closure_ready(owner) IS DISTINCT FROM true',
        'photo.semantic_preapply_preview_run_current(p_tenant,p_preview) IS DISTINCT FROM true',
        'expires_at>clock_timestamp()', 'share_id IS NULL', "SET state='ready'",
        "graph_sealed=true,status='queued',revision=revision+1", 'revision<9007199254740991'):
        assert clause in body
    assert 'GRANT EXECUTE' not in body and 'UPDATE photo.semantic_targets' not in body


def test_worker_preview_admission_preserves_original_gates_and_adds_persisted_closure():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02194_v27_preapply_preview_admission_closure.sql').read_text()
    assert 'RENAME TO admit_semantic_exposure_preview_before_closure' in body
    assert 'p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true' in body
    assert 'photo.semantic_preapply_preview_closure_ready(p_task) IS TRUE' in body
    assert body.count('GRANT EXECUTE') == 1


def test_preview_claim_preserves_claim_chain_and_fences_run_start():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02195_v27_preapply_preview_claim_start.sql').read_text()
    for clause in ('RENAME TO claim_task_before_preapply_preview',
        'photo.claim_task_before_preapply_preview(p_task,p_attempt,p_worker,p_command)',
        "IF claimed.outcome='claimed'", "t.operation='semantic.preview_local_exposure'",
        'photo.semantic_preapply_preview_closure_ready(t.id) IS DISTINCT FROM true',
        'photo.semantic_preapply_preview_run_current(t.tenant_id,r.id) IS DISTINCT FROM true',
        'photo.v27_downstream_profile_ready', 'q.expires_at>clock_timestamp()',
        "SET status='running',revision=revision+1", 'revision<9007199254740991',
        'SEMANTIC_PREVIEW_CLAIM_STALE'):
        assert clause in body
    assert 'UPDATE photo.semantic_targets' not in body


def test_preview_evidence_source_requires_exact_canonical_producer_and_original_session():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02198_v27_preapply_preview_evidence_source.sql').read_text()
    for clause in ('photo.inspect_semantic_preapply_preview', 'z.id=t.canonical_result_id',
        'z.attempt=t.attempt AND z.fencing_token=t.fencing_token',
        'jsonb_array_length(z.outputs)=1', 'a.created_by_task_id=t.id',
        'a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token',
        "a.output_role='preview_evidence'", 'a.bytes BETWEEN 2 AND 1048576',
        "a.object_version NOT IN('','null')", 'a.expires_at>clock_timestamp()',
        'photo.semantic_preapply_preview_closure_ready(t.id) IS TRUE'):
        assert clause in body
    assert 'UPDATE photo.' not in body and 'INSERT INTO' not in body


def test_preview_output_sidecars_require_evidence_hash_and_same_attempt_fence():
    from pathlib import Path
    root=Path(__file__).resolve().parents[2]
    body=(root/'backend/db/migrations/02199_v27_preapply_preview_output_sources.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_evidence_source',
        'octet_length(p_evidence_body) IS DISTINCT FROM evidence.bytes',
        "convert_to(p_evidence_body,'UTF8')", "e->'params' IS DISTINCT FROM p_body::jsonb",
        'a.created_by_task_id IS DISTINCT FROM evidence.task_id',
        'a.produced_attempt IS DISTINCT FROM evidence.attempt',
        'a.produced_fence IS DISTINCT FROM evidence.fence',
        'a.output_role IS DISTINCT FROM item.role', 'a.media_type IS DISTINCT FROM item.media',
        'a.bytes NOT BETWEEN 2 AND item.maximum', 'SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE'):
        assert clause in body
    assert 'UPDATE photo.' not in body and 'INSERT INTO' not in body


def test_preview_presentation_quota_preserves_authority_and_immutable_profile():
    from pathlib import Path
    root = Path(__file__).resolve().parents[2]
    body = (root/'backend/db/migrations/02200_v27_preapply_preview_presentation_quota.sql').read_text()
    for clause in ('CREATE OR REPLACE FUNCTION photo.reserve_semantic_preapply_preview_quota',
        'photo.lock_semantic_preapply_preview_publication',
        '640*pixels+134217728+1',
        '5*least(pixels,6553600)+1048576+65536',
        '20*least(pixels,6553600)+4*65536+8*1048576',
        'frozen IS NOT NULL AND frozen IS DISTINCT FROM p_profile',
        'photo.v27_downstream_profile_ready', "old.expires_at>clock_timestamp()"):
        assert clause in body
    assert 'ALTER TABLE' not in body


def test_presentation_source_requires_canonical_output_and_current_authority():
    from pathlib import Path
    root = Path(__file__).resolve().parents[2]
    body = (root/'backend/db/migrations/02201_v27_preapply_preview_presentation_source.sql').read_text()
    for clause in ('photo.inspect_semantic_preapply_preview',
        'z.id=t.canonical_result_id', 'z.attempt=t.attempt AND z.fencing_token=t.fencing_token',
        "a.output_role='preview_presentation'", 'jsonb_array_length(z.outputs)=1',
        'photo.semantic_preapply_preview_closure_ready(t.id) IS TRUE'):
        assert clause in body
    assert 'UPDATE photo.' not in body


def test_presentation_sidecars_are_hash_bound_and_same_producer():
    from pathlib import Path
    root = Path(__file__).resolve().parents[2]
    body = (root/'backend/db/migrations/02202_v27_preapply_preview_presentation_sidecars.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_presentation_source',
        "convert_to(p_presentation_body,'UTF8')", "e->'params' IS DISTINCT FROM p_body::jsonb",
        "count(DISTINCT value->>'artifact_id')", 'a.created_by_task_id IS DISTINCT FROM evidence.task_id',
        'a.produced_attempt IS DISTINCT FROM evidence.attempt',
        'a.produced_fence IS DISTINCT FROM evidence.fence',
        "'preview_evidence',e->'numerical_evidence'", "'before',e->'before'",
        "'after',e->'after'", "'affected',e->'affected'", "'protected',e->'protected'",
        'SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE'):
        assert clause in body
    assert 'UPDATE photo.' not in body and 'INSERT INTO' not in body


def test_current_preview_lookup_rejects_ambiguity_and_foreign_session():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02204_v27_preapply_preview_lookup.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_current', 'IS DISTINCT FROM true',
        "->>'session_id'=p_session::text", "candidate.status IN('running','completed')",
        'candidate.graph_sealed', 'LIMIT 2', 'SEMANTIC_PREVIEW_AMBIGUOUS',
        "object.object_version NOT IN('','null')"):
        assert clause in body
    assert 'INSERT INTO' not in body and 'UPDATE photo.' not in body


def test_semantic_preview_quota_release_uses_its_own_run_marker():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02205_v27_preapply_preview_quota_owner.sql').read_text()
    for clause in ("r.config_snapshot->'semantic_preapply_preview' IS NULL",
        "r.config_snapshot->'semantic_preapply_preview' IS NOT NULL",
        'r.v27_preapply_preview_profile IS NOT NULL',
        "r.status NOT IN('completed','completed_with_errors','failed','cancelled')",
        "u.state NOT IN('ready','cleaned')", "d.state NOT IN('ready','cleaned')",
        'reserved_bytes>=q.bytes'):
        assert clause in body
    assert 'r.v26_derivative_task_id IS' not in body


def test_preview_finalization_requires_canonical_presentation_and_preserves_quota():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02206_v27_preapply_preview_finalize.sql').read_text()
    for clause in ('photo.semantic_preapply_preview_run_current',
        'photo.semantic_preapply_preview_closure_ready(t.id) IS DISTINCT FROM true',
        'z.id=t.canonical_result_id', 'z.attempt=t.attempt AND z.fencing_token=t.fencing_token',
        "a.output_role='preview_presentation'", 'extra.id<>t.id',
        "done:='completed'", "done:='failed'", 'r.revision>=9007199254740991'):
        assert clause in body
    assert 'UPDATE photo.quota_reservations' not in body
    assert 'UPDATE photo.semantic_targets' not in body


def test_preview_finalization_filters_ineligible_success_before_limit():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02206_v27_preapply_preview_finalize.sql').read_text()
    selection = body.split('ORDER BY x.created_at,x.id LIMIT 1;')[0]
    for clause in ("tenant.status='active'", 'extra.id<>task.id',
        'photo.semantic_preapply_preview_closure_ready(task.id) IS TRUE',
        "a.output_role='preview_presentation'", 'z.id=task.canonical_result_id',
        "a.object_version NOT IN('','null')"):
        assert clause in selection


def test_preview_receipt_is_immutable_canonical_and_does_not_apply():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02207_v27_preapply_preview_receipt.sql').read_text()
    for clause in ('photo.lock_semantic_decision_publication',
        'photo.semantic_preapply_preview_presentation_source',
        'photo.semantic_preapply_preview_presentation_sidecars', 'counted<>7',
        "n->'protected_pixels_unchanged' IS DISTINCT FROM 'true'::jsonb",
        'r.v27_preapply_preview_receipt IS DISTINCT FROM receipt',
        'SEMANTIC_PREVIEW_RECEIPT_CONFLICT'):
        assert clause in body
    assert 'UPDATE photo.semantic_targets' not in body
    assert 'UPDATE photo.quota_reservations' not in body


def test_preview_selection_lock_requires_exact_shown_render_and_attempt():
    from pathlib import Path
    body = (Path(__file__).resolve().parents[2]/'backend/db/migrations/02208_v27_preapply_preview_selection_lock.sql').read_text()
    for clause in ('photo.lock_semantic_decision_publication', 'matches<>1',
        'photo.semantic_preapply_preview_current',
        "t.canonical_result_id::text IS DISTINCT FROM sealed->>'result_id'",
        "t.fencing_token::text IS DISTINCT FROM sealed->>'fence'",
        "z.outputs=jsonb_build_array(sealed->'presentation')",
        "('working_image',sealed->'working_image')", 'TO photo_semantic_admission'):
        assert clause in body
    assert 'UPDATE photo.semantic_targets' not in body
    assert 'INSERT INTO photo.tasks' not in body
