from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.retouch_preview_admission import sql_retouch_preview_adoption_admission


@pytest.mark.parametrize('failure',[None,'changed','missing','wrong_bytes','cancel'])
def test_retouch_adoption_sql_admission_re_resolves_exact_generation(failure):
    tenant,task,result,params=uuid4(),uuid4(),uuid4(),uuid4()
    raw=b'{}';digest=sha256(raw).hexdigest();calls=[];lost=False
    context=SimpleNamespace(attempt=1,fence=7,result_id=result,operation='semantic.preview_retouch_region',version='1')
    class Connection:
        def execute(self,statement,scope):
            nonlocal lost
            assert 'v27_retouch_preview_adoption_params' in statement
            assert scope==(tenant,task,1,7,result)
            calls.append(scope)
            if len(calls)>1 and failure=='cancel':lost=True
            row=None if len(calls)>1 and failure=='missing' else (uuid4() if len(calls)>1 and failure=='changed' else params,digest)
            return SimpleNamespace(fetchone=lambda:row)
    def active():
        if lost:raise InterruptedError('cancelled')
    ref,admit=sql_retouch_preview_adoption_admission(Connection(),tenant,task,context,check_active=active)
    assert ref.artifact_id==params
    # Context mutation cannot replace the frozen result-generation query scope.
    context.fence=99
    if failure in ('cancel','missing'):
        with pytest.raises(InterruptedError if failure=='cancel' else AdoptionInvalid):admit(raw)
    else:
        assert admit(b'{ }' if failure=='wrong_bytes' else raw) is (failure is None)
    assert len(calls)==(1 if failure=='wrong_bytes' else 2)


def test_retouch_params_resolver_uses_preview_authority_without_v2_workflow_gate():
    from pathlib import Path
    migration=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02366_v27_retouch_preview_adoption_scope.sql').read_text()
    body=migration.split('AS $$',1)[1].split('$$;',1)[0]
    assert "r.kind='preview'" in body
    assert "tenant.status='active'" in body
    assert 'photo.v2_adoption_context' not in body
    assert 'photo.v2_workflows' not in body
    for guard in ('semantic_retouch_preview_bundle_ready','semantic_retouch_preview_run_current','v27_downstream_profile_ready'):
        assert guard in body
    assert 'z.id=p_result' in body
    assert 't.attempt=p_attempt' in body
    assert 't.fencing_token=p_fence' in body


def test_retouch_presentation_sources_bind_adapter_and_canonical_cpu_generation():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02367_v27_retouch_preview_presentation_sources.sql').read_text()
    assert 'inspect_semantic_retouch_preview(p_tenant,p_preview,p_actor,p_session,p_body,p_sha,p_context,p_context_sha)' in sql
    assert "t.resource_class='cpu'" in sql
    assert "t.operation='semantic.preview_retouch_region'" in sql
    assert 'semantic_retouch_preview_bundle_ready(t.id,z.id,z.attempt,z.fencing_token)' in sql
    assert 'a.produced_attempt IS DISTINCT FROM evidence.attempt' in sql
    assert 'a.produced_fence IS DISTINCT FROM evidence.fence' in sql
    assert 'TO photo_api' in sql
    assert 'TO photo_worker' not in sql


def test_retouch_receipt_checks_physically_verified_generation_before_write():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02368_v27_retouch_preview_receipt.sql').read_text()
    write=sql.index('UPDATE photo.runs SET v27_retouch_preview_receipt=receipt')
    for check in ('root.task_id IS DISTINCT FROM p_task','root.result_id IS DISTINCT FROM p_result','root.attempt IS DISTINCT FROM p_attempt','root.fence IS DISTINCT FROM p_fence'):
        assert sql.index(check)<write
    assert "n->'alpha_unchanged' IS DISTINCT FROM 'true'::jsonb" in sql
    assert "n->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'" in sql
    assert 'lock_semantic_decision_publication' in sql
    assert 'SEMANTIC_PREVIEW_RECEIPT_CONFLICT' in sql


def test_retouch_lookup_and_startup_are_bound_to_own_preview():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02369_v27_retouch_preview_lookup.sql').read_text()
    assert 'SEMANTIC_PREVIEW_AMBIGUOUS' in sql
    assert 'adapter_context_id uuid,adapter_context_sha text' in sql
    assert 'semantic_retouch_preview_run_current(p_tenant,r.id)' in sql
    resolver=(root/'backend/internal/semanticreview/retouch_preview_resolver.go').read_text()
    assert 'InspectRetouchPreviewRun(ctx, db, who, run, params, adapter)' in resolver
    main=(root/'backend/cmd/photo/main.go').read_text()
    assert 'Retouch: semanticRetouchPreview' in main
    assert 'buildSemanticRetouchPreview(' in main


def test_retouch_landmark_discovery_keeps_review_and_source_origin_authority():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02370_v27_retouch_preview_landmark_catalog.sql').read_text()
    assert "current_setting('app.tenant_id',true)" in sql
    assert 'semantic_preapply_preview_current' in sql
    assert "target.asset_version_id::text=p_source->>'asset_version_id'" in sql
    assert 'semantic_retouch_preview_landmark_origin(p_tenant,project,p_actor,p_session,p_source,a.id,a.sha256) IS TRUE' in sql
    assert 'ORDER BY a.id LIMIT 65' in sql


def test_retouch_face_resolver_repeats_scoped_catalog_after_object_io():
    from pathlib import Path
    source=(Path(__file__).resolve().parents[1]/'backend/internal/semanticreview/retouch_preview_face_resolver.go').read_text()
    assert "set_config('app.tenant_id',$1,true)" in source
    assert 'AccessMode: pgx.ReadOnly' not in source
    assert 'DecodeRetouchPreviewFaces(p, source, original.Landmarks.Ref, landmarks)' in source
    assert 'latest != original' in source
    assert 'if count > 1' in source
    assert source.index('latest, err := load()')>source.index('landmarks, err := readExact(original.Landmarks)')


def test_retouch_dispatch_checks_actual_selected_renderer_and_capability_mask():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02371_v27_retouch_preview_dispatch_renderer.sql').read_text()
    assert 'w.worker_id=t.dispatch_worker_id' in sql
    assert "w.v2_renderer_versions ? 'v2.2'" in sql
    assert 'SEMANTIC_RETOUCH_PREVIEW_WORKER_STALE' in sql
    assert "USING ERRCODE='40001'" in sql
    for file in ('backend/internal/httpapi/semantic_review.go','backend/internal/capability/service.go'):
        assert '"semantic.preview_retouch_region"' in (root/file).read_text()


def test_retouch_dispatch_filters_renderer_before_worker_selection():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02372_v27_retouch_preview_worker_selection.sql').read_text()
    assert sql.index("w.v2_renderer_versions ? 'v2.2'")<sql.index('ORDER BY w.worker_id LIMIT 1 FOR SHARE')
    assert "t.operation<>'semantic.preview_retouch_region' OR" in sql
    assert 'photo.reserve_dispatch_resources(' in sql
    assert "routing_key='worker.'||target||'.'||routing_key" in sql
    assert 'TO photo_scheduler' not in sql


def test_shown_retouch_admission_uses_own_evidence_and_exact_inventory():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02376_v27_shown_retouch_admission_documents.sql').read_text()
    assert "numerical->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'" in sql
    assert "numerical->'alpha_unchanged' IS DISTINCT FROM 'true'::jsonb" in sql
    assert 'big_lama' not in sql
    assert 'global_pass' not in sql
    assert 'semantic_shown_retouch_inventory_exact' in sql
    assert 'lock_semantic_shown_retouch_params' in sql
    assert "'semantic.adopt_shown_retouch_region','1','cpu'" in sql


def test_shown_retouch_batch_locks_exact_selection_before_any_activation():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02377_v27_shown_retouch_batch_locks.sql').read_text()
    assert "ORDER BY value->>'target_id'" in sql
    assert 'photo.lock_semantic_retouch_preview_selection(' in sql
    assert 'photo.lock_semantic_decision_targets(' in sql
    assert "'semantic.adopt_shown_retouch_region:'" in sql
    assert 'photo.semantic_shown_retouch_transport_ready(' in sql
    assert "e->'working_image' IS DISTINCT FROM decision->'preview'->'render'" in sql
    assert "e->'presentation' IS DISTINCT FROM decision->'preview'->'evidence'" in sql
    assert 'background_cleanup' not in sql
    assert 'INSERT INTO' not in sql
    assert 'UPDATE photo.' not in sql


def test_shown_retouch_reserved_draft_validates_before_mutation_and_never_dispatches():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02378_v27_shown_retouch_reserved_draft.sql').read_text()
    assert sql.index('photo.lock_semantic_shown_retouch_decision_batch(')<sql.index('INSERT INTO photo.runs(')
    assert sql.index('photo.semantic_shown_retouch_admission_documents_ready(')<sql.index('INSERT INTO photo.runs(')
    assert "'blocked','[]'" in sql
    assert "'apply','draft'" in sql
    assert "t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region'" in sql
    assert 'photo.semantic_shown_retouch_profile_bound(' in sql
    assert "'semantic.adopt_shown_retouch_region','1','cpu'" in sql
    assert 'usage.storage_bytes::numeric+usage.reserved_bytes+amount' in sql
    assert 'q.principal_id IS DISTINCT FROM p_actor' in sql
    assert 'q.expires_at<=clock_timestamp()' in sql
    assert 'SEMANTIC_SHOWN_DRAFT_PARTIAL' in sql
    assert 'photo.reserve_semantic_shown_retouch_apply_quota(' in sql
    assert 'UPDATE photo.quota_reservations' not in sql
    assert 'dispatch_outbox' not in sql
    assert 'TO photo_semantic_admission' not in sql
    assert 'background_cleanup' not in sql


def test_shown_retouch_persisted_closure_reconstructs_complete_versioned_transport():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02379_v27_shown_retouch_persisted_closure.sql').read_text()
    assert "'886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90'" in sql
    assert "t.operation<>'semantic.adopt_shown_retouch_region'" in sql
    assert 'NOT BETWEEN 3 AND 514' in sql
    assert 'direct.ordinal<>direct.expected' in sql
    assert 'IS DISTINCT FROM jsonb_array_length(entries)+2' in sql
    assert "contract='InputClosureChunk' AND ordinal=1000000" in sql
    assert "contract='InputClosureManifest' AND ordinal=1000001" in sql
    assert "a.object_version IN('','null')" in sql
    assert "i.project_id::text IS DISTINCT FROM r.config_snapshot->>'project_id'" in sql
    assert 'photo.semantic_shown_retouch_transport_ready(' in sql
    assert 'INSERT INTO' not in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_retouch_input_seal_retains_current_guard_and_binds_registered_closure():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02380_v27_shown_retouch_input_sealing.sql').read_text()
    latest=(root/'backend/db/migrations/02354_v27_retouch_preview_task_registration.sql').read_text()
    start=latest.index('CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link()')
    old=latest[start:latest.index('-- +goose StatementEnd',start)]
    start=sql.index('CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link()')
    new=sql[start:sql.index('-- +goose StatementEnd',start)]
    extra='    OR photo.semantic_shown_retouch_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)\n'
    assert extra in new
    assert new.replace(extra,'')==old
    assert 'SEMANTIC_SHOWN_INPUT_REPLAY_CONFLICT' in sql
    assert 'SEMANTIC_SHOWN_SEAL_STALE' in sql
    assert 'SEMANTIC_SHOWN_SEAL_INCOMPLETE' in sql
    assert "'InputClosureChunk',1000000" in sql
    assert "'InputClosureManifest',1000001" in sql
    assert 'photo.semantic_shown_retouch_closure_ready(t.id,' in sql
    assert "'contracts_sha','886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90'" in sql
    assert "AND state='blocked' AND attempt=0" in sql
    assert 'dispatch_outbox' not in sql


def test_shown_retouch_atomic_commit_orders_all_mutations_and_propagates_refusal():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02381_v27_shown_retouch_atomic_commit.sql').read_text()
    commit=sql[sql.index('CREATE FUNCTION photo.commit_semantic_shown_retouch_apply_review'):]
    steps=['FROM photo.idempotency_records','photo.create_semantic_shown_retouch_apply_reserved_draft(',
           'photo.seal_semantic_shown_retouch_apply_inputs(', 'photo.finish_semantic_shown_retouch_apply_review(']
    offsets=[commit.index(step) for step in steps]
    assert offsets==sorted(offsets)
    assert 'EXCEPTION WHEN' not in sql
    assert 'photo.semantic_shown_retouch_apply_sealed_inputs_ready(' in sql
    assert 'q.bytes IS DISTINCT FROM expected::bigint' in sql
    assert 'SEMANTIC_APPLY_ACTIVATION_PARTIAL' in sql
    assert 'SEMANTIC_APPLY_ACTIVATION_STALE' in sql
    assert "'semantic.adopt_shown_retouch_region','1','cpu'" in sql
    assert "UPDATE photo.runs SET revision=revision+1" in sql
    assert 'INSERT INTO photo.audit_log' in sql
    assert 'INSERT INTO photo.run_events' in sql
    assert 'INSERT INTO photo.outbox' in sql
    assert "idem.response_status IS NOT NULL" in commit
    assert 'TO photo_semantic_admission' in commit
    assert 'background_cleanup' not in sql


def test_shown_retouch_tenant_startup_connects_copy_budget_provider_and_resolver():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    installation=(root/'backend/internal/semanticreview/service_installation.go').read_text()
    assert 'if config.ShownRetouchPreview == nil' in installation
    assert 'NewShownBudgetSelection(*config.ShownRetouchExecution)' in installation
    assert 'NewSQLShownRetouchApplyResolver(' in installation
    assert 'config.ShownResolver, removal, cleanup, retouch)' in installation
    catalog=(root/'backend/internal/semanticreview/tenant_installation.go').read_text()
    assert 'json:"shown_retouch_execution,omitempty"' in catalog
    assert 'entry.ShownBackgroundCleanupExecution, entry.ShownRetouchExecution}' in catalog
    assert 'config.ShownRetouchExecution = entry.ShownRetouchExecution' in catalog
    assert '"semantic.adopt_shown_retouch_region":' in catalog
    main=(root/'backend/cmd/photo/main.go').read_text()
    assert 'semanticPreview, semanticRemovalPreview, semanticBackgroundCleanupPreview, semanticRetouchPreview)' in main


def test_shown_retouch_publication_replay_rechecks_receipt_object_key_and_quota():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02383_v27_shown_retouch_publication_replay.sql').read_text()
    assert sql.count('p_run:=photo.lock_semantic_shown_retouch_publication(')==2
    assert sql.count("a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id")==2
    assert 'capacity<>trunc(capacity)' in sql
    assert 'used<0 OR used<>trunc(used)' in sql
    assert 'd.token IS DISTINCT FROM p_token' in sql
    assert 'DOCUMENT_QUOTA_ACCOUNTING' in sql
    assert 'photo_semantic_admission;' in sql
    assert 'TO photo_api;' in sql


def test_shown_retouch_worker_admission_requires_live_cpu_lease_and_full_producer_bundle():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02384_v27_shown_retouch_worker_admission.sql').read_text()
    for gate in ('photo.worker_authorized(', 'db_principal=session_user','photo.semantic_shown_retouch_closure_ready(',
                 'photo.semantic_retouch_preview_bundle_ready(', 'photo.v2_editor_authorized(', 'photo.v27_downstream_profile_ready('):
        assert gate in sql
    assert "producer.operation IS DISTINCT FROM 'semantic.preview_retouch_region'" in sql
    assert 't.fencing_token IS DISTINCT FROM p_fence' in sql
    assert 't.worker_id IS DISTINCT FROM p_worker' in sql
    assert 't.lease_until<=clock_timestamp()' in sql
    assert 'old_preview.v27_retouch_preview_receipt' in sql
    assert 'TO photo_worker;' in sql
    assert 'background_cleanup' not in sql


def test_shown_retouch_completed_adoption_has_own_canonical_bundle_and_scheduler_acl():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02385_v27_shown_retouch_adoption_discovery.sql').read_text()
    assert sql.count('TO photo_scheduler;')==5
    assert sql.count('FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;')==5
    for gate in ('photo.semantic_shown_retouch_params_bound(', 'photo.semantic_shown_retouch_closure_ready(',
                 'photo.semantic_retouch_preview_bundle_ready(', 'photo.v2_editor_authorized(',
                 'photo.v27_downstream_profile_ready(', 'old_preview.v27_retouch_preview_receipt'):
        assert gate in sql
    assert "producer.operation IS DISTINCT FROM 'semantic.preview_retouch_region'" in sql
    assert "t.state='succeeded'" in sql
    assert 'result.id=t.canonical_result_id' in sql
    assert 'result.attempt=t.attempt AND result.fencing_token=t.fencing_token' in sql
    assert "i.consumer='v2-adopt'" in sql
    assert 'background_cleanup' not in sql
    assert 'Forward-only migration' in sql


def test_shown_retouch_locked_adoption_is_dedicated_and_repeats_proof_after_retention():
    from pathlib import Path
    sql=(Path(__file__).resolve().parents[1]/'backend/db/migrations/02386_v27_shown_retouch_adoption_commit.sql').read_text()
    assert 'RENAME TO adopt_v2_plan_before_shown_retouch' in sql
    assert "AND operation='semantic.adopt_shown_retouch_region') THEN RETURN false" in sql
    assert 'RETURN photo.adopt_v2_plan_before_shown_retouch(' in sql
    final=sql[sql.index('CREATE FUNCTION photo.adopt_shown_retouch(p_tenant'):]
    assert final.count('photo.shown_retouch_output_bound(')>=2
    assert final.index('photo.shown_retouch_output_bound(')<final.index('accepted:=photo.adopt_shown_retouch_inventory(')
    assert 'SEMANTIC_SHOWN_ADOPTION_STALE' in final
    assert 'ORDER BY r.id FOR UPDATE' in final and 'ORDER BY id FOR UPDATE' in final
    assert 'TO photo_scheduler;' in final
    assert "copied.sha256 IS DISTINCT FROM original.sha256" in sql
    assert "(shown-'pixels') IS DISTINCT FROM (output-'pixels')" in sql
    assert "INSERT INTO photo.v2_artifact_links" in sql
    assert "INSERT INTO photo.inbox" in sql
    assert "INSERT INTO photo.outbox" in sql
    assert 'background_cleanup' not in sql


def test_shown_retouch_worker_registration_requires_explicit_cpu_installation():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    service=(root/'workers/photo_worker/runtime/service.py').read_text()
    assert 'if shown_retouch_installation_factory is not None and (not callable(shown_retouch_installation_factory)' in service
    assert "raise WorkerConfigurationError('Shown retouch installation requires CPU worker')" in service
    assert 'operations+=shown_retouch_operations(artifacts,installation_factory=shown_retouch_installation_factory)' in service
    assert "installed_renderers['shown_retouch_installation_factory']=shown_retouch_installation_factory" in service
    binding=(root/'workers/photo_worker/operations/semantic/preview_retouch_adopt_binding.py').read_text()
    assert 'bind_retouch_preview_result(' in binding
    assert "edit.kind!='retouch_region'" in binding
    assert 'evidence.roi != [0,0,image.canvas.width,image.canvas.height]' in binding


def test_shown_retouch_claim_preserves_predecessor_and_binds_current_contract():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02389_v27_shown_retouch_claim_start.sql').read_text()
    assert 'RENAME TO claim_task_before_shown_retouch' in sql
    assert 'FROM photo.claim_task_before_shown_retouch(' in sql
    assert "t.operation='semantic.adopt_shown_retouch_region'" in sql
    assert '886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90' in sql
    assert 'photo.v27_downstream_profile_ready(' in sql
    assert "q.bytes=expected::bigint" in sql
    assert "UPDATE photo.runs SET status='running'" in sql
    assert "SEMANTIC_SHOWN_RETOUCH_CLAIM_INPUTS_STALE" in sql
    assert "USING ERRCODE='40001'" in sql
    assert 'TO photo_worker;' in sql
    assert 'background_cleanup' not in sql


def test_shown_retouch_claim_namespace_fix_preserves_every_other_gate():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    old=(root/'backend/db/migrations/02389_v27_shown_retouch_claim_start.sql').read_text()
    fixed=(root/'backend/db/migrations/02394_v27_shown_retouch_claim_namespace.sql').read_text()
    def definition(raw,marker):
        start=raw.index(marker)
        return raw[start:raw.index('-- +goose StatementEnd',start)]
    expected=definition(old,'CREATE FUNCTION photo.claim_task(').replace(
        'CREATE FUNCTION photo.claim_task(','CREATE OR REPLACE FUNCTION photo.claim_task(',1).replace(
        "'semantic.adopt_shown_retouch:'","'semantic.adopt_shown_retouch_region:'")
    assert definition(fixed,'CREATE OR REPLACE FUNCTION photo.claim_task(')==expected
    assert 'RENAME TO' not in fixed
    assert "'semantic.adopt_shown_retouch:'" not in fixed
    # The SQL namespace must match the domain compiler's deterministic task ID.
    matches=list((root/'backend/internal/semanticreview').glob('retouch_preview_adoption*.go'))
    assert any('"semantic.adopt_shown_retouch_region:"' in path.read_text() for path in matches if not path.name.endswith('_test.go'))


def test_shown_retouch_startup_passes_distinct_installation_and_own_discovery():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    cli=(root/'workers/photo_worker/cli.py').read_text()
    assert 'shown_retouch=shown_retouch_from_environment(config)' in cli
    assert "installations['shown_retouch_installation_factory']=shown_retouch" in cli
    service=(root/'workers/photo_worker/v2/service.py').read_text()
    assert "PHOTO_ADOPTER_SHOWN_RETOUCH_ENABLED" in service
    assert 'shown_retouch_installation=shown_retouch_installation' in service
    assert 'versioned_shown_retouch_adoption_installation(' in service
    assert "if getattr(self,'shown_retouch_enabled',False):" in service
    assert 'SELECT * FROM photo.v27_pending_shown_retouch_adoptions()' in service
    assert "exposure_options['shown_retouch_enabled']=True" in service
    assert '_validate_retouch_pixel_scratch(config.shown_retouch_enabled,' in service


def test_shown_retouch_live_io_preserves_previous_entrypoints_and_current_receipt():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02390_v27_shown_retouch_live_io.sql').read_text()
    for gate in ('photo.semantic_retouch_preview_bundle_ready(', 'photo.v2_editor_authorized(',
                 'photo.v27_downstream_profile_ready(', 'preview.v27_retouch_preview_receipt',
                 'gateway_input_before_shown_retouch(', 'heartbeat_task_before_shown_retouch('):
        assert gate in sql
    assert "t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region' THEN RETURN true" in sql
    assert '886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90' in sql
    assert "RETURN QUERY SELECT false,NULL::timestamptz,true" in sql
    assert "q.bytes=expected::bigint" in sql
    assert 'background_cleanup' not in sql
    selection=(root/'backend/db/migrations/02392_v27_shown_retouch_worker_selection.sql').read_text()
    assert "t.operation NOT IN('semantic.preview_retouch_region','semantic.adopt_shown_retouch_region')" in selection
    assert "w.v2_renderer_versions ? 'v2.2'" in selection
    assert selection.index("w.v2_renderer_versions ? 'v2.2'")<selection.index('ORDER BY w.worker_id LIMIT 1')


def test_shown_retouch_catalog_is_cpu_copy_without_fabricated_qualification():
    from pathlib import Path
    import json
    root=Path(__file__).resolve().parents[1]
    sql=(root/'backend/db/migrations/02391_v27_shown_retouch_adoption_catalog.sql').read_text()
    assert "'[\"cpu\"]',false" in sql
    assert 'qualification_policies' not in sql
    operations=json.loads((root/'backend/internal/workflow/operations.json').read_text())['operations']
    own=[op for op in operations if op['name']=='semantic.adopt_shown_retouch_region']
    assert len(own)==1 and own[0]['resource_class']=='cpu' and own[0]['backends']==['cpu']
    assert own[0]['params_type']=='ShownRetouchAdoptionInput'
    assert own[0]['methods']==['execute_shown_retouch_task']


def test_shown_retouch_claim_rechecks_and_locks_current_worker_renderer():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    previous=(root/'backend/db/migrations/02394_v27_shown_retouch_claim_namespace.sql').read_text()
    current=(root/'backend/db/migrations/02395_v27_shown_retouch_claim_worker.sql').read_text()
    start=current.index(' -- Apply runs do not enter')
    end=current.index(' SELECT * INTO r FROM photo.runs',start)
    added=current[start:end]
    assert "w.v2_renderer_versions ? 'v2.2'" in added
    assert "w.db_principal=session_user" in added
    assert "w.tenant_id=t.tenant_id AND w.worker_id=p_worker AND w.enabled" in added
    assert "(w.qualified_operations->'semantic.adopt_shown_retouch_region') ? 'cpu'" in added
    assert "photo.capacity_policy_worker_fresh(w) FOR SHARE" in added
    assert "SEMANTIC_SHOWN_RETOUCH_CLAIM_WORKER_STALE" in added
    # Retain every namespace, input, quota, profile and predecessor check.
    def definition(raw):
        first=raw.index('CREATE OR REPLACE FUNCTION photo.claim_task(')
        return raw[first:raw.index('-- +goose StatementEnd',first)]
    assert definition(current).replace(added,'',1)==definition(previous)
    assert current.count('CREATE OR REPLACE FUNCTION photo.claim_task(')==1


def test_shown_retouch_live_io_worker_gate_preserves_all_scope_checks():
    from pathlib import Path
    root=Path(__file__).resolve().parents[1]
    old=(root/'backend/db/migrations/02390_v27_shown_retouch_live_io.sql').read_text()
    new=(root/'backend/db/migrations/02396_v27_shown_retouch_live_worker.sql').read_text()
    def body(raw):
        start=raw.index('FUNCTION photo.semantic_shown_retouch_task_live(')
        return raw[start:raw.index('END $$;',start)+len('END $$;')]
    start=new.index(' -- The gateway session')
    end=new.index(' SELECT * INTO r FROM photo.runs',start)
    added=new[start:end]
    assert 'w.worker_id=t.worker_id' in added
    assert "w.v2_renderer_versions ? 'v2.2'" in added
    assert 'photo.capacity_policy_worker_fresh(w) FOR SHARE' in added
    assert 'w.db_principal=session_user' not in added
    assert body(new).replace(added,'',1)==body(old)
