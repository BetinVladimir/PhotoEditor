"""Authenticated canonical presentation metadata after real preview activation.

No SQL function doubles. Synthetic output metadata is not physical verification,
worker execution, lease qualification or a claim that photographic quality passed.
"""
import hashlib
import json
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_activation import published_transport_fixture, commit_preview


def presentation_source_fixture(db, *, review_documents=False, _initial_root_builder=None, _initial_versions=None):
    versions = {} if _initial_versions is None else dict(_initial_versions)
    if set(versions) - {'preview_presentation','working_image','pixels','preview_evidence','before','after','affected','protected'} or any(v not in ('','null') for v in versions.values()):
        raise ValueError('closed metadata-only missing-version overrides')
    args,run,scope = published_transport_fixture(db,review_documents=review_documents)
    task = commit_preview(db,args,run,scope)
    db.execute('UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s',(task,))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(args[0],task,uuid4()))
    # The source API authorizes metadata; JSON schema is checked by the reader.
    raw = (b'{"schema_version":1}' if _initial_root_builder is None
           else _initial_root_builder(args,scope,task))
    assert isinstance(raw,bytes) and raw
    sha = hashlib.sha256(raw).hexdigest()
    root = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json',%s,clock_timestamp(),'preview_presentation',%s,1,1)",(root,args[0],str(root),sha,len(raw),versions.get('preview_presentation','metadata-only-v1'),task))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(args[0],task,Jsonb([dict(artifact_id=str(root),sha256=sha)]),sha)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",(run,))
    return args,run,scope,task,result,root,sha


def read_source(db,args,run):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        rows = db.execute('SELECT * FROM photo.semantic_background_cleanup_preview_presentation_source(%s,%s,%s,%s,%s,%s)',(args[0],run,args[2],args[3],args[5],args[6])).fetchall()
        db.execute('RESET ROLE')
        return rows


def test_background_cleanup_presentation_source_exact_canonical_metadata(database):
    args,run,scope,task,result,root,sha = presentation_source_fixture(database)
    rows = read_source(database,args,run)
    assert len(rows) == 1
    assert rows[0][:6] == (task,result,1,1,root,sha)
    assert rows[0][8] == 'metadata-only-v1'


@pytest.mark.parametrize('mutation',['cancel_run','cancel_task','fence','root_unverified','root_role','closure_unverified'])
def test_background_cleanup_presentation_source_rejects_stale_producer(database,mutation):
    args,run,scope,task,result,root,sha = presentation_source_fixture(database)
    if mutation == 'cancel_run':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    elif mutation == 'cancel_task':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
    elif mutation == 'fence':
        database.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,))
    elif mutation == 'root_role':
        database.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(root,))
    else:
        aid = scope['source'] if mutation == 'closure_unverified' else root
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(aid,))
    assert read_source(database,args,run) == []


@pytest.mark.parametrize('mutation',['foreign_actor','foreign_session','revoked_session'])
def test_background_cleanup_presentation_source_denies_foreign_editor(database,mutation):
    args,run,scope,*_ = presentation_source_fixture(database)
    if mutation == 'foreign_actor':
        args = (*args[:2],uuid4(),*args[3:])
    elif mutation == 'foreign_session':
        args = (*args[:3],uuid4(),*args[4:])
    else:
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        read_source(database,args,run)


def authorized_sidecars_fixture(db, *, review_documents=False, _initial_documents=None, _initial_versions=None):
    from test_v27_background_cleanup_preview_bundle import ROLES
    versions = {} if _initial_versions is None else dict(_initial_versions)
    captured = {}
    def initial_root(args,scope,task):
        presentation = dict(schema_version=1,params=json.loads(args[5]),width=16,height=16,
            resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1',icc_lock_sha='b'*64,srgb_sha='c'*64)
        refs = {role:dict(artifact_id=str(uuid4()),sha256=hashlib.sha256(role.encode()).hexdigest())
                for role,_ in ROLES}
        # Compute document dependencies before any immutable artifact/result INSERT.
        documents = {} if _initial_documents is None else _initial_documents(args,scope,refs)
        assert set(documents) <= {'working_image','preview_evidence'}
        for role in ('working_image','preview_evidence'):
            if role in documents:
                raw = documents[role].encode()
                refs[role]['sha256'] = hashlib.sha256(raw).hexdigest()
        for role,media in ROLES:
            ref = refs[role]
            size = len(documents[role].encode()) if role in documents else 128
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",
                (ref['artifact_id'],args[0],ref['artifact_id'],ref['sha256'],size,media,versions.get(role,'metadata-only-v1'),role,task))
            presentation['numerical_evidence' if role == 'preview_evidence' else role] = ref
        raw = json.dumps(presentation,sort_keys=True,separators=(',', ':'))
        captured.update(refs=refs,raw=raw)
        return raw.encode()
    args,run,scope,*_ = presentation_source_fixture(db,review_documents=review_documents,_initial_root_builder=initial_root,_initial_versions=versions)
    return args,run,scope,captured['refs'],captured['raw']


def read_authorized_sidecars(db,args,run,raw):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        rows = db.execute('SELECT * FROM photo.semantic_background_cleanup_preview_presentation_sidecars(%s,%s,%s,%s,%s,%s,%s)',(args[0],run,args[2],args[3],args[5],args[6],raw)).fetchall()
        db.execute('RESET ROLE')
        return rows


def test_background_cleanup_real_authority_returns_exact_seven_sidecars(database):
    args,run,scope,refs,raw = authorized_sidecars_fixture(database)
    rows = read_authorized_sidecars(database,args,run,raw)
    assert len(rows) == 7
    assert {role:dict(artifact_id=str(aid),sha256=sha) for role,aid,sha,*_ in rows} == refs
    assert all(row[5] == 'metadata-only-v1' for row in rows)


@pytest.mark.parametrize('role',['working_image','pixels','preview_evidence','before','after','affected','protected'])
def test_background_cleanup_real_authority_denies_sidecar_from_other_fence(database,role):
    args,run,scope,refs,raw = authorized_sidecars_fixture(database)
    database.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(refs[role]['artifact_id'],))
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        read_authorized_sidecars(database,args,run,raw)


def test_background_cleanup_real_sidecars_deny_session_revoked_between_reads(database):
    args,run,scope,refs,raw = authorized_sidecars_fixture(database)
    assert len(read_authorized_sidecars(database,args,run,raw)) == 7
    database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        read_authorized_sidecars(database,args,run,raw)


@pytest.mark.parametrize('role',['working_image','pixels','preview_evidence','before','after','affected','protected'])
@pytest.mark.parametrize('version',['','null'])
def test_background_cleanup_sidecars_deny_initial_missing_version_without_partial_bundle(database,role,version):
    from removal_transaction_state import tenant_transaction_state
    args,run,scope,refs,raw=authorized_sidecars_fixture(database,_initial_versions={role:version})
    assert database.execute('SELECT object_version FROM photo.artifacts WHERE id=%s',(refs[role]['artifact_id'],)).fetchone()==(version,)
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.errors.InsufficientPrivilege) as exact_failure:
        read_authorized_sidecars(database,args,run,raw)
    assert exact_failure.value.diag.message_primary=='SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE'
    assert tenant_transaction_state(database,args[0])==before
