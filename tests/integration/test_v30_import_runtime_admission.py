"""A live installed scheduler alone may advertise new import admission."""

from uuid import uuid4


def test_import_runtime_lease_roles_and_rotation(db):
    announce = 'photo.announce_v30_import_runtime(uuid)'
    withdraw = 'photo.withdraw_v30_import_runtime(uuid)'
    ready = 'photo.v30_import_runtime_ready()'
    for signature in (announce, withdraw, ready):
        assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                          (signature,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal',
                 'photo_maintenance'):
        for signature in (announce, withdraw):
            assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                                  (role, signature, 'EXECUTE')).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_scheduler', announce, 'EXECUTE')).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_api', ready, 'EXECUTE')).fetchone()[0]
    assert not db.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]
    instance = uuid4()
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.announce_v30_import_runtime(%s)',
                      (instance,)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    db.execute('SELECT photo.withdraw_v30_import_runtime(%s)', (instance,))
    db.execute('RESET ROLE')
    assert not db.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]
    db.execute("UPDATE photo.v30_import_runtime_leases SET "
               "expires_at=clock_timestamp()-interval '2 days' "
               "WHERE instance_id=%s", (instance,))
    replacement = uuid4()
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.announce_v30_import_runtime(%s)',
                      (replacement,)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.v30_import_runtime_leases').fetchone()[0] == 1
    db.execute('SET LOCAL ROLE photo_scheduler')
    db.execute('SELECT photo.withdraw_v30_import_runtime(%s)', (replacement,))
    db.execute('RESET ROLE')
    assert not db.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]


def test_import_parse_new_run_has_runtime_gate(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.begin_v30_interchange_parse_run"
                            "(uuid,uuid,uuid,text,text,text,text,text)'::regprocedure)").fetchone()[0]
    replay = definition.index('IF FOUND THEN')
    readiness = definition.index('photo.v30_import_runtime_ready()')
    assert replay < readiness
