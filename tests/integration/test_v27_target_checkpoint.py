"""Actual PostgreSQL checkpoint/role/replay gates; metadata only, no ML proof."""
from copy import deepcopy
from hashlib import sha256
import json
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v27_native_anchor import seed_native_anchor

ROLES = ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance')


def seed_checkpoint(db, extra_target=True, *, terminal_anchor=False):
    packet, case, stream = seed_native_anchor(db, 'face.detect', 'detector', extra_target=extra_target)
    config = json.loads(packet['config_text'])
    if terminal_anchor:
        config['v27_detector_prepare']['task_id'] = str(uuid5(uuid5(UUID(packet['run_id']), 'workflow'), 'face.detect:0'))
        config['v27_detector_prepare']['recovery'] = dict(request_key='target-terminal-fixture')
        packet['config_text'] = canonical_bytes(config).decode()
    db.execute("""INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
      VALUES(%s,%s,'v2',%s,%s,%s,%s,'queued',true,'fail_fast')""",
      (packet['run_id'],case['Tenant'],case['Snapshot'],Jsonb(config),sha256(packet['config_text'].encode()).hexdigest(),case['Actor']))
    native = config['v27_detector_prepare']
    entries = []
    for i,item in enumerate(json.loads(packet['anchor']['snapshot_text'])['items']):
        binding = item['binding']
        namespace = uuid5(UUID(packet['parent_run_id']),f"semantic.target.prepare:1:{native['snapshot']['sha256']}:{i}:{sha256(canonical_bytes(binding)).hexdigest()}")
        stages=[]
        reuse=binding==native['source']
        if not reuse:
            ops=['mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose']
            ids={op:str(uuid5(namespace,'stage:'+op)) for op in ops}
            deps={'face.landmarks':['face.detect'],'face.parse':['face.detect'],'mask.face_skin':['face.parse'],'defects.propose':['mask.person','face.landmarks','mask.face_skin']}
            stages=[dict(id=ids[op],operation=op,operation_version='1',requires=[ids[d] for d in deps.get(op,[])]) for op in ops]
        entries.append(dict(ordinal=i,binding=binding,namespace=str(namespace),reuse_reference=reuse,stages=stages))
    manifest=dict(schema_version=1,tenant_id=case['Tenant'],project_id=case['Project'],actor_id=case['Actor'],session_id=case['Session'],parent_run_id=packet['parent_run_id'],request_sha=packet['request_sha'],intent=native['intent'],snapshot=native['snapshot'],entries=entries)
    return packet,case,stream,manifest


def save(db,packet,body,tenant=None):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant or packet['tenant_id'],))
        result=db.execute('SELECT photo.save_v27_target_manifest(%s,%s,%s,%s)',(tenant or packet['tenant_id'],packet['run_id'],Jsonb(packet),body)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('extra_target',[False,True])
def test_target_checkpoint_exact_replay_and_substitution(db,extra_target):
    packet,case,stream,manifest=seed_checkpoint(db,extra_target)
    body=canonical_bytes(manifest).decode()
    expected=dict(body=body,sha=sha256(body.encode()).hexdigest())
    assert save(db,packet,body)==expected
    assert save(db,packet,body)==expected
    assert db.execute('SELECT v27_target_manifest,v27_target_manifest_sha FROM photo.runs WHERE id=%s',(packet['run_id'],)).fetchone()==(body,expected['sha'])
    variants=[]
    for key in ['tenant_id','project_id','actor_id','session_id','parent_run_id','request_sha','snapshot','intent']:
        changed=deepcopy(manifest)
        changed[key]='f'*64 if key=='request_sha' else dict(artifact_id=str(uuid4()),sha256='f'*64) if key in ['snapshot','intent'] else str(uuid4())
        variants.append(canonical_bytes(changed).decode())
    changed=deepcopy(manifest);changed['entries'][0]['namespace']=str(uuid4());variants.append(canonical_bytes(changed).decode())
    if extra_target:
        changed=deepcopy(manifest);changed['entries'][1]['stages'][5]['requires'][0]=str(uuid4());variants.append(canonical_bytes(changed).decode())
        changed=deepcopy(manifest);changed['entries'][1]['reuse_reference']=True;variants.append(canonical_bytes(changed).decode())
    variants.extend([body+'\n',json.dumps(manifest),'{}','{'])
    for candidate in variants:
        assert save(db,packet,candidate) is None
    assert save(db,packet,body,tenant=str(uuid4())) is None
    changed=deepcopy(packet);changed['config_text']=json.dumps(json.loads(packet['config_text']))
    assert save(db,changed,body) is None
    # Even exact replay repeats current authorization and recipe-head checks.
    db.execute('UPDATE photo.v2_document_streams SET current_document_id=NULL WHERE id=%s',(stream,))
    assert save(db,packet,body) is None
    assert db.execute('SELECT v27_target_manifest FROM photo.runs WHERE id=%s',(packet['run_id'],)).fetchone()[0]==body


def test_target_checkpoint_privileges_and_immutable_owner(db):
    packet,case,_,manifest=seed_checkpoint(db)
    body=canonical_bytes(manifest).decode()
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,'photo.save_v27_target_manifest(uuid,uuid,jsonb,text)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')
        for fn in ['photo.guard_v27_target_manifest()','photo.v27_target_manifest_json_text(jsonb)']:
            assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,fn)).fetchone()[0]
    with pytest.raises(errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            db.execute('UPDATE photo.runs SET v27_target_manifest=%s,v27_target_manifest_sha=%s WHERE id=%s',(body,sha256(body.encode()).hexdigest(),packet['run_id']))
    assert save(db,packet,body)
    with pytest.raises(errors.InsufficientPrivilege,match='V27_TARGET_MANIFEST_IMMUTABLE'):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('UPDATE photo.runs SET v27_target_manifest=NULL,v27_target_manifest_sha=NULL WHERE id=%s',(packet['run_id'],))


def test_checkpoint_repeats_live_authority_without_changing_saved_plan(db):
    packet,case,stream,manifest=seed_checkpoint(db)
    body=canonical_bytes(manifest).decode()
    expected=save(db,packet,body)
    assert expected
    mutations=[
        ('UPDATE photo.users SET status=\'disabled\' WHERE id=%s',(case['Actor'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(case['Project'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(packet['run_id'],)),
        ('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(packet['binding']['source']['artifact_id'],)),
        ('UPDATE photo.v2_document_streams SET current_document_id=NULL WHERE id=%s',(stream,)),
    ]
    for query,args in mutations:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert save(db,packet,body) is None, query
            assert db.execute('SELECT v27_target_manifest,v27_target_manifest_sha FROM photo.runs WHERE id=%s',(packet['run_id'],)).fetchone()==(body,expected['sha'])
        assert save(db,packet,body)==expected


def test_checkpoint_survives_connection_exit_and_serializes_replays(database):
    import psycopg
    with psycopg.connect(**database) as conn:
        packet,case,_,manifest=seed_checkpoint(conn)
        body=canonical_bytes(manifest).decode()
        expected=save(conn,packet,body)
        assert expected
    # New process/session state cannot choose a fresh plan: committed original
    # bytes remain authoritative. This is DB connection restart, not worker kill.
    with psycopg.connect(**database) as first, psycopg.connect(**database) as second:
        first.execute('SELECT 1')  # explicit outer transaction retains replay locks
        assert save(first,packet,body)==expected
        # First replay holds tenant/inventory/detector locks until commit.
        with pytest.raises(errors.LockNotAvailable):
            with second.transaction():
                second.execute("SET LOCAL lock_timeout='200ms'")
                save(second,packet,body)
        first.commit()
        assert save(second,packet,body)==expected
        assert second.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s AND v27_target_manifest=%s',(case['Tenant'],packet['run_id'],body)).fetchone()[0]==1


def test_target_entry_lock_requires_checkpoint_and_exact_live_scope(db):
    packet,case,stream,manifest=seed_checkpoint(db)
    body=canonical_bytes(manifest).decode()
    digest=sha256(body.encode()).hexdigest()
    def lock(ordinal=1,operation='mask.person',sha=digest,evidence=packet,scope=None):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(scope or case['Tenant'],))
            result=db.execute('SELECT photo.lock_v27_target_preparation_entry(%s,%s,%s,%s,%s,%s)',(scope or case['Tenant'],packet['run_id'],Jsonb(evidence),sha,ordinal,operation)).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert lock() is None  # locally compiled plan is not a persisted checkpoint
    assert save(db,packet,body)
    for op in ['mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose']:
        assert lock(operation=op)==manifest['entries'][1]
    for ordinal in [-1,0,2,5000,None]:
        assert lock(ordinal=ordinal) is None  # reference reuse uses original producer
    for op in ['semantic.transfer','mask.person:1','',None]:
        assert lock(operation=op) is None
    assert lock(sha='f'*64) is None
    assert lock(scope=str(uuid4())) is None
    changed=deepcopy(packet);changed['session_id']=str(uuid4())
    assert lock(evidence=changed) is None
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,'photo.lock_v27_target_preparation_entry(uuid,uuid,jsonb,text,integer,text)','EXECUTE')",(role,)).fetchone()[0]==(role=='photo_scheduler')
    for query,args in [
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(packet['run_id'],)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(case['Project'],)),
        ('UPDATE photo.v2_document_streams SET current_document_id=NULL WHERE id=%s',(stream,)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert lock() is None
        assert lock()==manifest['entries'][1]
