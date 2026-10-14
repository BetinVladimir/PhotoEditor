"""Prepared recipe inputs are visible only under current import adoption scope."""

from uuid import uuid4


def test_import_adoption_input_inventory_private_and_default_closed(db):
    signature = 'photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway',
                 'photo_portal', 'photo_maintenance'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_scheduler', signature, 'EXECUTE')).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT count(*) FROM photo.v30_import_adoption_input_inventory(%s,%s,1,1,%s)',
                      (uuid4(), uuid4(), uuid4())).fetchone()[0] == 0
    db.execute('RESET ROLE')
