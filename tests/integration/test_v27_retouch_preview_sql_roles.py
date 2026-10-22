"""Retouch SQL authority scenarios in an isolated per-case integration database."""
from hashlib import sha256
from uuid import uuid4

import psycopg
from conftest import database as isolated_database_config
import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


@pytest.fixture
def database(isolated_database_config):
    """Own a rollback connection inside the per-case disposable database."""
    with psycopg.connect(**isolated_database_config) as connection:
        try:
            yield connection
        finally:
            connection.rollback()


@pytest.mark.parametrize('signature', [
    'photo.admit_semantic_retouch_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
    'photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text)',
])
def test_retouch_authority_is_worker_only_definer(database, signature):
    row = database.execute(
        'SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',
        (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_worker')


@pytest.mark.parametrize('operation', [
    'semantic.preview_retouch_region', 'semantic.preview_background_cleanup',
    'semantic.preview_remove_object', 'semantic.preview_local_exposure',
])
def test_retouch_inventory_uses_own_digest_namespace(database, operation):
    task, params = uuid4(), uuid4()
    entries = [dict(contract='Params', ref=dict(artifact_id=str(params), sha256='a'*64))]
    entries_sha = sha256(canonical_bytes(entries)).hexdigest()
    digest = sha256(canonical_bytes(dict(entries=entries, operation=operation, task=str(task), version='1'))).hexdigest()
    database.execute('SET LOCAL ROLE photo_fn_owner')
    result = database.execute(
        'SELECT photo.semantic_retouch_preview_inventory_digest_ready(%s,%s,%s,%s,%s,%s)',
        (task, params, 'a'*64, digest, entries_sha, Jsonb(entries))).fetchone()[0]
    assert result is (operation == 'semantic.preview_retouch_region')


@pytest.mark.parametrize('body,attempt,fence', [('{}',0,1), ('{}',1,0), ('not-json',1,1)])
def test_retouch_unscoped_task_has_no_admission_or_report(database, body, attempt, fence):
    database.execute('SET LOCAL ROLE photo_worker')
    args = (uuid4(), uuid4(), uuid4(), attempt, fence, uuid4(), uuid4(), sha256(body.encode()).hexdigest(), body, 'a'*64)
    assert database.execute('SELECT photo.admit_semantic_retouch_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0] is False
    context = b'{"algorithm":"beautyfilter_v1"}'
    report_args = args + (uuid4(), sha256(context).hexdigest(), context.decode())
    assert database.execute('SELECT photo.v27_retouch_preview_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', report_args).fetchone()[0] is None


def test_retouch_saved_run_is_api_only_definer(database):
    signature = 'photo.save_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)'
    row = database.execute(
        'SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',
        (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')


@pytest.mark.parametrize('failure', ['nil_context', 'changed_hash', 'oversize', 'foreign_source', 'non_array_faces'])
def test_retouch_save_rejects_context_before_review_authority(database, failure):
    from pathlib import Path
    from uuid import UUID
    import json
    root = Path(__file__).resolve().parents[2]/'backend/internal/semanticreview/testdata'
    params = (root/'retouch_preview_params.json').read_bytes()
    document = json.loads((root/'retouch_preview_context.json').read_bytes())
    context_id = uuid4()
    if failure == 'nil_context':
        context_id = UUID(int=0)
    if failure == 'foreign_source':
        document['source']['asset_version_id'] = str(uuid4())
    if failure == 'non_array_faces':
        document['faces'] = {}
    context = canonical_bytes(document)
    digest = sha256(context).hexdigest()
    if failure == 'changed_hash':
        digest = '0'*64
    if failure == 'oversize':
        context = b' '*(1048576+1)
        digest = sha256(context).hexdigest()
    scope = json.loads(params)
    database.execute('SET LOCAL ROLE photo_api')
    with pytest.raises(psycopg.errors.InvalidParameterValue, match='SEMANTIC_RETOUCH_CONTEXT_INVALID'):
        database.execute('SELECT photo.save_semantic_retouch_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                         (UUID(scope['tenant_id']), UUID(scope['intent_id']), uuid4(), uuid4(), UUID(scope['target_id']),
                          params.decode(), sha256(params).hexdigest(), context_id, digest, context.decode()))


@pytest.mark.parametrize('signature', [
    'photo.reserve_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text)',
    'photo.complete_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text)',
])
def test_retouch_document_tickets_are_api_only(database, signature):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_retouch_publication_lock_is_private(database):
    signature = 'photo.lock_semantic_retouch_preview_publication(uuid,uuid,uuid,uuid,uuid,uuid,text,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False



def test_retouch_quota_reservation_is_api_only(database):
    signature = 'photo.reserve_semantic_retouch_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')


@pytest.mark.parametrize('signature', [
    'photo.semantic_retouch_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb)',
    'photo.semantic_retouch_preview_protection_ready(uuid,uuid,jsonb,uuid,text)',
])
def test_retouch_document_provenance_is_private(database, signature):
    row = database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


@pytest.mark.parametrize('bad', [None, {}, [], {'entries': []}])
def test_retouch_document_gate_refuses_incomplete_transport(database, bad):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_retouch_preview_documents_ready(%s,%s,%s,%s,%s,%s)',
                            (uuid4(), uuid4(), Jsonb({}), Jsonb({}), Jsonb(bad), Jsonb(bad))).fetchone()[0] is False


@pytest.mark.parametrize('signature', [
    'photo.semantic_retouch_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)',
    'photo.semantic_retouch_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)',
    'photo.semantic_retouch_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)',
])
def test_retouch_candidate_image_inventory_predicates_are_private(database, signature):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


@pytest.mark.parametrize('failure', ['', 'missing_landmarks', 'extra_root', 'wrong_contract', 'context_candidate', 'manual_mask'])
def test_retouch_exact_inventory_includes_frozen_face_context(database, failure):
    """Pure projection, synthetic documents; no provenance/dispatch claim."""
    import json
    from pathlib import Path
    root = Path(__file__).resolve().parents[2]/'backend/internal/semanticreview/testdata'
    params = json.loads((root/'retouch_preview_params.json').read_bytes())
    context = json.loads((root/'retouch_preview_context.json').read_bytes())
    mask = json.loads((root/'retouch_preview_manual_mask.json').read_bytes())
    def ref():
        return dict(artifact_id=str(uuid4()), sha256='f'*64)
    candidate = dict(mask=context['transferred_mask'], transform=ref())
    intent = dict(reference=dict(source=params['source']['source']))
    source = dict(pixels=ref(), alpha=None, transform_chain=dict(steps=[]))
    protection = dict(artifact=ref())
    node = dict(params_artifact_id=str(uuid4()), params_sha='a'*64, execution_profile=dict(retouch_context=ref()))
    roots = [
        (dict(artifact_id=node['params_artifact_id'], sha256=node['params_sha']), 'Params'),
        (params['plan'], 'SemanticPlan'), (params['intent'], 'EditIntent'),
        (params['candidate'], 'TransferCandidate'), (params['source']['source'], 'WorkingImage'),
        (params['protection'], 'MaskRef'), (candidate['mask'], 'MaskRef'),
        (candidate['transform'], 'ResolvedTransform'), (source['pixels'], 'NpyArray'),
        (mask['artifact'], 'NpyArray'), (protection['artifact'], 'NpyArray'),
        (node['execution_profile']['retouch_context'], 'RetouchPreviewAdapterContext'),
        (context['faces'][0]['landmarks'], 'Face68'),
    ]
    entries = [dict(ref=pin, contract=contract) for pin, contract in roots]
    if failure == 'missing_landmarks':
        entries = [e for e in entries if e['contract'] != 'Face68']
    if failure == 'extra_root':
        entries.append(dict(ref=ref(), contract='NpyArray'))
    if failure == 'wrong_contract':
        next(e for e in entries if e['contract'] == 'Face68')['contract'] = 'FaceLandmarks'
    if failure == 'context_candidate':
        context['candidate'] = ref()
    if failure == 'manual_mask':
        context['faces'][0]['manual']['confidence'] = .2
    entries.sort(key=lambda e: e['ref']['artifact_id'])
    database.execute('SET LOCAL ROLE photo_fn_owner')
    result = database.execute('SELECT photo.semantic_retouch_preview_inventory_exact(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                              tuple(Jsonb(x) for x in (params, node, dict(entries=entries), candidate, intent, source, source, mask, protection, context))).fetchone()[0]
    assert result is (failure == '')


@pytest.mark.parametrize('signature,worker', [
    ('photo.semantic_retouch_preview_landmark_origin(uuid,uuid,uuid,uuid,jsonb,uuid,text)', False),
    ('photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text)', True),
])
def test_retouch_landmark_authority_has_narrow_acl(database, signature, worker):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (worker and role == 'photo_worker')


def test_retouch_worker_landmark_refuses_unleased_task(database):
    database.execute('SET LOCAL ROLE photo_worker')
    body = '{}'
    args = (uuid4(), uuid4(), uuid4(), 1, 1, uuid4(), uuid4(), sha256(body.encode()).hexdigest(), body, 'a'*64, uuid4(), 'b'*64)
    assert database.execute('SELECT photo.semantic_retouch_preview_worker_landmark_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0] is False


def test_retouch_aggregate_admission_is_private(database):
    signature = 'photo.semantic_retouch_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)'
    row = database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


@pytest.mark.parametrize('position', [0, 1, 2])
def test_retouch_aggregate_rejects_invalid_transport_shape_before_authority(database, position):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    documents = [Jsonb({}), Jsonb({}), Jsonb({})]
    documents[position] = Jsonb([])
    args = (uuid4(), uuid4(), uuid4(), uuid4(), uuid4(), uuid4()) + ('{}', 'a'*64) + ('{}',)*7 + tuple(documents)
    assert database.execute('SELECT photo.semantic_retouch_preview_admission_inputs_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0] is False


@pytest.mark.parametrize('signature', [
    'photo.semantic_retouch_preview_run_current(uuid,uuid)',
    'photo.semantic_retouch_preview_input_scope(uuid,uuid,uuid)',
    'photo.create_semantic_retouch_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)',
    'photo.register_semantic_retouch_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)',
])
def test_retouch_registration_components_are_private(database, signature):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


def test_retouch_missing_task_has_no_registration_scope(database):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_retouch_preview_input_scope(%s,%s,%s)', (uuid4(),uuid4(),uuid4())).fetchone()[0] is False


@pytest.mark.parametrize('name,api', [
    ('seal_semantic_retouch_preview_inputs', False),
    ('activate_semantic_retouch_preview', False),
    ('commit_semantic_retouch_preview', True),
])
def test_retouch_activation_chain_has_one_api_entry(database, name, api):
    signature = 'photo.' + name + '(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)'
    row = database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (api and role == 'photo_api')


@pytest.mark.parametrize('signature', [
    'photo.claim_task_before_retouch_preview(uuid,integer,uuid,jsonb)',
    'photo.gateway_input_before_retouch_preview(uuid,integer,bigint,uuid,text,uuid)',
    'photo.heartbeat_task_before_retouch_preview(uuid,integer,bigint,uuid,jsonb)',
    'photo.semantic_retouch_preview_task_live(uuid)',
])
def test_retouch_claim_and_io_predecessors_are_private(database, signature):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


@pytest.mark.parametrize('signature,allowed', [
    ('photo.claim_task(uuid,integer,uuid,jsonb)', 'photo_worker'),
    ('photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)', 'photo_worker'),
    ('photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)', 'photo_gateway'),
])
def test_retouch_claim_and_io_public_wrappers_have_exact_role(database, signature, allowed):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == allowed)


def test_retouch_unknown_task_has_no_live_io_scope(database):
    database.execute('SET LOCAL ROLE photo_fn_owner')
    assert database.execute('SELECT photo.semantic_retouch_preview_task_live(%s)', (uuid4(),)).fetchone()[0] is None



def test_retouch_inspection_is_api_only(database):
    signature = 'photo.inspect_semantic_retouch_preview(uuid,uuid,uuid,uuid,text,text,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == 'photo_api')



def test_retouch_worker_protection_origin_is_worker_only(database):
    signature = 'photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_worker')


def test_retouch_bundle_predicate_is_private_definer_and_refuses_missing_task(database):
    signature='photo.semantic_retouch_preview_bundle_ready(uuid,uuid,integer,bigint)'
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
    assert database.execute('SELECT photo.semantic_retouch_preview_bundle_ready(%s,%s,%s,%s)',(uuid4(),uuid4(),1,1)).fetchone()[0] is False


def test_retouch_result_predecessor_is_not_worker_callable(database):
    signature='photo.commit_task_success_before_retouch_bundle(uuid,integer,bigint,uuid,text)'
    assert database.execute('SELECT to_regprocedure(%s) IS NOT NULL',(signature,)).fetchone()[0] is True
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


@pytest.mark.parametrize('signature',[
    'photo.semantic_retouch_preview_dispatch_ready(uuid)',
    'photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
])
def test_retouch_dispatch_components_are_private(database,signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


def test_retouch_dispatch_predicate_refuses_missing_task(database):
    assert database.execute('SELECT photo.semantic_retouch_preview_dispatch_ready(%s)',(uuid4(),)).fetchone()[0] is False


def test_retouch_adoption_params_are_scheduler_only_and_refuse_missing_generation(database):
    signature='photo.v27_retouch_preview_adoption_params(uuid,uuid,integer,bigint,uuid)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    assert database.execute('SELECT * FROM photo.v27_retouch_preview_adoption_params(%s,%s,%s,%s,%s)',(uuid4(),uuid4(),1,1,uuid4())).fetchall()==[]


def test_retouch_receipt_is_api_only(database):
    signature='photo.seal_semantic_retouch_preview_receipt(uuid,uuid,uuid,uuid,text,text,uuid,text,uuid,uuid,integer,bigint,text,text,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_retouch_landmark_catalog_is_api_only(database):
    signature='photo.semantic_retouch_preview_landmark_catalog(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


def test_retouch_worker_selection_predecessor_remains_private(database):
    signature='photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


def test_retouch_selection_lock_is_admission_only(database):
    signature='photo.lock_semantic_retouch_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_semantic_admission')


def test_shown_retouch_document_preflight_is_admission_only(database):
    signature='photo.semantic_shown_retouch_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_semantic_admission')


@pytest.mark.parametrize('signature',[
    'photo.lock_semantic_shown_retouch_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb)',
    'photo.semantic_shown_retouch_batch_nodes_bound(uuid,jsonb,jsonb)',
])
def test_shown_retouch_batch_components_are_private(database,signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


@pytest.mark.parametrize('decisions,tasks',[
    ([], []),
    ([{'target_id':None,'decision':'approve'}], [{}]),
    ([{'target_id':'00000000-0000-0000-0000-000000000000','decision':'approve'}], [{}]),
    ([{'target_id':str(uuid4()),'decision':None}], [{}]),
    ([{'target_id':str(uuid4()),'decision':'reject'}], [{}]),
])
def test_shown_retouch_batch_refuses_malformed_membership(database,decisions,tasks):
    import json
    assert database.execute('SELECT photo.semantic_shown_retouch_batch_nodes_bound(%s,%s::jsonb,%s::jsonb)',
        (uuid4(),json.dumps(decisions),json.dumps(tasks))).fetchone()[0] is False


@pytest.mark.parametrize('signature',[
    'photo.create_semantic_shown_retouch_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
    'photo.semantic_shown_retouch_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb)',
    'photo.reserve_semantic_shown_retouch_apply_quota(uuid,uuid,uuid,uuid,jsonb)',
    'photo.create_semantic_shown_retouch_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',
])
def test_shown_retouch_reserved_draft_components_are_private(database,signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


@pytest.mark.parametrize('tasks',[None,{},[],[{}]])
def test_shown_retouch_draft_refuses_missing_scope(database,tasks):
    import json
    assert database.execute('SELECT photo.semantic_shown_retouch_apply_draft_ready(%s,%s,%s,%s,%s::jsonb)',
        (uuid4(),uuid4(),uuid4(),uuid4(),json.dumps(tasks))).fetchone()[0] is False


def test_shown_retouch_persisted_closure_is_private_and_refuses_missing_task(database):
    signature='photo.semantic_shown_retouch_closure_ready(uuid,jsonb)'
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
    assert database.execute('SELECT photo.semantic_shown_retouch_closure_ready(%s,%s::jsonb)',(uuid4(),'{}')).fetchone()[0] is False


@pytest.mark.parametrize('signature',[
    'photo.semantic_shown_retouch_apply_input_scope(uuid,uuid,uuid)',
    'photo.register_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb)',
    'photo.semantic_shown_retouch_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb)',
    'photo.seal_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb)',
])
def test_shown_retouch_input_sealing_is_private(database,signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False


@pytest.mark.parametrize('tasks',[None,{},[],[{}]])
def test_shown_retouch_sealed_inputs_refuse_missing_scope(database,tasks):
    import json
    assert database.execute('SELECT photo.semantic_shown_retouch_apply_sealed_inputs_ready(%s,%s,%s,%s,%s::jsonb)',
        (uuid4(),uuid4(),uuid4(),uuid4(),json.dumps(tasks))).fetchone()[0] is False


def test_shown_retouch_input_scope_refuses_missing_task(database):
    assert database.execute('SELECT photo.semantic_shown_retouch_apply_input_scope(%s,%s,%s)',(uuid4(),uuid4(),uuid4())).fetchone()[0] is False


@pytest.mark.parametrize('signature,allowed',[
    ('photo.activate_semantic_shown_retouch_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb)',None),
    ('photo.finish_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb)',None),
    ('photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)','photo_semantic_admission'),
])
def test_shown_retouch_atomic_commit_has_one_admission_entry(database,signature,allowed):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


def test_shown_retouch_activation_refuses_missing_tenant(database):
    assert database.execute('SELECT photo.activate_semantic_shown_retouch_apply_child(%s,%s,%s,%s,%s,%s::jsonb)',
        (uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'[]')).fetchone()[0] is False


def test_shown_retouch_prechild_report_is_api_only(database):
    signature='photo.semantic_shown_retouch_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')
    # An unknown intent has no authenticated semantic identity; it is forbidden,
    # rather than an authorized report whose evidence is absent.
    database.execute('SET LOCAL ROLE photo_api')
    with database.transaction():
        with pytest.raises(psycopg.errors.InsufficientPrivilege) as refused:
            with database.transaction():
                database.execute('SELECT photo.semantic_shown_retouch_review_report_evidence(%s,%s,%s,%s,%s,%s::jsonb)',
                    (uuid4(),uuid4(),uuid4(),uuid4(),1,'{}')).fetchone()
        assert refused.value.sqlstate == '42501'
        assert refused.value.diag.message_primary == 'SEMANTIC_REVIEW_FORBIDDEN'
    # Malformed request shape is rejected before identity lookup, independently.
    assert database.execute('SELECT photo.semantic_shown_retouch_review_report_evidence(%s,%s,%s,%s,%s,%s::jsonb)',
        (uuid4(),uuid4(),uuid4(),uuid4(),0,'{}')).fetchone()[0] is None


@pytest.mark.parametrize('signature',[
    'photo.reserve_semantic_shown_retouch_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,text,bigint,text)',
    'photo.complete_semantic_shown_retouch_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text)',
])
def test_shown_retouch_transport_publication_is_api_only(database,signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_api')


@pytest.mark.parametrize('version',[None,'','null','x'*1025])
def test_shown_retouch_transport_rejects_invalid_object_version(database,version):
    args=[uuid4(),uuid4(),uuid4(),uuid4(),1,uuid4(),1,'a'*64,'b'*64,'{}','c'*64,uuid4(),'task_params',uuid4(),uuid4(),uuid4(),'c'*64,2,'photos',version]
    assert database.execute('SELECT photo.complete_semantic_shown_retouch_transport_document('+','.join(['%s']*20)+')',args).fetchone()[0] is False


@pytest.mark.parametrize('signature',[
    'photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
    'photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
])
def test_shown_retouch_live_lease_admission_is_worker_only(database,signature):
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_worker')


@pytest.mark.parametrize('attempt,fence',[(0,1),(1,0),(1,1)])
def test_shown_retouch_missing_live_task_cannot_admit_or_read_report(database,attempt,fence):
    database.execute('SET LOCAL ROLE photo_worker')
    args=(uuid4(),uuid4(),uuid4(),attempt,fence,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','a'*64)
    assert database.execute('SELECT photo.admit_semantic_shown_retouch('+','.join(['%s']*10)+')',args).fetchone()[0] is False
    assert database.execute('SELECT photo.v27_shown_retouch_report_evidence('+','.join(['%s']*10)+')',args).fetchone()[0] is None


@pytest.mark.parametrize('signature', [
    'photo.v27_shown_retouch_adoption_params(uuid,uuid,integer,bigint,uuid)',
    'photo.v27_shown_retouch_adoption_context(uuid,uuid)',
    'photo.v27_pending_shown_retouch_adoptions()',
    'photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text)',
    'photo.v27_shown_retouch_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text)',
])
def test_shown_retouch_completed_result_authority_is_scheduler_only(database, signature):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')


@pytest.mark.parametrize('attempt,fence', [(1,1),(0,1),(1,0)])
def test_shown_retouch_completed_result_missing_scope_cannot_adopt(database,attempt,fence):
    database.execute('SET LOCAL ROLE photo_scheduler')
    tenant,task,result,params=uuid4(),uuid4(),uuid4(),uuid4()
    scope=(tenant,task,attempt,fence,result)
    assert database.execute('SELECT * FROM photo.v27_shown_retouch_adoption_params(%s,%s,%s,%s,%s)',scope).fetchone() is None
    assert database.execute('SELECT * FROM photo.v27_shown_retouch_adoption_context(%s,%s)',(tenant,task)).fetchone() is None
    body='{}'
    args=scope+(params,sha256(body.encode()).hexdigest(),body)
    assert database.execute('SELECT photo.v27_shown_retouch_adoption_ready(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is False
    assert database.execute('SELECT photo.v27_shown_retouch_adoption_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0] is None


@pytest.mark.parametrize('signature,public_scheduler', [
    ('photo.adopt_shown_retouch_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text)',False),
    ('photo.shown_retouch_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text)',False),
    ('photo.adopt_v2_plan_before_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text)',False),
    ('photo.adopt_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text)',True),
])
def test_shown_retouch_locked_commit_keeps_helpers_private(database,signature,public_scheduler):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (public_scheduler and role=='photo_scheduler')


def test_shown_retouch_locked_commit_missing_task_does_not_publish(database):
    database.execute('SET LOCAL ROLE photo_scheduler')
    ids=[uuid4() for _ in range(5)]
    args=(ids[0],ids[1],1,1,ids[2],Jsonb([]),'a'*64,ids[3],'b'*64,'{}',ids[4],'c'*64,'{}','{}')
    assert database.execute('SELECT photo.adopt_shown_retouch('+','.join(['%s']*14)+')',args).fetchone()[0] is False


@pytest.mark.parametrize('signature,allowed', [
    ('photo.finalize_shown_semantic_apply_child(uuid,uuid)','photo_scheduler'),
    ('photo.finalize_next_shown_semantic_apply_child()','photo_scheduler'),
    ('photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid)','photo_api'),
    ('photo.register_media_retry_node(uuid,uuid,uuid,jsonb,uuid)','photo_scheduler'),
    ('photo.semantic_shown_media_source_live(uuid,jsonb)',None),
])
def test_shown_retouch_lifecycle_authority_allowlist(database,signature,allowed):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


def test_shown_retouch_applied_projection_missing_scope_returns_no_image(database):
    database.execute('SET LOCAL ROLE photo_api')
    assert database.execute('SELECT * FROM photo.semantic_shown_applied_image(%s,%s,%s,%s)',tuple(uuid4() for _ in range(4))).fetchone() is None


def test_shown_retouch_finalization_missing_scope_is_stale(database):
    database.execute('SET LOCAL ROLE photo_scheduler')
    assert database.execute('SELECT photo.finalize_shown_semantic_apply_child(%s,%s)',(uuid4(),uuid4())).fetchone()[0]=='stale'


@pytest.mark.parametrize('signature,allowed', [
    ('photo.claim_task_before_shown_retouch(uuid,integer,uuid,jsonb)',None),
    ('photo.claim_task(uuid,integer,uuid,jsonb)','photo_worker'),
])
def test_shown_retouch_claim_entrypoint_preserves_private_predecessor(database,signature,allowed):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


@pytest.mark.parametrize('signature,allowed', [
    ('photo.semantic_shown_retouch_task_live(uuid)',None),
    ('photo.gateway_input_before_shown_retouch(uuid,integer,bigint,uuid,text,uuid)',None),
    ('photo.heartbeat_task_before_shown_retouch(uuid,integer,bigint,uuid,jsonb)',None),
    ('photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)','photo_gateway'),
    ('photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)','photo_worker'),
])
def test_shown_retouch_live_io_allowlist(database,signature,allowed):
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role==allowed)


def test_shown_retouch_missing_task_refuses_live_heartbeat(database):
    database.execute('SET LOCAL ROLE photo_worker')
    row=database.execute('SELECT * FROM photo.heartbeat_task(%s,%s,%s,%s,%s)',(uuid4(),1,1,uuid4(),Jsonb({}))).fetchone()
    assert row==(False,None,True)


def test_shown_retouch_missing_task_has_no_input_grant(database):
    database.execute('SET LOCAL ROLE photo_gateway')
    assert database.execute('SELECT * FROM photo.gateway_input(%s,%s,%s,%s,%s,%s)',(uuid4(),1,1,uuid4(),'test-missing-certificate',uuid4())).fetchone() is None


def test_installed_shown_retouch_claim_uses_compiler_uuid_namespace(database):
    """Installed function and UUID algorithm agree; not a positive lease claim."""
    import re
    from uuid import uuid5
    public_source=database.execute("SELECT pg_get_functiondef('photo.claim_task(uuid,integer,uuid,jsonb)'::regprocedure)").fetchone()[0]
    assert 'FROM photo.claim_task_before_v2_activation_guard(' in public_source
    assert 'V2_OPERATION_CLAIM_ACTIVATION_STALE' in public_source
    assert "FOR SHARE" in public_source
    # 02438 wraps the retained 02406 namespace/worker/input/quota implementation.
    delegated='photo.claim_task_before_v2_activation_guard(uuid,integer,uuid,jsonb)'
    source=database.execute('SELECT pg_get_functiondef(%s::regprocedure)',(delegated,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,delegated)).fetchone()[0] is False
    namespaces=re.findall(r"photo\.semantic_uuid_v5\(r\.id,'([^']+)'\|\|target\.id::text\)",source)
    assert namespaces==['semantic.adopt_shown_retouch_region:']
    assert 'FROM photo.claim_task_before_shown_retouch(' in source
    assert "SEMANTIC_SHOWN_RETOUCH_CLAIM_INPUTS_STALE" in source
    assert "SEMANTIC_SHOWN_RETOUCH_CLAIM_QUOTA_STALE" in source
    assert "w.v2_renderer_versions ? 'v2.2'" in source
    assert "photo.capacity_policy_worker_fresh(w) FOR SHARE" in source
    assert "SEMANTIC_SHOWN_RETOUCH_CLAIM_WORKER_STALE" in source
    for _ in range(3):
        child,target=uuid4(),uuid4()
        actual=database.execute('SELECT photo.semantic_uuid_v5(%s,%s)',
            (child,namespaces[0]+str(target))).fetchone()[0]
        assert actual==uuid5(child,'semantic.adopt_shown_retouch_region:'+str(target))
        assert actual!=uuid5(child,'semantic.adopt_shown_retouch:'+str(target))
