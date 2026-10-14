"""SQL authority only; synthetic metadata does not prove document quality."""
from uuid import uuid4
from test_v27_semantic_final_producer_gate import fixture


def test_review_source_locks_fence_concurrent_revocation_and_producer_changes(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as setup:
        tid,task,result,producer,_,candidate,scope=fixture(setup,snapshot_authority=True)
        workflow,run=setup.execute('SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
        setup.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(run,))
        setup.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        revision=setup.execute('SELECT revision FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    with psycopg.connect(**database) as api,psycopg.connect(**database) as competing:
        api.execute('SET ROLE photo_api');api.commit()
        args=(tid,workflow,scope['actor'],scope['session'],revision)
        stale=(*args[:-1],revision+1)
        assert not api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s)',stale).fetchone()[0]
        api.rollback()
        assert api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s)',args).fetchone()[0]
        for query,values in (
            ("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tid,)),
            ("UPDATE photo.runs SET status='cancelled' WHERE id=%s",(run,)),
            ('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(producer,)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
            ("UPDATE photo.asset_versions SET review_state='accepted' WHERE id=%s",(scope['version'],)),
            ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
            ("UPDATE photo.memberships SET status='suspended' WHERE tenant_id=%s AND user_id=%s",(tid,scope['actor'])),
            ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
            ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
            ("UPDATE photo.users SET status='disabled' WHERE id=%s",(scope['actor'],)),
        ):
            with competing.transaction(force_rollback=True):
                competing.execute("SET LOCAL lock_timeout='100ms'")
                with pytest.raises(psycopg.errors.LockNotAvailable):competing.execute(query,values)
        api.rollback()
        competing.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],));competing.commit()
        assert not api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s)',args).fetchone()[0]


def test_review_lock_wait_rereads_committed_producer_generation(database):
    import psycopg
    import time
    from concurrent.futures import ThreadPoolExecutor
    with psycopg.connect(**database) as setup:
        tid,task,result,producer,_,_,scope=fixture(setup,snapshot_authority=True)
        workflow,run=setup.execute('SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
        setup.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(run,))
        setup.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        revision=setup.execute('SELECT revision FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    with psycopg.connect(**database) as producer_tx,psycopg.connect(**database) as api,psycopg.connect(**database,autocommit=True) as observer:
        api.execute('SET ROLE photo_api');api.commit()
        api.execute("SET statement_timeout='5000ms'");api.commit()
        producer_tx.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(producer,))
        with ThreadPoolExecutor(max_workers=1) as executor:
            future=executor.submit(lambda:api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s)',(tid,workflow,scope['actor'],scope['session'],revision)).fetchone()[0])
            try:
                deadline=time.monotonic()+3
                while time.monotonic()<deadline:
                    waiting=observer.execute('SELECT wait_event_type FROM pg_stat_activity WHERE pid=%s',(api.info.backend_pid,)).fetchone()
                    if waiting and waiting[0]=='Lock':break
                    time.sleep(.01)
                else:raise AssertionError('review transaction did not wait for producer lock')
                producer_tx.commit()
                assert future.result(timeout=5) is False
            finally:
                producer_tx.rollback()


def test_reference_outside_target_snapshot_is_scoped_and_locked(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as setup:
        tid,task,result,_,_,_,scope=fixture(setup,snapshot_authority=True)
        workflow,run=setup.execute('SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
        source=setup.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,'fixture',%s,%s,2,'application/json','v1',clock_timestamp()) RETURNING id",(tid,str(uuid4()),'b'*64)).fetchone()[0]
        def reference(project):
            asset=setup.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,scope['original'])).fetchone()[0]
            version=setup.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,'b'*64)).fetchone()[0]
            return asset,version
        asset,version=reference(scope['project'])
        other_project=setup.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Other') RETURNING id",(tid,)).fetchone()[0]
        _,foreign_version=reference(other_project)
        setup.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(run,))
        setup.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        revision=setup.execute('SELECT revision FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
        assert not setup.execute('SELECT EXISTS(SELECT 1 FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s AND asset_version_id=%s)',(tid,scope['snapshot'],version)).fetchone()[0]
    with psycopg.connect(**database) as api,psycopg.connect(**database) as competing:
        api.execute('SET ROLE photo_api');api.commit()
        prefix=(tid,workflow,scope['actor'],scope['session'],revision)
        for rejected in (uuid4(),foreign_version):
            assert not api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s,%s)',(*prefix,rejected)).fetchone()[0]
            api.rollback()
        assert api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s,%s)',(*prefix,version)).fetchone()[0]
        for query,args in (
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(source,)),
            ("UPDATE photo.asset_versions SET review_state='accepted' WHERE id=%s",(version,)),
            ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(asset,)),
        ):
            with competing.transaction(force_rollback=True):
                competing.execute("SET LOCAL lock_timeout='100ms'")
                with pytest.raises(psycopg.errors.LockNotAvailable):competing.execute(query,args)
        api.rollback()
        competing.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(asset,));competing.commit()
        assert not api.execute('SELECT photo.lock_semantic_review_sources(%s,%s,%s,%s,%s,%s)',(*prefix,version)).fetchone()[0]


def test_review_plan_sources_require_adoption_editor_and_exact_generation(db):
    tid,task,result,producer,_,candidate,scope=fixture(db,snapshot_authority=True)
    workflow,run=db.execute('SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
    def inventory(tenant=tid,actor=scope['actor'],session=scope['session'],owner=workflow):
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_api')
            return db.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',(tenant,owner,actor,session)).fetchall()
    assert inventory()==[]
    db.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(run,))
    assert inventory()==[]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
    rows=inventory()
    assert len([r for r in rows if r[0]=='SemanticPlan'])==1
    assert {r[0] for r in rows}=={'SemanticPlan','Params','InputSnapshot','TransferCandidate','Calibration'}
    for arguments in ({'tenant':uuid4()},{'actor':uuid4()},{'session':uuid4()},{'owner':uuid4()}):
        assert inventory(**arguments)==[]
    root=next(r[1] for r in rows if r[0]=='SemanticPlan')
    producer_params=db.execute('SELECT params_artifact_id FROM photo.tasks WHERE id=%s',(producer,)).fetchone()[0]
    for query,args in (
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(task,)),
        ('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(producer,)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['source'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(producer_params,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(root,)),
        ("UPDATE photo.runs SET status='cancelled' WHERE id=%s",(run,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert inventory()==[]
    db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",(run,))
    assert inventory()==rows
    assert not db.execute("SELECT has_function_privilege('photo_api','photo.semantic_review_plan_sources_pre_current(uuid,uuid,uuid,uuid)','EXECUTE')").fetchone()[0]
