"""Canonical bundle metadata cases; not lease/ICC/object-store qualification.

Uses only an explicitly supplied integration database and rolls back all rows.
The metadata predicate is the same private predicate invoked by commit 02300.
"""
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_semantic_final_producer_gate import fixture


ROLES = (
    ('working_image', 'application/json'),
    ('pixels', 'application/x-npy'),
    ('preview_evidence', 'application/json'),
    ('before', 'image/png'),
    ('after', 'image/png'),
    ('affected', 'image/png'),
    ('protected', 'image/png'),
)


def bundle(db, invalid_sidecar=None, presentation_builder=None, root_metadata_version=None, initial_run_config=None):
    refs = {}
    def build_root(authority, artifact, task):
        # Generic producer supplies the tenant/task before inserting its immutable
        # canonical root. Sidecars and exact presentation bytes must exist first.
        tenant = db.execute('SELECT tenant_id FROM photo.tasks WHERE id=%s', (task,)).fetchone()[0]
        for role, media in ROLES:
            aid = uuid4()
            fields = dict(bytes=2, media_type=media, object_version='v1')
            if invalid_sidecar is not None and invalid_sidecar[0] == role:
                fields.update(invalid_sidecar[1])
            db.execute("""INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,
                media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence)
                VALUES(%s,%s,'fixture',%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)""",
                (aid,tenant,str(aid),'a'*64,fields['bytes'],fields['media_type'],fields['object_version'],role,task))
            refs[role] = aid
        return presentation_builder(refs) if presentation_builder is not None else b'{}'
    tenant, task, result, *_ = fixture(db, assembly_output_builder=build_root, metadata_bodies=True, assembly_metadata_version=root_metadata_version, initial_run_config=initial_run_config)
    db.execute("UPDATE photo.tasks SET operation='semantic.preview_remove_object',resource_class='gpu' WHERE id=%s", (task,))
    root = db.execute('SELECT outputs FROM photo.task_results WHERE id=%s', (result,)).fetchone()[0][0]
    db.execute("UPDATE photo.artifacts SET output_role='preview_presentation' WHERE id=%s", (root['artifact_id'],))
    refs['preview_presentation'] = root['artifact_id']
    return tenant, task, result, refs


def ready(db, task, result, attempt=1, fence=1):
    db.execute('SET LOCAL ROLE photo_fn_owner')
    try:
        return db.execute('SELECT photo.semantic_removal_preview_bundle_ready(%s,%s,%s,%s)',
                          (task,result,attempt,fence)).fetchone()[0]
    finally:
        db.execute('RESET ROLE')


def test_complete_bundle_and_exact_canonical_identity(database):
    _, task, result, _ = bundle(database)
    assert ready(database, task, result) is True
    assert ready(database, uuid4(), result) is False
    assert ready(database, task, uuid4()) is False
    assert ready(database, task, result, attempt=2) is False
    assert ready(database, task, result, fence=2) is False
    # Repeated metadata inspection has no side effects.
    assert ready(database, task, result) is True


@pytest.mark.parametrize('role', [role for role, _ in ROLES])
@pytest.mark.parametrize('mutation', ['unverified','expired','wrong_attempt','wrong_fence','wrong_media','unversioned','oversized','wrong_producer'])
def test_each_sidecar_requires_live_exact_attempt_metadata(database, role, mutation):
    initial = {
        'wrong_media': dict(media_type='application/octet-stream'),
        'unversioned': dict(object_version=None),
        'oversized': dict(bytes=3000000000),
    }
    # Immutable fields are invalid at INSERT, never changed by disabling guards.
    _, task, result, refs = bundle(database, (role, initial[mutation]) if mutation in initial else None)
    assignments = {
        'unverified': 'verified_at=NULL',
        'expired': "expires_at=clock_timestamp()-interval '1 second'",
        'wrong_attempt': 'produced_attempt=2',
        'wrong_fence': 'produced_fence=2',
        'wrong_media': "media_type='application/octet-stream'",
        'unversioned': 'object_version=NULL',
        'oversized': 'bytes=3000000000',
        'wrong_producer': 'created_by_task_id=NULL',
    }
    if mutation not in initial:
        database.execute('UPDATE photo.artifacts SET '+assignments[mutation]+' WHERE id=%s', (refs[role],))
    assert ready(database, task, result) is False


@pytest.mark.parametrize('outputs', ['empty','extra','wrong_root_sha','sidecar_root'])
def test_canonical_output_is_one_exact_presentation(database, outputs):
    _, task, result, refs = bundle(database)
    root = dict(artifact_id=str(refs['preview_presentation']),sha256=database.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s', (refs['preview_presentation'],)).fetchone()[0])
    sidecar = dict(artifact_id=str(refs['working_image']),sha256='a'*64)
    choices = {'empty': [], 'extra': [root,sidecar],
               'wrong_root_sha': [dict(root,sha256='0'*64)], 'sidecar_root': [sidecar]}
    from psycopg.errors import CheckViolation
    before = database.execute('SELECT to_jsonb(r) FROM photo.task_results r WHERE id=%s', (result,)).fetchone()
    with pytest.raises(CheckViolation, match=r'^immutable field task_results\.outputs') as rejected:
        with database.transaction():
            database.execute('UPDATE photo.task_results SET outputs=%s WHERE id=%s', (Jsonb(choices[outputs]),result))
    assert rejected.value.sqlstate == '23514'
    assert rejected.value.diag.message_primary == 'immutable field task_results.outputs'
    assert database.execute('SELECT to_jsonb(r) FROM photo.task_results r WHERE id=%s', (result,)).fetchone() == before
    # Initial publication also refuses these envelopes before any canonical binding.
    expected = 'V2_ROOT_REQUIRED' if outputs in ('empty', 'extra') else 'V2_ROOT_UNVERIFIED'
    with pytest.raises(CheckViolation, match='^'+expected) as publication:
        with database.transaction():
            database.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) SELECT tenant_id,task_id,2,2,%s,'{}',result_digest FROM photo.task_results WHERE id=%s", (Jsonb(choices[outputs]),result))
    assert publication.value.sqlstate == '23514'
    assert publication.value.diag.message_primary == expected
    assert database.execute('SELECT to_jsonb(r) FROM photo.task_results r WHERE id=%s', (result,)).fetchone() == before
    assert ready(database, task, result) is True
    assert ready(database, task, result, attempt=2, fence=2) is False


def test_bundle_predicate_has_no_application_grants(database):
    signature = 'photo.semantic_removal_preview_bundle_ready(uuid,uuid,integer,bigint)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role,signature)).fetchone()[0] is False


def replace_body(db, signature, body):
    """Transactional test double; rollback restores the installed definition."""
    definition, source = db.execute(
        'SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',
        (signature,)).fetchone()
    assert source in definition
    db.execute(definition.replace(source, body, 1))


def isolated_commit(db):
    """Isolate 02300 transaction behavior; deliberately NOT a lease/auth fixture."""
    # Freeze the wrapper metadata before graph sealing; never rewrite sealed config.
    config = dict(semantic_removal_preview=dict(actor_id=str(uuid4()),session_id=str(uuid4()),
                  params=dict(intent_id=str(uuid4()),target_id=str(uuid4()),workflow_revision=1)))
    tenant, task, result, refs = bundle(db, initial_run_config=config)
    db.execute("UPDATE photo.tasks SET state='running',lease_until=clock_timestamp()+interval '1 hour' WHERE id=%s", (task,))
    replace_body(db, 'photo.semantic_removal_preview_task_live(uuid)', 'SELECT true;')
    replace_body(db, 'photo.semantic_removal_preview_run_current(uuid,uuid)', 'BEGIN RETURN true; END;')
    replace_body(db, 'photo.lock_semantic_decision_publication(uuid,uuid,uuid,uuid,bigint)', 'BEGIN RETURN NULL; END;')
    replace_body(db, 'photo.commit_task_success_before_removal_bundle(uuid,integer,bigint,uuid,text)', '''
DECLARE held photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO held FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF held.state='succeeded' THEN
  RETURN QUERY SELECT 'replayed'::text,held.canonical_result_id,'c'::text; RETURN;
 END IF;
 UPDATE photo.tasks SET state='succeeded',lease_until=NULL WHERE id=p_task;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(held.tenant_id,gen_random_uuid(),'photo.events.v1','test.removal.bundle','{}');
 RETURN QUERY SELECT 'committed'::text,held.canonical_result_id,'c'::text;
END;
''')
    return tenant, task, result, refs


def commit(db, task):
    db.execute('SET LOCAL ROLE photo_worker')
    try:
        return db.execute('SELECT * FROM photo.commit_task_success(%s,1,1,%s,%s)',
                          (task,uuid4(),'{}')).fetchone()
    finally:
        # An expected exception is contained in a caller-owned savepoint;
        # RESET must happen after that savepoint rolls back, not in an aborted txn.
        if db.info.transaction_status.name != 'INERROR':
            db.execute('RESET ROLE')


def test_commit_wrapper_rejects_incomplete_bundle_and_rolls_back_predecessor(database):
    from psycopg.errors import CheckViolation
    tenant, task, _, refs = isolated_commit(database)
    database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (refs['after'],))
    before = database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    with pytest.raises(CheckViolation, match='SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE'):
        with database.transaction():
            commit(database, task)
    assert database.execute('SELECT state FROM photo.tasks WHERE id=%s', (task,)).fetchone()[0] == 'running'
    assert database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0] == before


def test_commit_wrapper_complete_bundle_and_replay_do_not_duplicate_effect(database):
    tenant, task, result, _ = isolated_commit(database)
    first = commit(database, task)
    assert first[:2] == ('committed',result)
    before = database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    replay = commit(database, task)
    assert replay[:2] == ('replayed',result)
    assert database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0] == before

