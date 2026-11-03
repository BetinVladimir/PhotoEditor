"""02683 video upload sessions: constraints, uniqueness and closed direct DML."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_database import tenant

ROLES = ['photo_api', 'photo_worker', 'photo_scheduler', 'photo_auth', 'photo_portal', 'photo_publisher', 'photo_maintenance', 'photo_gateway']
BASE = dict(media_kind='video', filename='clip.mp4', declared_bytes=1_000_000, declared_sha256='a' * 64, request_sha256='b' * 64,
            part_bytes=33_554_432, parts_total=1, object_key='video/x', state='initializing', expires_at='2030-01-01T00:00:00Z')


def project(db):
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    pid = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Video') RETURNING id", (tid,)).fetchone()[0]
    return tid, user, pid


def insert(db, tid, pid, user, **over):
    v = {**BASE, **over}
    cols = ','.join(v)
    return db.execute(f'INSERT INTO photo.video_upload_sessions(tenant_id,project_id,principal_id,{cols}) VALUES(%s,%s,%s,{",".join(["%s"] * len(v))}) RETURNING id',
                      (tid, pid, user, *v.values())).fetchone()[0]


def violates(db, **over):
    tid, user, pid = project(db)
    with pytest.raises(errors.CheckViolation), db.transaction():
        insert(db, tid, pid, user, **over)


def test_valid_session_is_accepted_and_deferred_fk_checked(db):
    tid, user, pid = project(db)
    assert insert(db, tid, pid, user) is not None
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')


def test_parts_total_must_match_ceil_of_bytes_over_part_bytes(db):
    violates(db, parts_total=2)


def test_size_limit_is_16_gib(db):
    tid, user, pid = project(db)
    assert insert(db, tid, pid, user, declared_bytes=17_179_869_184, parts_total=512, part_bytes=33_554_432, request_sha256='c' * 64)
    violates(db, declared_bytes=17_179_869_185, parts_total=513)


@pytest.mark.parametrize('name', ['a/b.mp4', 'a\\b.mp4', '', 'x' * 256])
def test_bad_filenames_rejected(db, name):
    violates(db, filename=name)


def test_rejected_requires_code_and_state_enum(db):
    violates(db, state='rejected')
    violates(db, state='unknown')


def test_request_key_is_unique_per_principal(db):
    tid, user, pid = project(db)
    insert(db, tid, pid, user)
    with pytest.raises(errors.UniqueViolation), db.transaction():
        insert(db, tid, pid, user)


def test_parts_validate_bounds_and_belong_to_a_session(db):
    tid, user, pid = project(db)
    up = insert(db, tid, pid, user)
    db.execute("INSERT INTO photo.video_upload_parts(tenant_id,upload_id,part_no,etag,bytes) VALUES(%s,%s,1,'e',10)", (tid, up))
    for part, etag, size in [(0, 'e', 1), (10001, 'e', 1), (2, '', 1), (2, 'e', 0)]:
        with pytest.raises(errors.CheckViolation), db.transaction():
            db.execute('INSERT INTO photo.video_upload_parts(tenant_id,upload_id,part_no,etag,bytes) VALUES(%s,%s,%s,%s,%s)', (tid, up, part, etag, size))
    with pytest.raises(errors.UniqueViolation), db.transaction():
        db.execute("INSERT INTO photo.video_upload_parts(tenant_id,upload_id,part_no,etag,bytes) VALUES(%s,%s,1,'e',10)", (tid, up))


@pytest.mark.parametrize('role', ROLES)
@pytest.mark.parametrize('stmt', ['SELECT count(*) FROM photo.video_upload_sessions', 'DELETE FROM photo.video_upload_sessions',
                                  "UPDATE photo.video_upload_sessions SET revision=revision",
                                  'SELECT count(*) FROM photo.video_upload_parts', 'DELETE FROM photo.video_upload_parts'])
def test_application_roles_have_no_direct_access(db, role, stmt):
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute(f'SET LOCAL ROLE {role}')
        db.execute(stmt)


@pytest.mark.parametrize('role', ['photo_api', 'photo_worker'])
def test_application_roles_cannot_insert(db, role):
    tid, user, pid = project(db)
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute(f'SET LOCAL ROLE {role}')
        insert(db, tid, pid, user)
