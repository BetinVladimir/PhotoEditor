"""Durable handoff refusal and lease retention; not a model/positive-chain proof."""
import json
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_v27_native_anchor import seed_native_anchor


def test_automatic_handoff_requires_parent_and_preserves_lease(db):
    packet, case, _ = seed_native_anchor(db, 'face.detect', 'detector')
    tenant, actor = case['Tenant'], case['Actor']
    detector = UUID(packet['run_id'])
    config = json.loads(packet['config_text'])
    config['v27_detector_prepare']['recovery'] = dict(request_key='handoff-refusal')
    db.execute("""INSERT INTO photo.runs
      (id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,
       status,graph_sealed,failure_policy)
      VALUES(%s,%s,'v2',%s,%s,%s,%s,'queued',true,'fail_fast')""",
      (detector, tenant, case['Snapshot'], Jsonb(config), 'c'*64, actor))
    # Owner-only metadata setup; obtain the actual lease through scheduler claim.
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute('UPDATE photo.runs SET v27_initial_recovery_enabled=true WHERE id=%s', (detector,))
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    claimed = db.execute('SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)', (case['Project'],)).fetchone()
    db.execute('RESET ROLE')
    assert claimed and claimed[0] == detector
    token = claimed[1]
    before = db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s', (detector,)).fetchone()[0]

    def record(outcome, lease=token, scope=tenant):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            result = db.execute('SELECT photo.record_v27_native_initial_pipeline_handoff(%s,%s,%s,%s)',
                                (scope, detector, lease, outcome)).fetchone()[0]
            db.execute('RESET ROLE')
            return result

    for scope, lease, outcome in ((tenant, token, 'completed'), (str(uuid4()), token, 'completed'),
                                  (tenant, uuid4(), 'completed'), (tenant, token, 'unknown')):
        assert not record(outcome, lease, scope)
        assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s', (detector,)).fetchone()[0] == before
    # Even a sealed parent and a matching receipt cannot substitute for the
    # original person/defect producers. Metadata-only fixture, no model proof.
    parent = UUID(packet['parent_run_id'])
    parent_config = dict(project_id=case['Project'], input_snapshot_id=case['Snapshot'],
                         v27_semantic=dict(auth_session_id=case['Session'], nodes=[dict(task_id=str(uuid4()))]))
    db.execute("""INSERT INTO photo.runs
      (id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
      VALUES(%s,%s,'v2',%s,%s,%s,%s,'queued',true,'fail_fast')""",
      (parent,tenant,case['Snapshot'],Jsonb(parent_config),'d'*64,actor))
    assert not record('completed')
    workflow = uuid5(parent, 'workflow')
    db.execute("""INSERT INTO photo.v2_workflows
      (id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
      VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)""",
      (workflow,tenant,case['Project'],parent,case['Snapshot'],'b'*64))
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute("""INSERT INTO photo.idempotency_records
      (tenant_id,principal_type,principal_id,route,key,request_hash,resource_id,response_status,response_body,expires_at)
      VALUES(%s,'user',%s,'V2.Admit','synthetic-handoff',%s,%s,202,%s,clock_timestamp()+interval '1 hour')""",
      (tenant,actor,packet['request_sha'],parent,Jsonb(dict(run_id=str(parent),workflow_id=str(workflow),state='queued'))))
    db.execute('RESET ROLE')
    assert not record('completed')
    assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s', (detector,)).fetchone()[0] == before
    for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher',
                 'photo_portal','photo_auth','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_native_automatic_handoff_ready(uuid,uuid)','EXECUTE')", (role,)).fetchone()[0]
    with pytest.raises(errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            db.execute('SELECT photo.v27_native_automatic_handoff_ready(%s,%s)', (tenant, detector))
    assert record('waiting')
    assert db.execute('SELECT v27_initial_recovery_done,v27_initial_recovery_token,v27_initial_recovery_until,v27_initial_recovery_after IS NOT NULL FROM photo.runs WHERE id=%s',
                      (detector,)).fetchone() == (False, None, None, True)
    assert not record('completed')  # consumed lease cannot commit terminal state


def seed_claimable_detector(database):
    import psycopg
    with psycopg.connect(**database) as conn:
        packet, case, _ = seed_native_anchor(conn, 'face.detect', 'detector')
        detector = UUID(packet['run_id'])
        config = json.loads(packet['config_text'])
        config['v27_detector_prepare']['recovery'] = dict(request_key='claim-contention')
        conn.execute("""INSERT INTO photo.runs
          (id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
          VALUES(%s,%s,'v2',%s,%s,%s,%s,'queued',true,'fail_fast')""",
          (detector,case['Tenant'],case['Snapshot'],Jsonb(config),'c'*64,case['Actor']))
        conn.execute('SET LOCAL ROLE photo_fn_owner')
        conn.execute('UPDATE photo.runs SET v27_initial_recovery_enabled=true WHERE id=%s',(detector,))
        conn.execute('RESET ROLE')
    return case, detector


def test_initial_claim_skip_locked_and_replacement_fence(database):
    import psycopg
    case, detector = seed_claimable_detector(database)
    query = 'SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)'
    with psycopg.connect(**database) as first, psycopg.connect(**database,autocommit=True) as second:
        second.execute('SET ROLE photo_scheduler')
        with first.transaction():
            first.execute('SET LOCAL ROLE photo_scheduler')
            claimed = first.execute(query,(case['Project'],)).fetchone()
            assert claimed and claimed[0]==detector
            # The first transaction still holds its row lock. Another scheduler
            # must skip it promptly rather than block or publish a second token.
            second.execute("SET statement_timeout='2s'")
            assert second.execute(query,(case['Project'],)).fetchall()==[]
        assert second.execute(query,(case['Project'],)).fetchall()==[]
        with first.transaction():
            first.execute('SET LOCAL ROLE photo_fn_owner')
            first.execute("UPDATE photo.runs SET v27_initial_recovery_until=clock_timestamp()-interval '1 second' WHERE id=%s",(detector,))
        replacement=second.execute(query,(case['Project'],)).fetchone()
        assert replacement and replacement[0]==detector and replacement[1]!=claimed[1]
        record='SELECT photo.record_v27_native_initial_pipeline_handoff(%s,%s,%s,%s)'
        assert second.execute(record,(case['Tenant'],detector,claimed[1],'waiting')).fetchone()==(False,)
        assert second.execute(record,(case['Tenant'],detector,replacement[1],'waiting')).fetchone()==(True,)
        assert second.execute(record,(case['Tenant'],detector,replacement[1],'waiting')).fetchone()==(False,)


def test_initial_claim_survives_abrupt_scheduler_process_exit(database):
    import os
    import subprocess
    import sys
    import psycopg
    case, detector = seed_claimable_detector(database)
    # Separate interpreter commits the actual SECURITY DEFINER claim, then exits
    # without Python cleanup. No model, transport or complete Recover is implied.
    script = '''
import json,os,psycopg
cfg=json.loads(os.environ['PHOTO_RECOVERY_TEST_DATABASE'])
conn=psycopg.connect(**cfg,autocommit=True)
conn.execute('SET ROLE photo_scheduler')
row=conn.execute('SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)',(os.environ['PHOTO_RECOVERY_TEST_PROJECT'],)).fetchone()
print(json.dumps([str(x) for x in row] if row else None),flush=True)
os._exit(23)
'''
    env=dict(os.environ,PHOTO_RECOVERY_TEST_DATABASE=json.dumps(dict(database)),
             PHOTO_RECOVERY_TEST_PROJECT=case['Project'],PYTHONDONTWRITEBYTECODE='1')
    def crashed_claim():
        child=subprocess.run([sys.executable,'-c',script],env=env,capture_output=True,text=True,timeout=10)
        assert child.returncode==23, 'scheduler fixture did not reach its deliberate abrupt exit'
        return json.loads(child.stdout)
    original=crashed_claim()
    assert original and original[0]==str(detector)
    assert crashed_claim() is None  # process loss cannot erase a committed lease
    with psycopg.connect(**database,autocommit=True) as owner:
        owner.execute('SET ROLE photo_fn_owner')
        owner.execute("UPDATE photo.runs SET v27_initial_recovery_until=clock_timestamp()-interval '1 second' WHERE id=%s",(detector,))
    replacement=crashed_claim()
    assert replacement and replacement[0]==str(detector) and replacement[1]!=original[1]
    with psycopg.connect(**database,autocommit=True) as scheduler:
        scheduler.execute('SET ROLE photo_scheduler')
        query='SELECT photo.record_v27_native_initial_pipeline_handoff(%s,%s,%s,%s)'
        assert scheduler.execute(query,(case['Tenant'],detector,original[1],'waiting')).fetchone()==(False,)
        assert scheduler.execute(query,(case['Tenant'],detector,replacement[1],'waiting')).fetchone()==(True,)


@pytest.mark.parametrize('scope', ('user', 'session', 'project'))
def test_initial_claim_requires_current_authorization(database, scope):
    import psycopg
    case, detector = seed_claimable_detector(database)
    statements = {
        'user': ("UPDATE photo.users SET status='disabled' WHERE id=%s",
                 "UPDATE photo.users SET status='active' WHERE id=%s", case['Actor']),
        'session': ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",
                    "UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s", case['Session']),
        'project': ("UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s",
                    "UPDATE photo.projects SET archived_at=NULL WHERE id=%s", case['Project']),
    }
    mutation, restore, identifier = statements[scope]
    with psycopg.connect(**database) as conn:
        conn.execute(mutation,(identifier,))
        conn.execute('SET LOCAL ROLE photo_scheduler')
        query='SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)'
        assert conn.execute(query,(case['Project'],)).fetchall()==[]
        assert conn.execute(query,(uuid4(),)).fetchall()==[]
        conn.execute('RESET ROLE')
        assert conn.execute('SELECT v27_initial_recovery_token,v27_initial_recovery_until,v27_initial_recovery_done FROM photo.runs WHERE id=%s',(detector,)).fetchone()==(None,None,False)
        conn.execute(restore,(identifier,))
        conn.execute('SET LOCAL ROLE photo_scheduler')
        claimed=conn.execute(query,(case['Project'],)).fetchone()
        assert claimed and claimed[0]==detector  # same candidate becomes eligible
