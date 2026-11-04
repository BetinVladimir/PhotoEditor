"""02712: photo_admin limits (FN-D-18) and monthly egress partitions."""
import pytest
from test_video_tables_schema import ctx
from test_video_viewer_functions import delivery

SIG = 'photo.set_video_tenant_limits(uuid,bigint,integer,integer,integer,integer,bigint,boolean,integer,uuid)'


def test_security(db):
    row = db.execute(f"SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege('photo_admin',oid,'EXECUTE'),"
                     f"has_function_privilege('photo_api',oid,'EXECUTE') FROM pg_proc WHERE oid='{SIG}'::regprocedure").fetchone()
    assert row == (True, 'photo_fn_owner', False, True, False)
    assert db.execute("SELECT rolcanlogin FROM pg_roles WHERE rolname='photo_admin'").fetchone() == (False,)


def test_limits_cas_and_audit(db):
    tid, user, pid = ctx(db)
    call = lambda exp, **kw: db.execute(f'SELECT {SIG.split("(")[0]}(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (tid, exp, kw.get('i', 5), None, None, None, kw.get('daily'), kw.get('unl', False), 3, user)).fetchone()[0]
    assert call(0) == 1
    with pytest.raises(Exception, match='REVISION_CONFLICT'), db.transaction():
        call(0)
    assert call(1, i=7) == 2
    assert db.execute('SELECT max_outstanding_ingest,max_active_uploads,revision FROM photo.video_tenant_limits WHERE tenant_id=%s', (tid,)).fetchone() == (7, 3, 2)
    n = db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND action='video.tenant_limits.set'", (tid,)).fetchone()[0]
    assert n == 2
    with pytest.raises(Exception, match='LIMIT_INVALID'), db.transaction():
        call(2, daily=100, unl=True)


def test_partitions_are_created_and_rows_move(db):
    tid, user, cid, rid = delivery(db)
    db.execute("INSERT INTO photo.video_egress_events(tenant_id,render_id,kind,bytes,at) VALUES(%s,%s,'download',5,date_trunc('month',now())+interval '2 days')", (tid, rid))
    assert db.execute('SELECT photo.ensure_video_egress_partitions(2)').fetchone()[0] in (2, 3)
    assert db.execute('SELECT photo.ensure_video_egress_partitions(2)').fetchone()[0] == 0
    part = db.execute("SELECT tableoid::regclass::text FROM photo.video_egress_events WHERE tenant_id=%s", (tid,)).fetchone()[0]
    assert 'video_egress_events_20' in part
    assert db.execute('SELECT count(*) FROM photo.video_egress_events_default WHERE tenant_id=%s', (tid,)).fetchone()[0] == 0
    # new rows land in a monthly partition, not the default one
    db.execute("INSERT INTO photo.video_egress_events(tenant_id,render_id,kind,bytes) VALUES(%s,%s,'download',6)", (tid, rid))
    assert db.execute('SELECT count(*) FROM photo.video_egress_events_default').fetchone()[0] == 0


def test_partition_function_is_maintenance_only(db):
    assert db.execute("SELECT has_function_privilege('photo_maintenance','photo.ensure_video_egress_partitions(integer)','EXECUTE'),"
                      "has_function_privilege('photo_api','photo.ensure_video_egress_partitions(integer)','EXECUTE')").fetchone() == (True, False)
