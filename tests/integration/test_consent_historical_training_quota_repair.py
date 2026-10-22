"""Historical relational repair. Qualification/model/output receipts remain synthetic.

The old generic consumer, not a direct terminal-status UPDATE, produces the leak.
No worker inference or physical object-deletion claim is made by this fixture.
"""
from pathlib import Path
from uuid import uuid4
import pytest
from test_consent_authorized_training_withdrawal import _adopted_training
from test_database import tenant

ROOT=Path(__file__).resolve().parents[2]

def _migration(db, number):
    paths=list((ROOT/'backend/db/migrations').glob(f'{number:05d}_*.sql'))
    assert len(paths)==1
    db.execute(paths[0].read_text().split('-- +goose Down',1)[0])

def _historical(db):
    qualification={}
    main=_adopted_training(db,qualification=qualification)
    pending=_adopted_training(db,qualification=qualification,base=main['base'],stop_before_evaluate_result=True)
    tid=main['tenant']
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    changed=db.execute('SELECT photo.change_v2_consent('+','.join(['%s']*10)+')',
        (tid,main['project'],main['consent'],2,'learning_photographer',None,'revoke','v2.2',main['actor'],main['session'])).fetchone()[0]
    assert changed['status']=='revoked' and changed['epoch']=='3'
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.cancel_idle_task()').fetchone()==(1,)
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',(tid,main['run'])).fetchone()==('cancelled',)
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,cancel_requested,graph_sealed FROM photo.runs WHERE id=%s',(pending['run'],)).fetchone()==('cancelled',True,True)
    q=db.execute("SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'",(tid,pending['run'])).fetchone()
    assert q and q[1]>0
    return main,pending,q

def _preserved(db,tid,run):
    return (
        db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',(run,)).fetchall(),
        db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE tenant_id=%s AND run_id=%s ORDER BY id',(tid,run)).fetchall(),
        db.execute('SELECT to_jsonb(u) FROM photo.output_uploads u JOIN photo.tasks t ON t.id=u.task_id AND t.tenant_id=u.tenant_id WHERE t.tenant_id=%s AND t.run_id=%s ORDER BY u.id',(tid,run)).fetchall(),
        db.execute('SELECT to_jsonb(d) FROM photo.backend_documents d WHERE tenant_id=%s AND run_id=%s ORDER BY id',(tid,run)).fetchall(),
        db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE tenant_id=%s ORDER BY id',(tid,)).fetchall(),
        db.execute('SELECT to_jsonb(e) FROM photo.run_events e WHERE tenant_id=%s ORDER BY id',(tid,)).fetchall(),
    )

@pytest.mark.migration_head(2586)
@pytest.mark.parametrize('mode',['ready','expired','cleaned'])
def test_actual_historical_terminal_reservation_is_repaired_once(db,mode):
    main,pending,(qid,remaining)=_historical(db)
    tid,run=main['tenant'],pending['run']
    if mode=='expired':
        db.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(qid,))
    for n in (2587,2588,2589): _migration(db,n)
    if mode=='cleaned':
        # Synthetic never-uploaded pending descriptor. Invoke the real maintenance
        # completion writer; no claim of deleting a physical object is made.
        doc=db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,cleanup_after) VALUES(%s,%s,%s,'task_params',%s,%s,%s,2,'consent-test',clock_timestamp()-interval '1 day') RETURNING id",
            (tid,run,uuid4(),uuid4(),uuid4(),'a'*64)).fetchone()[0]
        db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+2 WHERE tenant_id=%s',(tid,))
        db.execute('SET LOCAL ROLE photo_maintenance')
        cleanup=db.execute('SELECT document_id,cleanup_token FROM photo.claim_document_cleanup()').fetchone()
        assert cleanup and cleanup[0]==doc
        assert db.execute('SELECT photo.finish_document_cleanup(%s,%s)',cleanup).fetchone()==(True,)
        db.execute('RESET ROLE')
        assert db.execute('SELECT state,cleanup_token,cleanup_lease_until FROM photo.backend_documents WHERE id=%s',(doc,)).fetchone()==('cleaned',None,None)
    neighbor,neighbor_actor=tenant(db,str(uuid4())+'@example.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id,reserved_bytes) VALUES(%s,77)',(neighbor,))
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,'user',%s,'run',%s,77,0,'reserved',clock_timestamp()+interval '1 hour')",(neighbor,neighbor_actor,uuid4()))
    neighbor_before=(db.execute('SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=%s',(neighbor,)).fetchall(),db.execute('SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE tenant_id=%s',(neighbor,)).fetchall())
    frozen=_preserved(db,tid,run)
    before=db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    assert db.execute('SELECT photo.v23_cancelled_training_repair_ready(%s,%s)',(tid,run)).fetchone()==(True,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(True,)
    assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',(qid,)).fetchone()==('released',0)
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(before[0]-remaining,before[1])
    assert _preserved(db,tid,run)==frozen
    assert (db.execute('SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=%s',(neighbor,)).fetchall(),db.execute('SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE tenant_id=%s',(neighbor,)).fetchall())==neighbor_before

@pytest.mark.migration_head(2586)
@pytest.mark.parametrize('invalid',['principal','files','marker','pending-document','missing-artifact','frozen-role'])
def test_historical_repair_refuses_unproven_ownership_or_unsettled_outputs(db,invalid):
    main,pending,(qid,remaining)=_historical(db)
    tid,run=main['tenant'],pending['run']
    for n in (2587,2588,2589): _migration(db,n)
    if invalid=='principal':
        other=db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(str(uuid4())+'@example.test',)).fetchone()[0]
        db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'editor')",(tid,other))
        # Existing authenticated principal in this tenant, but not the reservation owner.
        db.execute('UPDATE photo.quota_reservations SET principal_id=%s WHERE id=%s',(other,qid))
    elif invalid=='files': db.execute('UPDATE photo.quota_reservations SET files=1 WHERE id=%s',(qid,))
    elif invalid=='marker':
        original_run=run
        run,qid=_typed_terminal_copy(db,original_run,malformed=True)
        remaining=17
        # All other typed fields come from a valid historical cancellation.
        assert db.execute('SELECT photo.v23_cancelled_training_repair_ready(%s,%s)',(tid,original_run)).fetchone()==(True,)
    elif invalid=='pending-document':
        # Historical document incomplete; savepoint rollback leaves original receipt intact.
        changed=db.execute("UPDATE photo.backend_documents SET state='cleaning',object_version=NULL,completed_at=NULL,cleanup_token=%s,cleanup_lease_until=clock_timestamp()+interval '1 minute' WHERE tenant_id=%s AND run_id=%s",(uuid4(),tid,run)).rowcount
        assert changed>0
    elif invalid=='frozen-role':
        # Actual00386 writer uses v23_frozen_recipe for exactly these purposes.
        changed=db.execute("UPDATE photo.artifacts a SET output_role='v23_frozen_base' FROM photo.backend_documents d WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND d.tenant_id=%s AND d.run_id=%s AND d.purpose IN('v23_frozen_base','v23_frozen_approved')",(tid,run)).rowcount
        assert changed>0
    else:
        # Fail closed on a mismatched otherwise-ready physical descriptor, no object IO.
        changed=db.execute("UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id IN (SELECT artifact_id FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s)",(tid,tid,run)).rowcount
        assert changed>0
    before=db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    assert db.execute('SELECT photo.v23_cancelled_training_repair_ready(%s,%s)',(tid,run)).fetchone()==(False,)
    if invalid=='marker':
        # The original legitimate fixture remains eligible; isolate the malformed
        # constructed neighbor through the owner-only exact-scope primitive.
        assert db.execute('SELECT photo.release_v23_cancelled_training_quota(%s,%s)',(tid,run)).fetchone()==(False,)
    else:
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(False,)
        db.execute('RESET ROLE')
    assert db.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',(qid,)).fetchone()==('reserved',remaining)
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==before

@pytest.mark.migration_head(2588)
def test_historical_repair_upgrade_keeps_existing_authority_and_has_exact_new_acl(db):
    before=db.execute("SELECT oid,proowner,proacl,proconfig,prosrc FROM pg_proc WHERE oid='photo.finalize_run_cancellation()'::regprocedure").fetchone()
    tables=db.execute("SELECT oid,relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid IN ('photo.quota_reservations'::regclass,'photo.tenant_usage'::regclass,'photo.output_uploads'::regclass,'photo.backend_documents'::regclass) ORDER BY oid").fetchall()
    _migration(db,2589)
    assert db.execute("SELECT oid,proowner,proacl,proconfig,prosrc FROM pg_proc WHERE oid='photo.finalize_run_cancellation()'::regprocedure").fetchone()==before
    assert db.execute("SELECT oid,relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid IN ('photo.quota_reservations'::regclass,'photo.tenant_usage'::regclass,'photo.output_uploads'::regclass,'photo.backend_documents'::regclass) ORDER BY oid").fetchall()==tables
    for signature in ('photo.v23_cancelled_training_repair_ready(uuid,uuid)','photo.release_v23_cancelled_training_quota(uuid,uuid)','photo.release_next_v23_cancelled_training_quota()'):
        row=db.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig,prorettype::regtype::text,proretset FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()
        assert row[:2]==('photo_fn_owner',True) and row[3:]==('boolean',False)
        assert row[2]==['search_path=pg_catalog, photo']
        for role in ('photo_api','photo_worker','photo_gateway','photo_portal','photo_auth','photo_publisher','photo_maintenance','photo_scheduler'):
            allowed=signature.endswith('quota()') and role=='photo_scheduler'
            assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(allowed,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(False,)
    db.execute('RESET ROLE')


# Draft additions only; not collected/executed until Lens releases tracked source.
import psycopg
from psycopg.types.json import Jsonb


def _typed_terminal_copy(db, template_run, *, malformed=False):
    """Explicit synthetic relational neighbor, never called an admitted training run.

    Copy the legitimate cancelled fixture's bindings at INSERT (no immutable
    update/trigger bypass). Keep workflow/profile/snapshot + nonempty terminal
    typed tasks valid, isolating malformed profile marker as the sole refusal.
    No outputs/documents are manufactured or claimed physically cleaned.
    """
    run, workflow, quota = uuid4(), uuid4(), uuid4()
    override = {'id': str(run), 'output_collection_id': None}
    if malformed:
        config = db.execute('SELECT config_snapshot FROM photo.runs WHERE id=%s',
                            (template_run,)).fetchone()[0]
        config = dict(config, v23_training=dict(config['v23_training'], profile_id='not-a-uuid'))
        override['config_snapshot'] = config
    db.execute('INSERT INTO photo.runs SELECT (jsonb_populate_record(NULL::photo.runs,to_jsonb(r)||%s)).* FROM photo.runs r WHERE id=%s',
               (Jsonb(override), template_run))
    db.execute('INSERT INTO photo.v2_workflows SELECT (jsonb_populate_record(NULL::photo.v2_workflows,to_jsonb(w)||%s)).* FROM photo.v2_workflows w WHERE run_id=%s',
               (Jsonb({'id':str(workflow),'run_id':str(run)}), template_run))
    # Canonical results are not copied across tasks. These terminal cancellation
    # rows have no live lease or result, and retain known real Params references.
    count = db.execute("INSERT INTO photo.tasks SELECT (jsonb_populate_record(NULL::photo.tasks,to_jsonb(t)||jsonb_build_object('id',gen_random_uuid(),'run_id',%s::uuid,'state','cancelled','canonical_result_id',NULL,'lease_until',NULL,'worker_id',NULL))).* FROM photo.tasks t WHERE run_id=%s",
                       (run,template_run)).rowcount
    assert count>0
    tid,actor = db.execute('SELECT tenant_id,created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()
    db.execute("INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,%s,'user',%s,'run',%s,17,0,'reserved',clock_timestamp()-interval '1 day')",(quota,tid,actor,run))
    db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+17 WHERE tenant_id=%s',(tid,))
    return run,quota


@pytest.mark.migration_head(2586)
def test_historical_discovery_filters_65_permanently_invalid_before_limit(db):
    main,pending,(qid,remaining)=_historical(db)
    tid,run=main['tenant'],pending['run']
    for n in (2587,2588,2589): _migration(db,n)
    invalid=[]
    for _ in range(65):
        bad,q=_typed_terminal_copy(db,run,malformed=True)
        db.execute("UPDATE photo.runs SET created_at=clock_timestamp()-interval '1 day' WHERE id=%s",(bad,))
        assert db.execute('SELECT photo.v23_cancelled_training_repair_ready(%s,%s)',(tid,bad)).fetchone()==(False,)
        invalid.append(q)
    before=db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(True,)
    assert db.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',(qid,)).fetchone()==('released',0)
    assert db.execute("SELECT count(*) FROM photo.quota_reservations WHERE id=ANY(%s::uuid[]) AND state='reserved' AND bytes=17",(invalid,)).fetchone()==(65,)
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(before[0]-remaining,before[1])


@pytest.mark.migration_head(2586)
@pytest.mark.parametrize('locked', ['tenant','usage','run','quota','artifact'])
def test_historical_repair_skips_contended_lock_and_rechecks_after_release(database,locked):
    # Isolated DB ownership belongs to the standard fixture. Commit setup so
    # another actual connection can lock it; no unknown resources are touched.
    with psycopg.connect(**database,autocommit=True) as setup:
        with setup.transaction():
            main,pending,(qid,remaining)=_historical(setup)
            tid,run=main['tenant'],pending['run']
            for n in (2587,2588,2589): _migration(setup,n)
            artifact=setup.execute('SELECT artifact_id FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s ORDER BY id LIMIT 1',(tid,run)).fetchone()[0]
            next_quota=None
            if locked in ('run','quota','artifact'):
                next_run,next_quota=_typed_terminal_copy(setup,run)
                assert setup.execute('SELECT photo.v23_cancelled_training_repair_ready(%s,%s)',(tid,next_run)).fetchone()==(True,)
            setup.execute('SET CONSTRAINTS ALL IMMEDIATE')
        before=setup.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
        queries={
            'tenant':('SELECT id FROM photo.tenants WHERE id=%s FOR UPDATE',tid),
            'usage':('SELECT tenant_id FROM photo.tenant_usage WHERE tenant_id=%s FOR UPDATE',tid),
            'run':('SELECT id FROM photo.runs WHERE id=%s FOR UPDATE',run),
            'quota':('SELECT id FROM photo.quota_reservations WHERE id=%s FOR UPDATE',qid),
            'artifact':('SELECT id FROM photo.artifacts WHERE id=%s FOR UPDATE',artifact),
        }
        with psycopg.connect(**database) as blocker:
            with blocker.transaction():
                query,key=queries[locked]
                assert blocker.execute(query,(key,)).fetchone()
                with setup.transaction():
                    setup.execute("SET LOCAL statement_timeout='2s'")
                    setup.execute('SET LOCAL ROLE photo_scheduler')
                    assert setup.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(next_quota is not None,)
                if next_quota is not None:
                    assert setup.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',(next_quota,)).fetchone()==('released',0)
                assert setup.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',(qid,)).fetchone()==('reserved',remaining)
        with setup.transaction():
            setup.execute('SET LOCAL ROLE photo_scheduler')
            assert setup.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(True,)
            assert setup.execute('SELECT photo.release_next_v23_cancelled_training_quota()').fetchone()==(False,)
        assert setup.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(before[0]-remaining-(17 if next_quota is not None else 0),before[1])

@pytest.mark.parametrize('state',['installed','missing','revoked'])
def test_scheduler_startup_requires_actual_training_repair_signature_and_execute(db,state):
    signature='photo.release_next_v23_cancelled_training_quota()'
    # These exact two predicates are mounted in scheduler's existing startup gate.
    presence="to_regprocedure('"+signature+"') IS NOT NULL"
    execute="has_function_privilege(current_user,to_regprocedure('"+signature+"'),'EXECUTE')"
    service=(ROOT/'backend/internal/workflow/service.go').read_text()
    assert service.count(presence)==1 and service.count(execute)==1
    if state=='missing':
        db.execute('ALTER FUNCTION photo.release_next_v23_cancelled_training_quota() RENAME TO missing_v23_training_repair_test')
    elif state=='revoked':
        db.execute('REVOKE EXECUTE ON FUNCTION photo.release_next_v23_cancelled_training_quota() FROM photo_scheduler')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT '+presence+' AND '+execute).fetchone()==(state=='installed',)
    db.execute('RESET ROLE')
