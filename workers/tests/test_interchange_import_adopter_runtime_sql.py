"""Real-role readiness needs both independent V30 import processes."""

import os
from uuid import uuid4

import psycopg
import pytest


def test_import_readiness_requires_scheduler_and_adopter_leases():
    dsn = os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not dsn:
        pytest.skip('isolated fully migrated PostgreSQL required')
    scheduler_id, adopter_id = uuid4(), uuid4()
    with psycopg.connect(dsn, autocommit=True,
            options='-c role=photo_scheduler -c statement_timeout=3000') as scheduler:
        with psycopg.connect(dsn, autocommit=True,
                options='-c role=photo_api -c statement_timeout=3000') as api:
            def ready():
                return api.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]
            def ready_for(bucket):
                return api.execute('SELECT photo.v30_import_runtime_ready(%s)',
                                   (bucket,)).fetchone()[0]
            try:
                assert ready() is False
                assert scheduler.execute('SELECT photo.announce_v30_import_runtime_bound(%s,%s)',
                    (scheduler_id, 'ab')).fetchone() == (False,)
                assert scheduler.execute(
                    'SELECT photo.announce_v30_import_adopter_runtime_bound(%s,%s)',
                    (adopter_id, 'x' * 64)).fetchone() == (False,)
                assert ready_for('ab') is False
                assert scheduler.execute('SELECT photo.announce_v30_import_runtime_bound(%s,%s)',
                    (scheduler_id, 'photo')).fetchone() == (True,)
                assert ready() is False
                assert scheduler.execute(
                    'SELECT photo.announce_v30_import_adopter_runtime_bound(%s,%s)',
                    (adopter_id, 'different-photo')).fetchone() == (True,)
                assert ready() is False
                assert scheduler.execute(
                    'SELECT photo.announce_v30_import_adopter_runtime_bound(%s,%s)',
                    (adopter_id, 'photo')).fetchone() == (True,)
                assert ready() is True
                assert ready_for('photo') is True
                assert ready_for('different-photo') is False
                scheduler.execute('SELECT photo.withdraw_v30_import_adopter_runtime(%s)',
                                  (adopter_id,))
                assert ready() is False
                assert api.execute('''
                    SELECT has_function_privilege('photo_api',
                        'photo.announce_v30_import_adopter_runtime_bound(uuid,text)','EXECUTE')
                ''').fetchone() == (False,)
            finally:
                scheduler.execute('SELECT photo.withdraw_v30_import_adopter_runtime(%s)',
                                  (adopter_id,))
                scheduler.execute('SELECT photo.withdraw_v30_import_runtime(%s)',
                                  (scheduler_id,))
