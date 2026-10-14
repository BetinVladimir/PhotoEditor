"""Leased SQL staging; synthetic metadata, no upload/model qualification."""
from uuid import uuid4
from psycopg.types.json import Jsonb
import psycopg
import pytest
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_stage_owns_params_and_complete_producers(db):
    tid, task, result, producer, _, candidate, scope = fixture(db, snapshot_authority=True, qualification=True)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (task,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='blocked',canonical_result_id=NULL WHERE id=%s", (task,))
    for table in ('output_uploads', 'task_results', 'task_attempts', 'v2_task_inputs'):
        db.execute(f'DELETE FROM photo.{table} WHERE task_id=%s', (task,))
    db.execute('DELETE FROM photo.artifacts WHERE created_by_task_id=%s', (task,))
    db.execute('DELETE FROM photo.tasks WHERE id=%s', (task,))
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tid,))
    def call(name, *args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            with db.transaction():
                return db.execute(f"SELECT * FROM photo.{name}({','.join(['%s']*len(args))})", args).fetchall()
        finally:db.execute('RESET ROLE')
    expansion = call('open_v27_semantic_assembly', tid, run)[0][0]
    lease = call('claim_v27_semantic_assembly')[0][1]
    reservation = call('reserve_v27_semantic_assembly_document', tid, run, expansion, lease, 'task_params', 'a'*64, 512, 'fixture')[0]
    params = reservation[1]
    assert call('complete_v27_semantic_assembly_document', tid, run, expansion, lease, 'task_params', params, reservation[2], 'a'*64, 512, 'fixture', 'v1') == [(True,)]
    profile = dict(backend='cpu', renderer_version='v2.2', resource_estimate=dict(ram_bytes=268435456, vram_bytes=0, scratch_bytes=33554432, output_bytes=8388608, pixel_count=0))
    refs = [dict(artifact_id=str(candidate), sha256='9'*64)]
    def stage(token=lease, targets=refs, params_id=params, task_id=task):
        return call('stage_v27_semantic_assembly', tid, run, expansion, token, task_id, params_id, 'a'*64, 'b'*64, Jsonb(targets), Jsonb(profile))[0][0]
    assert not stage(token=uuid4())
    assert not stage(params_id=scope['root'])
    assert not stage(task_id=uuid4())
    assert not stage(targets=[])
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert not stage()
    assert stage()
    assert stage()
    assert db.execute('SELECT state,attempt,inputs FROM photo.tasks WHERE id=%s', (task,)).fetchone() == ('blocked', 0, [])
    assert not db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone()[0]
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s', (run,)).fetchone() == (True,)
    assert db.execute('SELECT prerequisite_id,dependent_id FROM photo.task_edges WHERE run_id=%s', (run,)).fetchall() == [(producer, task)]
    assert db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation='v2.semantic.assemble' AND version='1'").fetchone() == (False,)
    for role in ('photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.stage_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)','EXECUTE')", (role,)).fetchone()[0]
    # The legacy publisher cannot bypass the assembly fence even though this
    # task is blocked and its graph is sealed.
    assert call('reserve_v2_input_document', tid, run, task, 'v2_input_manifest', 'c'*64, 512, 'fixture')[0][0] == 'forbidden'
    assert call('reserve_v27_semantic_transport_document', tid, run, task, uuid4(), 'v2_input_manifest', 'c'*64, 512, 'fixture')[0][0] == 'forbidden'
    assert call('reserve_v27_semantic_transport_document', tid, run, producer, lease, 'v2_input_manifest', 'c'*64, 512, 'fixture')[0][0] == 'forbidden'
    transport = {}
    for purpose in ('v2_input_chunk_0', 'v2_input_manifest'):
        created = call('reserve_v27_semantic_transport_document', tid, run, task, lease, purpose, 'c'*64, 512, 'fixture')[0]
        assert created[0] == 'created'
        transport[purpose] = dict(artifact_id=str(created[1]), sha256='c'*64)
        args = (tid, run, task, lease, purpose, created[1], created[2], 'c'*64, 512, 'fixture', 'pinned-v1')
        with db.transaction(force_rollback=True):
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
            assert call('complete_v27_semantic_transport_document', *args) == [(False,)]
        assert call('complete_v27_semantic_transport_document', *args) == [(True,)]
        assert call('complete_v27_semantic_transport_document', *args) == [(True,)]
        assert call('reserve_v27_semantic_transport_document', tid, run, task, lease, purpose, 'c'*64, 512, 'fixture')[0][0] == 'replayed'
    def document(digest):
        aid = uuid4()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json',clock_timestamp())", (aid,tid,str(aid),digest))
        return dict(artifact_id=str(aid), sha256=digest)
    params_ref = dict(artifact_id=str(params), sha256='a'*64)
    intent, calibration = document('4'*64), scope['qualification']['calibration']
    snapshot_ref = dict(artifact_id=str(scope['root']), sha256='8'*64)
    entries = [dict(ref=ref, contract=contract) for ref,contract in [
        (params_ref,'Params'), (intent,'EditIntent'), (calibration,'Calibration'), (snapshot_ref,'InputSnapshot'),
        (refs[0],'TransferCandidate'), (dict(artifact_id=str(scope['source']),sha256='5'*64),'WorkingImage')]]
    entries.sort(key=lambda e:e['ref']['artifact_id'])
    def commit(token=lease, manifest=transport['v2_input_manifest']):
        return call('commit_v27_semantic_assembly', tid, run, expansion, token, task, Jsonb(entries), Jsonb(manifest), Jsonb([transport['v2_input_chunk_0']]), 'b'*64, Jsonb([params_ref,intent,calibration,snapshot_ref]))[0][0]
    assert not commit(token=uuid4())
    substitute=document('6'*64)
    substituted_entries=[dict(e,ref=substitute) if e['contract']=='Calibration' else e for e in entries]
    assert not call('commit_v27_semantic_assembly',tid,run,expansion,lease,task,Jsonb(substituted_entries),Jsonb(transport['v2_input_manifest']),Jsonb([transport['v2_input_chunk_0']]),'b'*64,Jsonb([params_ref,intent,substitute,snapshot_ref]))[0][0]
    pin=scope['qualification']
    for statement,args in [
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed'] WHERE id=%s",(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['calibration']['artifact_id'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,args)
            assert not commit()
            assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.commit_v27_semantic_assembly_pre_qualification(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb)','EXECUTE')").fetchone()[0]

    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
        assert not commit()
    with pytest.raises(psycopg.errors.CheckViolation):
        with db.transaction():
            commit(manifest=dict(artifact_id=str(uuid4()), sha256='c'*64))
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s', (run,)).fetchone() == (True,)
    for role in ('photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb)','EXECUTE')", (role,)).fetchone()[0]
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s', (task,)).fetchone() == (0,)
    assert commit()
    assert not commit()
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone() == (True,)
    # Migration 00534 retains this immutable compiler pin; the worker explicitly
    # supports it alongside the current schema. Never rewrite persisted task pins.
    from photo_worker.v2.contract_lock import EXPOSURE_PREVIOUS_SCHEMAS_SHA256
    assert db.execute("SELECT execution_profile->>'contracts_sha' FROM photo.tasks WHERE id=%s",(task,)).fetchone()==(EXPOSURE_PREVIOUS_SCHEMAS_SHA256,)
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
        with db.transaction():
            db.execute("UPDATE photo.tasks SET execution_profile=execution_profile-'contracts_sha' WHERE id=%s",(task,))
    assert db.execute("SELECT execution_profile->>'contracts_sha' FROM photo.tasks WHERE id=%s",(task,)).fetchone()==(EXPOSURE_PREVIOUS_SCHEMAS_SHA256,)
    assert db.execute('SELECT state,expansion_token FROM photo.graph_expansions WHERE id=%s', (expansion,)).fetchone() == ('closed',None)
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s', (run,)).fetchone() == (True,)
    assert call('reserve_v27_semantic_transport_document', tid, run, task, lease, 'v2_input_manifest', 'c'*64, 512, 'fixture')[0][0] == 'forbidden'
    for signature in ('reserve_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,text,bigint,text)', 'complete_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)'):
        for role in ('photo_api', 'photo_worker', 'public'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, 'photo.'+signature)).fetchone()[0]
