"""Region catalog/policies are present without activating an incomplete renderer."""


def test_region_operation_registered_with_bounded_outputs_but_disabled(db):
    row=db.execute("SELECT version,resource_class,routing_key,backends,v2_enabled FROM photo.operation_registry WHERE operation='region.preview'").fetchone()
    assert row==('1','gpu','gpu.region_preview',['cuda'],False)
    rows=db.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='region.preview' AND version='1' ORDER BY role").fetchall()
    assert rows==[('derivative',['image/jpeg','image/webp'],16777216,1),('document',['application/json'],8388608,1)]


def test_region_adoption_params_is_scheduler_only_and_generation_scoped(db):
    from uuid import uuid4
    signature='photo.v26_region_adoption_params(uuid,uuid,integer,bigint,uuid)'
    for role,allowed in [('photo_api',False),('photo_scheduler',True),('photo_worker',False)]:
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(allowed,)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            assert db.execute('SELECT * FROM photo.v26_region_adoption_params(%s,%s,%s,%s,%s)',(uuid4(),uuid4(),1,1,uuid4())).fetchone() is None
        finally:db.execute('RESET ROLE')


def test_locked_region_scope_is_scheduler_only(db):
    signature='photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb)'
    for role,allowed in [('photo_api',False),('photo_scheduler',True),('photo_worker',False)]:
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(allowed,)


def test_region_commit_wrapper_cannot_be_bypassed_by_scheduler(db):
    from uuid import uuid4
    signature='photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)'
    predecessor='photo.adopt_v2_plan_pre_region_authority(uuid,uuid,integer,bigint,uuid,jsonb,text)'
    for role in ('photo_api', 'photo_scheduler', 'photo_worker'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          (role,signature,'EXECUTE')).fetchone()==(role=='photo_scheduler',)
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          (role,predecessor,'EXECUTE')).fetchone()==(False,)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
                              (uuid4(),uuid4(),uuid4(),'[]','0'*64)).fetchone()==(False,)
        finally:
            db.execute('RESET ROLE')


def test_region_seal_scope_is_api_only_and_rejects_unknown_run(db):
    from uuid import uuid4
    signature='photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb)'
    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(role=='photo_api',)
    tenant=uuid4()
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        try:
            assert db.execute("SELECT photo.v26_region_seal_scope(%s,%s,%s,%s,'{}')",(tenant,uuid4(),uuid4(),uuid4())).fetchone()==(False,)
        finally:db.execute('RESET ROLE')


def test_region_worker_admission_is_worker_only(db):
    from uuid import uuid4
    signature='photo.admit_v26_region_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(role=='photo_worker',)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        try:
            assert db.execute('SELECT photo.admit_v26_region_preview(%s,%s,%s,1,1,%s,%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,'{}','b'*64)).fetchone()==(False,)
        finally:db.execute('RESET ROLE')


def test_early_region_availability_is_api_only_tenant_scoped_and_disabled(db):
    from uuid import uuid4
    from test_v23_record_correction import _fixture
    tenant,_,_,_,_,_=_fixture(db)
    signature='photo.v26_region_available(uuid)'
    for role,allowed in [('photo_api',True),('photo_scheduler',False),('photo_worker',False)]:
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(allowed,)
    def check(target=tenant,context=tenant):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(context),))
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute('SELECT photo.v26_region_available(%s)',(target,)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert check() is False
    with db.transaction(force_rollback=True):
        # SQL availability fixture only; no production renderer qualification.
        db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
        assert check() is True
        assert check(context=uuid4()) is False
        assert check(target=uuid4()) is False
    assert check() is False


def test_disabled_region_go_queue_stops_before_external_preparation(database):
    import os
    import subprocess
    from pathlib import Path
    from psycopg.conninfo import make_conninfo
    env=dict(os.environ,PHOTO_REGION_QUEUE_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2region','-run','^TestIntegrationDisabledRegionQueueStopsBeforePreparation$','-count=1'],cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=30)
    assert result.returncode==0,result.stdout+result.stderr


def test_region_scheduler_scope_wrappers_are_narrow_and_role_bound(db):
    from uuid import uuid4
    for name in ('admission','seal'):
        signature=f'photo.v26_region_{name}_scope_scheduler(uuid,uuid,uuid,uuid,jsonb)'
        for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway'):
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(role=='photo_scheduler',)
        tenant=uuid4()
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:
                assert db.execute(f"SELECT photo.v26_region_{name}_scope_scheduler(%s,%s,%s,%s,'{{}}')",(tenant,uuid4(),uuid4(),uuid4())).fetchone()==(False,)
            finally:db.execute('RESET ROLE')
