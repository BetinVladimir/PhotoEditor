"""Tenant deletion must drain every admitted/draft run via scheduler cancellation."""
from uuid import uuid4
from pathlib import Path
import pytest
from layer_original_fixture import seed_original_composition



def seed_untouched_runs(db,case):
    """Full-row witnesses for terminal source runs and active foreign tenant runs."""
    neighbor=seed_original_composition(db)
    ids=[]
    for scope,states in ((case,('completed','completed_with_errors','failed','cancelled')),
                         (neighbor,('draft','queued','running','waiting_review'))):
        for state in states:
            run=uuid4();ids.append(run)
            db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'apply',%s,%s,'{}',%s,%s)",(run,scope['Tenant'],state,scope['Snapshot'],'a'*64,scope['Actor']))
    return ids,db.execute('SELECT id,to_jsonb(r) FROM photo.runs r WHERE id=ANY(%s) ORDER BY id',(ids,)).fetchall()


def assert_untouched_runs(db,witness):
    ids,before=witness
    assert db.execute('SELECT id,to_jsonb(r) FROM photo.runs r WHERE id=ANY(%s) ORDER BY id',(ids,)).fetchall()==before

def test_tenant_deletion_moves_all_nonterminal_runs_to_cancelling(db):
    case=seed_original_composition(db)
    tenant,actor,session=case['Tenant'],case['Actor'],case['Session']
    untouched=seed_untouched_runs(db,case)
    ids=[]
    for state in ('draft','queued','running','waiting_review'):
        run=uuid4();ids.append(run)
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'apply',%s,%s,'{}',%s,%s)",(run,tenant,state,case['Snapshot'],'a'*64,actor))
    revision=db.execute('SELECT revision FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
    job=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT outcome,job_id FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,job)).fetchone()==('queued',job)
        assert db.execute('SELECT outcome,job_id FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,uuid4())).fetchone()==('deleting',job)
    db.execute("SET LOCAL ROLE NONE")
    assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=ANY(%s) AND status='cancelling' AND cancel_requested AND revision=2",(tenant,ids)).fetchone()[0]==4
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        for _ in range(16):
            if not db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0]:break
    db.execute("SET LOCAL ROLE NONE")
    assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=ANY(%s) AND status='cancelled'",(tenant,ids)).fetchone()[0]==4
    assert_untouched_runs(db,untouched)


@pytest.mark.migration_head(973)
def test_upgrade_repairs_existing_queued_deletion_runs_without_acl_drift(db):
    case=seed_original_composition(db)
    tenant,actor,session=case['Tenant'],case['Actor'],case['Session']
    untouched=seed_untouched_runs(db,case)
    ids=[]
    for state in ('draft','queued','running','waiting_review'):
        run=uuid4();ids.append(run)
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'apply',%s,%s,'{}',%s,%s)",(run,tenant,state,case['Snapshot'],'a'*64,actor))
    revision=db.execute('SELECT revision FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
    before=db.execute("SELECT oid,proowner,proacl FROM pg_proc WHERE oid='photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid)'::regprocedure").fetchone()
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute('SELECT outcome FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,uuid4())).fetchone()[0]=='queued'
    db.execute('SET LOCAL ROLE NONE')
    assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=ANY(%s) AND status='cancelling'",(tenant,ids)).fetchone()[0]==1
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00974_tenant_deletion_cancellation_drain.sql'
    db.execute(migration.read_text().split('-- +goose Down',1)[0])
    assert db.execute("SELECT oid,proowner,proacl FROM pg_proc WHERE oid='photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid)'::regprocedure").fetchone()==before
    repaired=db.execute("SELECT id,status,cancel_requested,revision FROM photo.runs WHERE tenant_id=%s AND id=ANY(%s)",(tenant,ids)).fetchall()
    assert {row[0]:(row[1],row[2],row[3]) for row in repaired}=={run:('cancelling',True,revision) for run,revision in zip(ids,(2,2,3,3))}
    assert_untouched_runs(db,untouched)


def test_tenant_deletion_revision_exhaustion_is_atomic(db):
    import psycopg
    case=seed_original_composition(db)
    tenant,actor,session=case['Tenant'],case['Actor'],case['Session']
    run=uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,revision) VALUES(%s,%s,'apply','running',%s,'{}',%s,%s,9007199254740991)",(run,tenant,case['Snapshot'],'a'*64,actor))
    before=db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s',(tenant,)).fetchone()
    snapshot=db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',(run,)).fetchone()
    revision=db.execute('SELECT revision FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
    with pytest.raises(psycopg.Error) as failure:
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute('SELECT outcome FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,uuid4()))
    assert failure.value.sqlstate=='23514' and 'REVISION_EXHAUSTED' in str(failure.value)
    assert db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s',(tenant,)).fetchone()==before
    assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',(run,)).fetchone()==snapshot
    assert db.execute("SELECT count(*) FROM photo.deletion_jobs WHERE tenant_id=%s",(tenant,)).fetchone()[0]==0
