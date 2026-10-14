import psycopg
import pytest


def test_restore_pending_counter_uses_narrow_publisher_authority(db):
    signature='photo.restore_task_outbox_pending(text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_maintenance','photo_portal','photo_auth'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(False,)
    assert db.execute("SELECT has_function_privilege('photo_publisher',%s,'EXECUTE')",(signature,)).fetchone()==(True,)
    database=db.execute('SELECT current_database()').fetchone()[0]
    expected=db.execute('SELECT count(*) FROM photo.outbox WHERE sent_at IS NULL').fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_publisher')
        assert db.execute('SELECT photo.restore_task_outbox_pending(%s)',(database,)).fetchone()==(expected,)
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():db.execute('SELECT photo.restore_task_outbox_pending(%s)',(database+'_wrong',))
