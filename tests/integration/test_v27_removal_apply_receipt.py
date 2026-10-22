"""Own receipt/selection SQL scenarios; synthetic objects and canonical success."""
from semantic_target_state_fixture import reject_target_with_revision
import pytest
import psycopg
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from removal_preview_receipt_fixture import receipt_fixture, seal, lock_selection


def test_removal_receipt_replay_and_selection_preserve_proposed_target(database):
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert seal(database, data) == receipt
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    assert database.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s',
        (data[1],)).fetchone()[0] == receipt
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data[0][4],)).fetchone() == ('proposed', None, None)


@pytest.mark.parametrize('document_index', [4, 5, 6])
def test_removal_receipt_refuses_changed_document_bytes(database, document_index):
    data = list(receipt_fixture(database)); data[document_index] += ' '
    with pytest.raises((psycopg.errors.InvalidParameterValue, psycopg.errors.SerializationFailure)):
        seal(database, data)
    assert database.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s',
        (data[1],)).fetchone()[0] is None


@pytest.mark.parametrize('mutation', ['session', 'fence', 'revision'])
def test_removal_selection_refuses_lost_authority_without_approval(database, mutation):
    data = receipt_fixture(database); receipt = seal(database, data)
    if mutation == 'session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (data[0][3],))
    elif mutation == 'fence':
        database.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s', (data[7],))
    else:
        rejected_batch = reject_target_with_revision(database, data[0][4])[2]
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        lock_selection(database, data, receipt)
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data[0][4],)).fetchone() == (('rejected', None, rejected_batch) if mutation == 'revision' else ('proposed', None, None))


@pytest.mark.parametrize('role', ['presentation', 'working_image', 'pixels', 'numerical_evidence'])
@pytest.mark.parametrize('loss', ['unverified', 'expired', 'unversioned'])
def test_removal_selection_refuses_each_lost_receipt_object(database, role, loss):
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    artifact = receipt[role]['artifact_id']
    if loss == 'unverified':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (artifact,))
    elif loss == 'expired':
        database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (artifact,))
    else:
        before = _selection_state(database, data)
        artifact_before=database.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE id=%s',(artifact,)).fetchone()[0]
        with pytest.raises(psycopg.errors.CheckViolation) as exact_failure:
            with database.transaction():
                database.execute("UPDATE photo.artifacts SET object_version='' WHERE id=%s", (artifact,))
        assert exact_failure.value.diag.message_primary=='immutable field artifacts.object_version'
        assert database.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE id=%s',(artifact,)).fetchone()[0]==artifact_before
        assert _selection_state(database,data)==before
        assert lock_selection(database,data,receipt)==[(data[1],receipt)]
        return
    before = _selection_state(database, data)
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        lock_selection(database, data, receipt)
    assert _selection_state(database, data) == before


@pytest.mark.parametrize('loss', ['run_cancel', 'task_cancel', 'attempt', 'canonical_result'])
def test_removal_selection_refuses_changed_producer_after_receipt(database, loss):
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    if loss == 'run_cancel':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (data[1],))
    elif loss == 'task_cancel':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (data[7],))
    elif loss == 'attempt':
        database.execute('UPDATE photo.tasks SET attempt=attempt+1 WHERE id=%s', (data[7],))
    else:
        before = _selection_state(database, data)
        task_before=database.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(data[7],)).fetchone()
        with pytest.raises(psycopg.errors.CheckViolation) as failure:
            with database.transaction():
                database.execute('UPDATE photo.tasks SET canonical_result_id=NULL WHERE id=%s', (data[7],))
        assert failure.value.diag.constraint_name=='tasks_domain_4'
        assert database.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(data[7],)).fetchone()==task_before
        assert _selection_state(database,data)==before
        assert lock_selection(database,data,receipt)==[(data[1],receipt)]
        return
    before = _selection_state(database, data)
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        lock_selection(database, data, receipt)
    assert _selection_state(database, data) == before


@pytest.mark.parametrize('role', ['working_image', 'presentation'])
@pytest.mark.parametrize('field', ['artifact_id', 'sha256'])
def test_removal_selection_refuses_substituted_shown_reference(database, role, field):
    import copy
    from uuid import uuid4
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    substituted = copy.deepcopy(receipt)
    substituted[role][field] = str(uuid4()) if field == 'artifact_id' else ('0' if receipt[role][field][0] != '0' else '1') + receipt[role][field][1:]
    before = _selection_state(database, data)
    with pytest.raises(psycopg.errors.SerializationFailure):
        lock_selection(database, data, substituted)
    assert _selection_state(database, data) == before


def _selection_state(db, data):
    # The lock helper grants no approval/admission and must preserve receipt.
    return (
        db.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (data[0][4],)).fetchone(),
        db.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s', (data[1],)).fetchone(),
        db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s', (data[0][0],)).fetchone(),
        db.execute('SELECT count(*) FROM photo.tasks WHERE tenant_id=%s', (data[0][0],)).fetchone(),
    )


@pytest.mark.parametrize('field', ['tenant', 'intent', 'actor', 'session', 'target', 'revision', 'target_revision', 'plan_sha', 'candidate_sha'])
def test_removal_selection_refuses_foreign_identity_and_request_fences(database, field):
    import json
    from uuid import uuid4
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    params = json.loads(data[0][5])
    if field in ('revision', 'target_revision'):
        value = int(params['workflow_revision' if field == 'revision' else field]) + 1
    elif field in ('plan_sha', 'candidate_sha'):
        original = params['plan' if field == 'plan_sha' else 'candidate']['sha256']
        value = ('0' if original[0] != '0' else '1') + original[1:]
    else:
        value = uuid4()
    before = _selection_state(database, data)
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        lock_selection(database, data, receipt, overrides={field: value})
    assert _selection_state(database, data) == before


@pytest.mark.parametrize('name,signature,allowed', [
    ('seal', 'photo.seal_semantic_removal_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text)', 'photo_api'),
    ('selection', 'photo.lock_semantic_removal_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text)', 'photo_semantic_admission'),
])
def test_removal_receipt_selection_private_definer_acl(database, name, signature, allowed):
    row = database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)', (signature,)).fetchone()
    assert row is not None and row[0] == 'photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    assert database.execute("SELECT NOT EXISTS(SELECT 1 FROM pg_proc p, LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=to_regprocedure(%s) AND a.grantee=0 AND a.privilege_type='EXECUTE')", (signature,)).fetchone()[0] is True
    for role in ('photo_api', 'photo_semantic_admission', 'photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is (role == allowed)


def test_removal_selection_rejects_ambiguous_saved_receipts(database):
    from uuid import uuid4
    data = receipt_fixture(database)
    receipt = seal(database, data)
    assert lock_selection(database, data, receipt) == [(data[1], receipt)]
    # Deliberate inconsistent duplicate metadata tests ambiguity handling before
    # producer selection. It does not manufacture a second physical preview.
    duplicate = uuid4()
    database.execute('''INSERT INTO photo.runs
        (id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,
         created_by,graph_sealed,v27_removal_preview_receipt)
        SELECT %s,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,
         created_by,graph_sealed,v27_removal_preview_receipt FROM photo.runs WHERE id=%s''',
        (duplicate, data[1]))
    before = _selection_state(database, data)
    with pytest.raises(psycopg.errors.SerializationFailure) as failure:
        lock_selection(database, data, receipt)
    assert failure.value.diag.message_primary == 'SEMANTIC_PREVIEW_SELECTION_UNAVAILABLE'
    assert _selection_state(database, data) == before


@pytest.mark.parametrize('role',['preview_presentation','working_image','pixels','preview_evidence'])
@pytest.mark.parametrize('version',['','null'])
def test_removal_receipt_refuses_initial_missing_version_without_saved_receipt(database,role,version):
    from removal_transaction_state import tenant_transaction_state
    data=receipt_fixture(database,_initial_versions={role:version})
    before=tenant_transaction_state(database,data[0][0])
    expected = psycopg.errors.SerializationFailure if role=='preview_presentation' else psycopg.errors.InsufficientPrivilege
    message = 'SEMANTIC_PREVIEW_RECEIPT_STALE' if role=='preview_presentation' else 'SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE'
    with pytest.raises(expected) as exact_failure:
        seal(database,data)
    assert exact_failure.value.diag.message_primary==message
    assert tenant_transaction_state(database,data[0][0])==before
    assert database.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s',(data[1],)).fetchone()[0] is None
