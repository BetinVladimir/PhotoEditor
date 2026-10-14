"""Current-migration SQL quarantine authority and physical-version admission."""
from uuid import uuid4
import psycopg
import pytest
from test_database import tenant


def test_quarantine_registry_private_and_trigger_definer_owned(db):
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_maintenance',
                 'photo_portal','photo_publisher','photo_auth'):
        for table in ('photo.restore_quarantine_runs','photo.restore_quarantine_versions'):
            for privilege in ('SELECT','INSERT','UPDATE','DELETE'):
                assert db.execute('SELECT has_table_privilege(%s,%s,%s)',(role,table,privilege)).fetchone()==(False,)
        assert db.execute("SELECT has_function_privilege(%s,'photo.refuse_restore_quarantined_artifact()','EXECUTE')",(role,)).fetchone()==(False,)
        assert db.execute("SELECT has_function_privilege(%s,'photo.refuse_restore_referenced_quarantine()','EXECUTE')",(role,)).fetchone()==(False,)
    row=db.execute("SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid='photo.refuse_restore_quarantined_artifact()'::regprocedure").fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    definition=db.execute("SELECT pg_get_functiondef('photo.refuse_restore_quarantined_artifact()'::regprocedure)").fetchone()[0]
    assert "transaction_isolation" in definition
    assert "pg_advisory_xact_lock" in definition
    assert definition.index('pg_advisory_xact_lock')<definition.index('restore_quarantine_versions')
    assert db.execute("SELECT tgenabled FROM pg_trigger WHERE tgrelid='photo.artifacts'::regclass AND tgname='refuse_restore_quarantined_artifact'").fetchone()==('O',)
    reverse=db.execute("SELECT prosecdef,pg_get_userbyid(proowner),pg_get_functiondef(oid) FROM pg_proc WHERE oid='photo.refuse_restore_referenced_quarantine()'::regprocedure").fetchone()
    assert reverse[0] and reverse[1]=='photo_fn_owner'
    assert reverse[2].index('pg_advisory_xact_lock')<reverse[2].index('photo.artifacts')
    assert db.execute("SELECT tgenabled FROM pg_trigger WHERE tgrelid='photo.restore_quarantine_versions'::regclass AND tgname='refuse_restore_referenced_quarantine'").fetchone()==('O',)
    for table in ('restore_quarantine_runs','restore_quarantine_versions'):
        assert db.execute("SELECT tgenabled FROM pg_trigger WHERE tgrelid=%s::regclass AND tgname='refuse_restore_quarantine_mutation'",('photo.'+table,)).fetchone()==('O',)
        assert db.execute("SELECT tgenabled FROM pg_trigger WHERE tgrelid=%s::regclass AND tgname='refuse_restore_quarantine_truncate'",('photo.'+table,)).fetchone()==('O',)
    index=db.execute("SELECT indexdef FROM pg_indexes WHERE schemaname='photo' AND tablename='artifacts' AND indexname='artifacts_ready_physical_quarantine_idx'").fetchone()
    assert index is not None and '(bucket, object_key, object_version)' in index[0]
    assert "state = 'ready'" in index[0]


def test_quarantine_blocks_ready_insert_update_and_cross_tenant_reuse(db):
    first,_=tenant(db,str(uuid4())+'@example.test')
    second,_=tenant(db,str(uuid4())+'@example.test')
    run=uuid4();key='quarantine/'+str(uuid4())
    db.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
    db.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
    insert="INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,%s,%s,7,'application/octet-stream') RETURNING id"
    for scope in (first,second):
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_OBJECT_QUARANTINED'):
            with db.transaction():db.execute(insert,(scope,key,'v1','b'*64))
    artifact=db.execute(insert,(first,key,'v2','b'*64)).fetchone()[0]
    with pytest.raises(psycopg.errors.CheckViolation,match='immutable field artifacts.object_version'):
        with db.transaction():db.execute("UPDATE photo.artifacts SET object_version='v1' WHERE id=%s",(artifact,))
    assert db.execute('SELECT state,object_version FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('ready','v2')
    deleting=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream','deleting') RETURNING id",(first,key+'-deleting','b'*64)).fetchone()[0]
    second_run=uuid4()
    db.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(second_run,'c'*64))
    db.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(second_run,key+'-deleting'))
    with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_OBJECT_QUARANTINED'):
        with db.transaction():db.execute("UPDATE photo.artifacts SET state='ready' WHERE id=%s",(deleting,))
    assert db.execute('SELECT state FROM photo.artifacts WHERE id=%s',(deleting,)).fetchone()==('deleting',)


def test_quarantine_physical_update_cannot_retarget_a_ready_artifact(db):
    scope,_=tenant(db,str(uuid4())+'@example.test')
    ready='quarantine/ready/'+str(uuid4())
    quarantined='quarantine/extra/'+str(uuid4())
    db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream')",(scope,ready,'b'*64))
    run=uuid4()
    db.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
    db.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,quarantined))
    with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_IMMUTABLE'):
        with db.transaction():
            db.execute('UPDATE photo.restore_quarantine_versions SET object_key=%s WHERE run_id=%s AND bucket=%s AND object_key=%s AND object_version=%s',(ready,run,'test',quarantined,'v1'))
    assert db.execute('SELECT object_key FROM photo.restore_quarantine_versions WHERE run_id=%s',(run,)).fetchone()==(quarantined,)


def test_quarantine_runs_and_versions_are_append_only(db):
    run=uuid4();key='quarantine/append-only/'+str(uuid4())
    db.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
    db.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
    for sql,params in (
        ('UPDATE photo.restore_quarantine_runs SET extra_count=0 WHERE id=%s',(run,)),
        ('DELETE FROM photo.restore_quarantine_runs WHERE id=%s',(run,)),
        ('UPDATE photo.restore_quarantine_versions SET bytes=0 WHERE run_id=%s',(run,)),
        ('DELETE FROM photo.restore_quarantine_versions WHERE run_id=%s',(run,))):
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_IMMUTABLE'):
            with db.transaction():db.execute(sql,params)
    for table in ('restore_quarantine_runs','restore_quarantine_versions'):
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_IMMUTABLE'):
            with db.transaction():db.execute('TRUNCATE photo.'+table+' CASCADE')
    assert db.execute('SELECT extra_count,extra_bytes FROM photo.restore_quarantine_runs WHERE id=%s',(run,)).fetchone()==(1,7)
    assert db.execute('SELECT bytes FROM photo.restore_quarantine_versions WHERE run_id=%s',(run,)).fetchone()==(7,)


def test_quarantine_reverse_fence_is_global_across_tenants(db):
    first,_=tenant(db,str(uuid4())+'@example.test')
    second,_=tenant(db,str(uuid4())+'@example.test')
    key='quarantine/global/'+str(uuid4())
    db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream')",(first,key,'b'*64))
    # The quarantine registry has no tenant column: a second tenant cannot
    # designate another tenant's ready physical version as an unreferenced extra.
    run=uuid4()
    db.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
    assert second!=first
    with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_REFERENCED_OBJECT'):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(second),))
            db.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
    assert db.execute('SELECT count(*) FROM photo.restore_quarantine_versions WHERE run_id=%s',(run,)).fetchone()==(0,)


def test_ready_publication_waits_for_uncommitted_quarantine_then_refuses(database):
    key='quarantine/race/'+str(uuid4())
    with psycopg.connect(**database) as registrar:
        scope,_=tenant(registrar,str(uuid4())+'@example.test')
        registrar.commit()
    insert="INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream')"
    with psycopg.connect(**database) as quarantine,psycopg.connect(**database) as publisher:
        with quarantine.transaction():
            run=uuid4()
            quarantine.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
            quarantine.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with publisher.transaction():
                    publisher.execute("SET LOCAL lock_timeout='100ms'")
                    publisher.execute(insert,(scope,key,'b'*64))
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_OBJECT_QUARANTINED'):
            with publisher.transaction():
                publisher.execute(insert,(scope,key,'b'*64))


def test_quarantine_activation_waits_for_ready_publication_then_observes_it(database):
    key='quarantine/race/'+str(uuid4())
    with psycopg.connect(**database) as registrar:
        scope,_=tenant(registrar,str(uuid4())+'@example.test')
        registrar.commit()
    insert="INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream')"
    physical=('test',key,'v1')
    with psycopg.connect(**database) as publisher,psycopg.connect(**database) as quarantine:
        with publisher.transaction():
            publisher.execute(insert,(scope,key,'b'*64))
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with quarantine.transaction():
                    quarantine.execute("SET LOCAL lock_timeout='100ms'")
                    run=uuid4()
                    quarantine.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
                    quarantine.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
        with quarantine.transaction():
            ready=quarantine.execute("SELECT EXISTS(SELECT FROM photo.artifacts WHERE state='ready' AND bucket=%s AND object_key=%s AND object_version=%s)",physical).fetchone()
            assert ready==(True,)
            run=uuid4()
            quarantine.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
            with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_REFERENCED_OBJECT'):
                with quarantine.transaction():
                    quarantine.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))


def test_physical_fences_reject_stale_transaction_snapshots(database):
    key='quarantine/isolation/'+str(uuid4())
    with psycopg.connect(**database) as registrar:
        scope,_=tenant(registrar,str(uuid4())+'@example.test')
        registrar.commit()
    with psycopg.connect(**database) as connection:
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_REQUIRES_READ_COMMITTED'):
            with connection.transaction():
                connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ')
                connection.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES(%s,'test',%s,'v1',%s,7,'application/octet-stream')",(scope,key,'b'*64))
        with pytest.raises(psycopg.errors.CheckViolation,match='RESTORE_QUARANTINE_REQUIRES_READ_COMMITTED'):
            with connection.transaction():
                connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ')
                run=uuid4()
                connection.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,1,7)',(run,'a'*64))
                connection.execute("INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,'test',%s,'v1',7)",(run,key))
