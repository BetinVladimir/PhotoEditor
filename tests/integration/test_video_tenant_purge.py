"""02698: tenant deletion purges video tables (immutable revisions included) and leaves nothing behind."""
from uuid import uuid4
from psycopg import sql
from test_video_tables_schema import ctx, put, revision

def test_tenant_deletion_purges_video_rows(db):
    tid, user, pid = ctx(db)
    sid = db.execute('INSERT INTO photo.video_sequences(tenant_id,project_id,name,fps_num,fps_den,width,height,created_by) VALUES(%s,%s,%s,25,1,1920,1080,%s) RETURNING id', (tid, pid, 's', user)).fetchone()[0]
    put(db, 'video_sequence_revisions', **revision(tid, sid, user, 1, None))
    put(db, 'video_sequence_revisions', **revision(tid, sid, user, 2, 1, 'manual'))
    put(db, 'video_sequence_sources', tenant_id=tid, sequence_id=sid, revision=2, source_kind='video', source_id=uuid4(), source_sha256='a' * 64)
    put(db, 'video_tenant_limits', tenant_id=tid, max_active_uploads=3)
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s", (tid,))
    job = db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s) RETURNING id", (tid, tid, user)).fetchone()[0]
    token = db.execute('SELECT cleanup_token FROM photo.claim_tenant_deletion()').fetchone()[0]
    assert db.execute('SELECT photo.finish_tenant_deletion(%s,%s)', (job, token)).fetchone()[0]
    tables = [r[0] for r in db.execute("SELECT table_name FROM information_schema.columns WHERE table_schema='photo' AND column_name='tenant_id' AND table_name LIKE 'video\\_%' ORDER BY 1")]
    assert 'video_sequence_revisions' in tables and 'video_egress_events' in tables
    left = {t: db.execute(sql.SQL('SELECT count(*) FROM photo.{} WHERE tenant_id=%s').format(sql.Identifier(t)), (tid,)).fetchone()[0] for t in tables}
    assert not {t: c for t, c in left.items() if c}
    assert db.execute("SELECT current_setting('photo.video_gc',true)").fetchone()[0] in ('', None)
