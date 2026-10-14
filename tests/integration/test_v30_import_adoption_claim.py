"""V30 import result discovery is scheduler-only and fenced independently."""

from uuid import uuid4


def test_import_adoption_claim_sql_surface(db):
    signatures = (
        'photo.v30_claim_next_import_adoption()',
        'photo.v30_import_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid)',
        'photo.v30_renew_import_adoption_claim(uuid,uuid,uuid,uuid,uuid,integer,bigint,uuid)',
    )
    for signature in signatures:
        assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                          (signature,)).fetchone()[0]
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          ('photo_scheduler', signature, 'EXECUTE')).fetchone()[0]
        for role in ('photo_api', 'photo_worker', 'photo_gateway',
                     'photo_portal', 'photo_maintenance'):
            assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                                  (role, signature, 'EXECUTE')).fetchone()[0]


def test_import_adoption_claim_default_closed(db):
    db.execute('SET LOCAL ROLE photo_scheduler')
    ids = [uuid4() for _ in range(5)]
    assert not db.execute(
        'SELECT photo.v30_import_adoption_claim_current(%s,%s,%s,%s,%s,1,1,%s)',
        (*ids, uuid4())).fetchone()[0]
    db.execute('RESET ROLE')
