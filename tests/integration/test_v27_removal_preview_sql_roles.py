"""Complete removal SQL installation in an isolated per-case integration database."""
import pytest
import psycopg
from conftest import database as isolated_database_config


@pytest.fixture
def database(isolated_database_config):
    """Own a rollback connection inside the per-case disposable database."""
    with psycopg.connect(**isolated_database_config) as connection:
        try:
            yield connection
        finally:
            connection.rollback()


def test_removal_protection_is_worker_only_definer(database):
    signature='photo.semantic_removal_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)'
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_scheduler','photo_portal','photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is False
    assert database.execute('SELECT has_function_privilege(\'photo_worker\',%s,\'EXECUTE\')',(signature,)).fetchone()[0] is True


def test_removal_inventory_digest_is_operation_specific(database):
    from hashlib import sha256
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    from photo_worker.contracts.base import canonical_bytes
    task,params=uuid4(),uuid4()
    entries=[dict(contract='Params',ref=dict(artifact_id=str(params),sha256='a'*64))]
    entries_sha=sha256(canonical_bytes(entries)).hexdigest()
    def digest(operation):
        return sha256(canonical_bytes(dict(entries=entries,operation=operation,task=str(task),version='1'))).hexdigest()
    # Pure predicates are private: exercise as their owner only on an explicitly
    # provisioned integration database with a login allowed to SET ROLE.
    database.execute('SET LOCAL ROLE photo_fn_owner')
    query='SELECT photo.semantic_removal_preview_inventory_digest_ready(%s,%s,%s,%s,%s,%s)'
    assert database.execute(query,(task,params,'a'*64,digest('semantic.preview_remove_object'),entries_sha,Jsonb(entries))).fetchone()[0] is True
    assert database.execute(query,(task,params,'a'*64,digest('semantic.preview_local_exposure'),entries_sha,Jsonb(entries))).fetchone()[0] is False


@pytest.mark.parametrize('body,attempt,fence', [('{}',0,1),('{}',1,0),('not-json',1,1)])
def test_removal_admission_denies_invalid_or_unscoped_task(database,body,attempt,fence):
    from hashlib import sha256
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_worker')
    args=(uuid4(),uuid4(),uuid4(),attempt,fence,uuid4(),uuid4(),
        sha256(body.encode()).hexdigest(),body,'a'*64)
    assert database.execute('SELECT photo.admit_semantic_removal_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is False
    assert database.execute('SELECT photo.v27_removal_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is None


def test_removal_document_tickets_are_api_only(database):
    signatures=(
        'photo.reserve_semantic_removal_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text)',
        'photo.complete_semantic_removal_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text)',
    )
    for signature in signatures:
        assert database.execute('SELECT has_function_privilege(\'photo_api\',%s,\'EXECUTE\')',(signature,)).fetchone()[0] is True
        for role in ('photo_worker','photo_scheduler','photo_portal','photo_gateway'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is False


def test_removal_document_reservation_rejects_exposure_task_namespace(database):
    from uuid import uuid4,uuid5
    from hashlib import sha256
    tenant,intent,actor,session,target,run=(uuid4() for _ in range(6))
    task=uuid5(run,'semantic.preview_local_exposure:'+str(target))
    params=uuid5(task,'Params');raw='{}';digest=sha256(raw.encode()).hexdigest()
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT * FROM photo.reserve_semantic_removal_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (tenant,intent,actor,session,target,run,raw,digest,'task_params',task,params,digest,2,'photos'))


def test_removal_saved_run_rejects_exposure_parameters(database):
    from uuid import uuid4
    from hashlib import sha256
    raw='{"schema_version":1,"kind":"local_exposure"}'
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT photo.save_semantic_removal_preview(%s,%s,%s,%s,%s,%s,%s)',
                (*[uuid4() for _ in range(5)],raw,sha256(raw.encode()).hexdigest()))


def test_removal_input_gate_rejects_incomplete_graph_without_writes(database):
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    database.execute('SET LOCAL ROLE photo_fn_owner')
    args=(*[uuid4() for _ in range(6)],*['{}']*9,Jsonb(None),Jsonb({}),Jsonb({}))
    assert database.execute('SELECT photo.semantic_removal_preview_admission_inputs_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is False


def test_removal_materialization_helpers_remain_private(database):
    signature='photo.create_semantic_removal_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is False


def test_removal_commit_is_narrow_api_entrypoint(database):
    suffix='(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)'
    for name in ('seal_semantic_removal_preview_inputs','activate_semantic_removal_preview','commit_semantic_removal_preview'):
        signature='photo.'+name+suffix
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
            allowed=database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
            assert allowed is (role=='photo_api' and name=='commit_semantic_removal_preview')


def test_removal_registry_stays_disabled_before_qualification_and_wiring(database):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    row=database.execute("SELECT resource_class,model_id,backends,v2_enabled FROM photo.operation_registry WHERE operation='semantic.preview_remove_object' AND version='1'").fetchone()
    assert row is not None and row[0]=='gpu' and row[1]=='big_lama'
    assert set(row[2])=={'cpu','cuda','mps'} and row[3] is False


def test_removal_claim_preserves_private_previous_chain(database):
    signature='photo.claim_task_before_removal_preview(uuid,integer,uuid,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is False


def test_removal_live_io_delegates_are_private(database):
    signatures = (
        'photo.semantic_removal_preview_task_live(uuid)',
        'photo.gateway_input_before_removal_preview(uuid,integer,bigint,uuid,text,uuid)',
        'photo.heartbeat_task_before_removal_preview(uuid,integer,bigint,uuid,jsonb)',
    )
    for signature in signatures:
        row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
        assert row[0]=='photo_fn_owner' and row[1] is True
        assert 'search_path=pg_catalog, photo' in row[2]
        assert database.execute("SELECT NOT EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=to_regprocedure(%s) AND a.grantee=0 AND a.privilege_type='EXECUTE')",(signature,)).fetchone()[0] is True
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway','photo_semantic_admission'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False


def test_removal_live_io_rejects_missing_task(database):
    from uuid import uuid4
    task, worker, artifact = uuid4(), uuid4(), uuid4()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_gateway')
        assert database.execute('SELECT * FROM photo.gateway_input(%s,1,1,%s,%s,%s)', (task, worker, 'missing', artifact)).fetchall() == []
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        row = database.execute("SELECT * FROM photo.heartbeat_task(%s,1,1,%s,'{}'::jsonb)", (task, worker)).fetchone()
        assert tuple(row) == (False, None, True)


def test_removal_presentation_sources_are_api_only(database):
    signatures = (
        'photo.semantic_removal_preview_presentation_source(uuid,uuid,uuid,uuid,text,text)',
        'photo.semantic_removal_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_removal_result_delegate_is_private(database):
    signature = 'photo.commit_task_success_before_removal_preview(uuid,integer,bigint,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False


def test_removal_receipt_sealer_is_api_only(database):
    signature = 'photo.seal_semantic_removal_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_removal_selection_lock_is_admission_only(database):
    signature = 'photo.lock_semantic_removal_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_semantic_admission')


def test_shown_removal_worker_entrypoints_are_narrow(database):
    suffix = '(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for name in ('admit_semantic_shown_removal', 'v27_shown_removal_report_evidence'):
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, 'photo.'+name+suffix)).fetchone()[0] is (role == 'photo_worker')


def test_shown_removal_admission_rejects_invalid_body(database):
    from uuid import uuid4
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        accepted = database.execute('SELECT photo.admit_semantic_shown_removal(%s,%s,%s,1,1,%s,%s,%s,%s,%s)',
            (uuid4(), uuid4(), uuid4(), uuid4(), uuid4(), 'a'*64, '{}', 'b'*64)).fetchone()[0]
        assert accepted is False


def test_shown_removal_adoption_readers_are_scheduler_only(database):
    signatures = (
        'photo.v27_shown_removal_adoption_params(uuid,uuid,integer,bigint,uuid)',
        'photo.v27_shown_removal_adoption_context(uuid,uuid)',
        'photo.v27_pending_shown_removal_adoptions()',
        'photo.v27_shown_removal_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text)',
        'photo.v27_shown_removal_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_scheduler')


def test_shown_removal_adoption_missing_task_is_not_discovered(database):
    from uuid import uuid4
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        assert database.execute('SELECT * FROM photo.v27_shown_removal_adoption_context(%s,%s)', (uuid4(), uuid4())).fetchall() == []


def test_shown_removal_commit_has_no_generic_or_private_bypass(database):
    private = (
        'photo.adopt_shown_removal_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text)',
        'photo.shown_removal_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text)',
        'photo.adopt_v2_plan_before_shown_removal(uuid,uuid,integer,bigint,uuid,jsonb,text)',
    )
    for signature in private:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False
    signature = 'photo.adopt_shown_removal(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_scheduler')


def test_shown_removal_commit_rejects_absent_task(database):
    from uuid import uuid4
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        row = database.execute("SELECT photo.adopt_shown_removal(%s,%s,1,1,%s,'[]'::jsonb,%s,%s,%s,%s,%s,%s,%s,%s)",
            (uuid4(), uuid4(), uuid4(), 'a'*64, uuid4(), 'b'*64, '{}', uuid4(), 'c'*64, '{}', '{}')).fetchone()
        assert row[0] is False


def test_shown_removal_document_preflight_is_admission_only(database):
    signature = 'photo.semantic_shown_removal_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_semantic_admission')


def test_shown_removal_profile_gate_rejects_empty_image(database):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute("SELECT photo.semantic_shown_removal_profile_bound('{}'::jsonb,'{}'::jsonb,'{}'::jsonb)").fetchone()[0] is False


def test_shown_removal_batch_locks_remain_private(database):
    signatures = (
        'photo.lock_semantic_shown_removal_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb)',
        'photo.semantic_shown_removal_batch_nodes_bound(uuid,jsonb,jsonb)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False


def test_shown_removal_batch_rejects_empty_and_unmatched_nodes(database):
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    database.execute('SET LOCAL ROLE photo_fn_owner')
    for decisions, tasks in (([], []), ([{'target_id':str(uuid4()),'decision':'approve'}], [])):
        assert database.execute('SELECT photo.semantic_shown_removal_batch_nodes_bound(%s,%s,%s)', (uuid4(), Jsonb(decisions), Jsonb(tasks))).fetchone()[0] is False


def test_shown_removal_draft_quota_components_are_private(database):
    signatures = (
        'photo.create_semantic_shown_removal_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
        'photo.semantic_shown_removal_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb)',
        'photo.reserve_semantic_shown_removal_apply_quota(uuid,uuid,uuid,uuid,jsonb)',
        'photo.create_semantic_shown_removal_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False


def test_shown_removal_empty_draft_is_not_ready(database):
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute("SELECT photo.semantic_shown_removal_apply_draft_ready(%s,%s,%s,%s,'[]'::jsonb)", (uuid4(), uuid4(), uuid4(), uuid4())).fetchone()[0] is False


def test_shown_removal_registration_sealing_are_private(database):
    signatures = (
        'photo.semantic_shown_removal_apply_input_scope(uuid,uuid,uuid)',
        'photo.register_semantic_shown_removal_apply_inputs(uuid,uuid,uuid,uuid,jsonb)',
        'photo.semantic_shown_removal_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb)',
        'photo.seal_semantic_shown_removal_apply_inputs(uuid,uuid,uuid,uuid,jsonb)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is False


def test_shown_removal_input_scope_rejects_missing_task(database):
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_shown_removal_apply_input_scope(%s,%s,%s)', (uuid4(), uuid4(), uuid4())).fetchone()[0] is False


def test_shown_removal_atomic_commit_is_only_admission_entrypoint(database):
    signatures = (
        ('photo.activate_semantic_shown_removal_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb)', None),
        ('photo.finish_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb)', None),
        ('photo.commit_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)', 'photo_semantic_admission'),
    )
    for signature, allowed in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == allowed)


def test_shown_removal_claim_delegate_is_private(database):
    signature = 'photo.claim_task_before_shown_removal(uuid,integer,uuid,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


def test_shown_removal_catalog_stays_disabled_and_cpu_only(database):
    row = database.execute("SELECT resource_class, routing_key, model_id, timeout_seconds, backends, v2_enabled FROM photo.operation_registry WHERE operation='semantic.adopt_shown_removal' AND version='1'").fetchone()
    assert row is not None
    assert tuple(row) == ('cpu', 'cpu.semantic_adopt_shown_removal', None, 1800, ['cpu'], False)
    policies = database.execute("SELECT role,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='semantic.adopt_shown_removal' AND version='1' ORDER BY role").fetchall()
    assert [tuple(row) for row in policies] == [('pixels', 2400016384, 1), ('working_image', 1048576, 1)]


def test_shown_removal_live_io_helpers_are_private(database):
    for signature in (
        'photo.semantic_shown_removal_task_live(uuid)',
        'photo.gateway_input_before_shown_removal(uuid,integer,bigint,uuid,text,uuid)',
        'photo.heartbeat_task_before_shown_removal(uuid,integer,bigint,uuid,jsonb)',
    ):
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


def test_shown_removal_missing_task_cannot_refresh_or_read(database):
    from uuid import uuid4
    task, worker, artifact = uuid4(), uuid4(), uuid4()
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_shown_removal_task_live(%s)', (task,)).fetchone()[0] is False
    row = database.execute("SELECT * FROM photo.heartbeat_task(%s,1,1,%s,'{}'::jsonb)", (task, worker)).fetchone()
    assert tuple(row) == (False, None, True)
    rows = database.execute("SELECT * FROM photo.gateway_input(%s,1,1,%s,'missing',%s)", (task, worker, artifact)).fetchall()
    assert rows == []


def test_shown_copy_finalizers_are_scheduler_only(database):
    for signature in (
        'photo.finalize_shown_semantic_apply_child(uuid,uuid)',
        'photo.finalize_next_shown_semantic_apply_child()',
    ):
        for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_scheduler')


def test_shown_copy_finalizer_rejects_missing_tenant_and_run(database):
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.finalize_shown_semantic_apply_child(%s,%s)', (uuid4(), uuid4())).fetchone()[0] == 'stale'


def test_shown_removal_applied_projection_is_api_only(database):
    signature = 'photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_shown_applied_projection_rejects_unknown_authority(database):
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT * FROM photo.semantic_shown_applied_image(%s,%s,%s,%s)', (uuid4(),uuid4(),uuid4(),uuid4())).fetchall() == []


def test_shown_removal_review_report_metadata_is_api_only(database):
    signature = 'photo.semantic_shown_removal_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_preview_intent_dispatch_metadata_is_api_only(database):
    signature='photo.semantic_preview_intent_object(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_removal_preview_finalization_is_scheduler_only(database):
    for signature, allowed in (
        ('photo.finalize_next_semantic_removal_preview()', 'photo_scheduler'),
        ('photo.finalize_next_semantic_preapply_preview()', 'photo_scheduler'),
        ('photo.finalize_next_semantic_preapply_preview_before_removal()', None),
    ):
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


def test_removal_bundle_commit_delegate_is_private(database):
    signature = 'photo.commit_task_success_before_removal_bundle(uuid,integer,bigint,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role,signature)).fetchone()[0] is False


def test_background_cleanup_preview_catalog_is_disabled_and_has_exact_output_policies(database):
    import json
    from pathlib import Path
    operation='semantic.preview_background_cleanup'
    row=database.execute("SELECT resource_class,model_id,backends,v2_enabled FROM photo.operation_registry WHERE operation=%s AND version='1'",(operation,)).fetchone()
    assert row is not None and row[0]=='gpu' and row[1]=='big_lama'
    assert row[2]==['cpu','cuda','mps'] and row[3] is False
    normative=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
    contract=next(value for value in normative if value['id']==operation)
    rows=database.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation=%s AND version='1'",(operation,)).fetchall()
    actual={role:(media,maximum,count) for role,media,maximum,count in rows}
    expected={role:(policy['media_types'],policy['max_bytes'],policy['max_count']) for role,policy in contract['role_policies'].items()}
    assert actual==expected and len(actual)==8


def test_background_cleanup_worker_authority_functions_are_worker_only(database):
    signatures=(
        'photo.admit_semantic_background_cleanup_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        'photo.v27_background_cleanup_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        'photo.semantic_background_cleanup_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
    )
    for signature in signatures:
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_worker')


def test_background_cleanup_inventory_digest_rejects_removal_namespace(database):
    from hashlib import sha256
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    from photo_worker.contracts.base import canonical_bytes
    task,params=uuid4(),uuid4()
    entries=[dict(contract='Params',ref=dict(artifact_id=str(params),sha256='a'*64))]
    entries_sha=sha256(canonical_bytes(entries)).hexdigest()
    def digest(operation):
        return sha256(canonical_bytes(dict(entries=entries,operation=operation,task=str(task),version='1'))).hexdigest()
    database.execute('SET LOCAL ROLE photo_fn_owner')
    query='SELECT photo.semantic_background_cleanup_preview_inventory_digest_ready(%s,%s,%s,%s,%s,%s)'
    assert database.execute(query,(task,params,'a'*64,digest('semantic.preview_background_cleanup'),entries_sha,Jsonb(entries))).fetchone()[0] is True
    assert database.execute(query,(task,params,'a'*64,digest('semantic.preview_remove_object'),entries_sha,Jsonb(entries))).fetchone()[0] is False


@pytest.mark.parametrize('body,attempt,fence',[('{}',0,1),('{}',1,0),('not-json',1,1)])
def test_background_cleanup_admission_rejects_invalid_unscoped_task(database,body,attempt,fence):
    from hashlib import sha256
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_worker')
    args=(uuid4(),uuid4(),uuid4(),attempt,fence,uuid4(),uuid4(),sha256(body.encode()).hexdigest(),body,'a'*64)
    assert database.execute('SELECT photo.admit_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is False
    assert database.execute('SELECT photo.v27_background_cleanup_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is None


def test_background_cleanup_saved_run_documents_and_quota_are_api_only(database):
    signatures=(
        'photo.save_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,uuid,text,text)',
        'photo.reserve_semantic_background_cleanup_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text)',
        'photo.complete_semantic_background_cleanup_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text)',
        'photo.reserve_semantic_background_cleanup_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb)',
    )
    for signature in signatures:
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


@pytest.mark.parametrize('operation',['semantic.preview_local_exposure','semantic.preview_remove_object'])
def test_background_cleanup_document_reservation_rejects_foreign_task_namespace(database,operation):
    from uuid import uuid4,uuid5
    from hashlib import sha256
    tenant,intent,actor,session,target,run=(uuid4() for _ in range(6))
    task=uuid5(run,operation+':'+str(target))
    params=uuid5(task,'Params');raw='{}';digest=sha256(raw.encode()).hexdigest()
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT * FROM photo.reserve_semantic_background_cleanup_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (tenant,intent,actor,session,target,run,raw,digest,'task_params',task,params,digest,2,'photos'))


@pytest.mark.parametrize('kind',['local_exposure','remove_object','retouch_region'])
def test_background_cleanup_saved_run_rejects_foreign_kind(database,kind):
    import json
    from uuid import uuid4
    from hashlib import sha256
    raw=json.dumps(dict(schema_version=1,kind=kind))
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT photo.save_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s)',
                (*[uuid4() for _ in range(5)],raw,sha256(raw.encode()).hexdigest()))


def test_background_cleanup_materialization_keeps_components_private(database):
    arguments='uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb'
    for function in ('create_semantic_background_cleanup_preview_draft_task',
                     'register_semantic_background_cleanup_preview_inputs',
                     'seal_semantic_background_cleanup_preview_inputs',
                     'activate_semantic_background_cleanup_preview',
                     'commit_semantic_background_cleanup_preview'):
        signature='photo.'+function+'('+arguments+')'
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            allowed=role=='photo_api' and function=='commit_semantic_background_cleanup_preview'
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is allowed


def test_background_input_guard_preserves_prior_operation_scopes(database):
    source=database.execute("SELECT pg_get_functiondef('photo.guard_v2_artifact_link()'::regprocedure)").fetchone()[0]
    for function in ('semantic_apply_input_scope','semantic_preapply_preview_input_scope',
                     'semantic_removal_preview_input_scope','semantic_background_cleanup_preview_input_scope',
                     'semantic_shown_apply_input_scope','semantic_shown_removal_apply_input_scope'):
        assert 'photo.'+function+'(NEW.tenant_id,NEW.project_id,NEW.task_id)' in source
    assert "WHEN 'v2_documents'" in source and "WHEN 'v2_artifact_links'" in source
    assert "'V2_ARTIFACT_UNVERIFIED'" in source and "'V2_ARTIFACT_LINK_SCOPE_INVALID'" in source


def test_background_preview_inspection_is_api_only(database):
    signature='photo.inspect_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,text,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_background_scope_and_current_reject_missing_authority(database):
    from uuid import uuid4
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_background_cleanup_preview_input_scope(%s,%s,%s)',(uuid4(),uuid4(),uuid4())).fetchone()[0] is False
    assert database.execute('SELECT photo.semantic_background_cleanup_preview_run_current(%s,%s)',(uuid4(),uuid4())).fetchone()[0] is False


def test_background_cleanup_live_chain_delegates_are_private(database):
    signatures=(
        'photo.claim_task_before_background_cleanup_preview(uuid,integer,uuid,jsonb)',
        'photo.gateway_input_before_background_cleanup_preview(uuid,integer,bigint,uuid,text,uuid)',
        'photo.heartbeat_task_before_background_cleanup_preview(uuid,integer,bigint,uuid,jsonb)',
        'photo.commit_task_success_before_background_cleanup_preview(uuid,integer,bigint,uuid,text)',
        'photo.commit_task_success_before_background_cleanup_bundle(uuid,integer,bigint,uuid,text)',
        'photo.finalize_next_semantic_preapply_preview_before_bg()',
    )
    for signature in signatures:
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


def test_background_cleanup_result_sources_and_finalizer_roles(database):
    for signature,allowed in (
        ('photo.semantic_background_cleanup_preview_presentation_source(uuid,uuid,uuid,uuid,text,text)','photo_api'),
        ('photo.semantic_background_cleanup_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text)','photo_api'),
        ('photo.finalize_next_semantic_background_cleanup_preview()','photo_scheduler'),
        ('photo.finalize_next_semantic_preapply_preview()','photo_scheduler'),
    ):
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


def test_background_cleanup_lookup_and_receipt_are_api_only(database):
    for signature in (
        'photo.lookup_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)',
        'photo.seal_semantic_background_cleanup_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text)',
    ):
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
            assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_background_cleanup_selection_lock_is_admission_only(database):
    signature = 'photo.lock_semantic_background_cleanup_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0] is (role == 'photo_semantic_admission')



@pytest.mark.parametrize("signature,allowed", [('photo.lock_semantic_shown_background_cleanup_params(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,jsonb)', 'photo_semantic_admission'), ('photo.lock_semantic_shown_background_cleanup_publication(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,uuid)', None), ('photo.reserve_semantic_shown_background_cleanup_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,text,bigint,text)', 'photo_api'), ('photo.complete_semantic_shown_background_cleanup_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text)', 'photo_api')])
def test_shown_background_cleanup_transport_acl(database, signature, allowed):
    for role in ("photo_api", "photo_worker", "photo_scheduler", "photo_portal", "photo_gateway", "photo_semantic_admission"):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == allowed)


@pytest.mark.parametrize("signature,allowed", [('photo.semantic_shown_background_cleanup_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)', ()), ('photo.semantic_shown_background_cleanup_input_objects_ready(uuid,jsonb)', ()), ('photo.semantic_shown_background_cleanup_transport_documents_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)', ()), ('photo.semantic_shown_background_cleanup_profile_bound(jsonb,jsonb,jsonb)', ()), ('photo.semantic_shown_background_cleanup_approved_edit_bound(jsonb,text,jsonb,jsonb,jsonb)', ()), ('photo.semantic_shown_background_cleanup_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)', ('photo_semantic_admission',)), ('photo.lock_semantic_shown_background_cleanup_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb)', ()), ('photo.semantic_shown_background_cleanup_batch_nodes_bound(uuid,jsonb,jsonb)', ()), ('photo.create_semantic_shown_background_cleanup_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)', ()), ('photo.semantic_shown_background_cleanup_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.reserve_semantic_shown_background_cleanup_apply_quota(uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.create_semantic_shown_background_cleanup_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)', ()), ('photo.semantic_shown_background_cleanup_apply_input_scope(uuid,uuid,uuid)', ()), ('photo.register_semantic_shown_background_cleanup_apply_inputs(uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.semantic_shown_background_cleanup_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.seal_semantic_shown_background_cleanup_apply_inputs(uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.activate_semantic_shown_background_cleanup_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb)', ()), ('photo.finish_semantic_shown_background_cleanup_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb)', ()), ('photo.commit_semantic_shown_background_cleanup_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)', ('photo_semantic_admission',))])
def test_shown_background_cleanup_atomic_components_acl(database, signature, allowed):
    row = database.execute("SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)", (signature,)).fetchone()
    assert row == ("photo_fn_owner", True)
    for role in ("photo_api", "photo_worker", "photo_scheduler", "photo_portal", "photo_gateway", "photo_semantic_admission"):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role in allowed)


def test_shown_background_cleanup_guard_preserves_all_previous_scopes(database):
    body = database.execute("SELECT pg_get_functiondef('photo.guard_v2_artifact_link()'::regprocedure)").fetchone()[0]
    for name in ('semantic_apply_input_scope', 'semantic_preapply_preview_input_scope', 'semantic_removal_preview_input_scope', 'semantic_background_cleanup_preview_input_scope', 'semantic_shown_apply_input_scope', 'semantic_shown_removal_apply_input_scope', 'semantic_shown_background_cleanup_apply_input_scope'):
        assert 'photo.' + name + '(' in body


def test_shown_background_cleanup_review_report_is_api_only(database):
    signature='photo.semantic_shown_background_cleanup_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_shown_background_cleanup_live_predicate_is_private(database):
    signature='photo.semantic_shown_background_cleanup_task_live(uuid)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


def test_shown_background_cleanup_claim_live_wrappers_preserve_previous_chain(database):
    for signature, predecessor in (
        ('photo.claim_task(uuid,integer,uuid,jsonb)','claim_task_before_shown_background_cleanup'),
        ('photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)','gateway_input_before_shown_background_cleanup'),
        ('photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)','heartbeat_task_before_shown_background_cleanup'),
    ):
        body=database.execute('SELECT pg_get_functiondef(%s::regprocedure)',(signature,)).fetchone()[0]
        # Current public entrypoints delegate through later retouch/activation
        # wrappers. Assert every actual edge, not just any historical marker.
        if signature.startswith('photo.claim_task('):
            chain=('claim_task_before_v2_activation_guard','claim_task_before_shown_retouch','claim_task_before_retouch_preview',predecessor)
        else:
            stem=signature.split('.')[1].split('(')[0]
            chain=(stem+'_before_shown_retouch',stem+'_before_retouch_preview',predecessor)
        for delegate in chain:
            assert 'photo.'+delegate+'(' in body
            delegate_signature='photo.'+delegate+signature[signature.index('('):]
            body=database.execute('SELECT pg_get_functiondef(%s::regprocedure)',(delegate_signature,)).fetchone()[0]
        prior=database.execute("SELECT pg_get_functiondef(oid) FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=%s",(predecessor,)).fetchone()[0]
        assert 'background_cleanup_preview' in prior


def test_background_cleanup_publication_requires_explicit_current_authority(database):
    """Installed-definition regression only; not a positive admission fixture."""
    signature = 'photo.lock_semantic_background_cleanup_preview_publication(uuid,uuid,uuid,uuid,uuid,uuid,text,text)'
    source = database.execute(
        'SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)
    ).fetchone()
    assert source is not None
    predicate = "e->'plan'->>'sha256',e->'candidate'->>'sha256') IS DISTINCT FROM true"
    assert predicate in source[0]
    # NULL from a nested authority predicate must deny, just as false does.
    for authority, denied in ((None, True), (False, True), (True, False)):
        assert database.execute('SELECT %s::boolean IS DISTINCT FROM true', (authority,)).fetchone()[0] is denied


def test_background_cleanup_quota_replay_rejects_share_reservation_definition(database):
    """Align replay's installed guard with activation; does not exercise quota IO."""
    signature = 'photo.reserve_semantic_background_cleanup_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb)'
    source = database.execute(
        'SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)
    ).fetchone()
    assert source is not None
    assert 'old.share_id IS NULL' in source[0]


@pytest.mark.parametrize('signature', [
    'photo.admit_semantic_background_cleanup_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
    'photo.semantic_background_cleanup_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
    'photo.v27_background_cleanup_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
])
def test_background_cleanup_worker_guards_require_explicit_true_definition(database,signature):
    """Installed-definition regression, not a full live worker admission fixture."""
    import re
    row = database.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None
    assert re.search(r'\bNOT\s+photo\.[a-z0-9_]+\(', row[0], re.IGNORECASE) is None
    assert 'IS DISTINCT FROM true' in row[0]
