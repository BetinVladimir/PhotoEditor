from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture

def test_relight_source_current_accepted_snapshot_and_revocation(db):
    def params(pin,scope):
        return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),
            source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)))
    args,scope,_=relight_lease_fixture(db,params_builder=params)
    def ready():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            result=db.execute('SELECT photo.v28_worker_relight_source_ready('+','.join(['%s']*10)+')',args).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert ready() is True
    for query,values in (
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(scope['source'],)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(args[2],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert ready() is False
        assert ready() is True
    sig='photo.v28_worker_relight_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_worker','photo_api','photo_scheduler','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_worker')


def test_relight_depth_origin_worker_only_and_unowned_refused(db):
    from uuid import uuid4
    from hashlib import sha256
    sig='photo.v28_worker_relight_depth_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)'
    for role in ('photo_worker','photo_api','photo_scheduler','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_worker')
    args=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','b'*64,'{}')
    assert db.execute('SELECT photo.v28_worker_relight_depth_origin('+','.join(['%s']*11)+')',args).fetchone()==(None,)


def test_relight_person_origin_worker_only_and_unowned_refused(db):
    from uuid import uuid4
    from hashlib import sha256
    sig='photo.v28_worker_relight_person_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)'
    for role in ('photo_worker','photo_api','photo_scheduler','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_worker')
    args=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','b'*64,'{}')
    assert db.execute('SELECT photo.v28_worker_relight_person_origin('+','.join(['%s']*11)+')',args).fetchone()==(None,)


def test_relight_manual_origin_worker_only_and_unowned_refused(db):
    from uuid import uuid4
    from hashlib import sha256
    sig='photo.v28_worker_relight_manual_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text,boolean)'
    for role in ('photo_worker','photo_api','photo_scheduler','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_worker')
    args=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','b'*64,'{}',None,True)
    assert db.execute('SELECT photo.v28_worker_relight_manual_origin('+','.join(['%s']*13)+')',args).fetchone()==(None,)
