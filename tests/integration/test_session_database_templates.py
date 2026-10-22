"""Real clone checks, opt-in only; current-session migrations remain prerequisite."""
import os
import psycopg
import pytest

pytestmark = pytest.mark.skipif(os.environ.get('PHOTO_TEST_DB_TEMPLATES') != '1',
                                reason='Session database templates are opt-in')


@pytest.mark.parametrize('independent_test', [1, 2])
def test_each_test_receives_independently_committable_database(database, independent_test):
    # The second parameter must not see the first test's committed table/data.
    with psycopg.connect(**database, autocommit=True) as connection:
        assert connection.execute("SELECT to_regclass('public.template_clone_probe')").fetchone()[0] is None
        connection.execute('CREATE TABLE public.template_clone_probe(value integer NOT NULL)')
        connection.execute('INSERT INTO public.template_clone_probe VALUES (%s)', (independent_test,))
    with psycopg.connect(**database) as second_connection:
        assert second_connection.execute('SELECT value FROM public.template_clone_probe').fetchone()[0] == independent_test


def test_baseline_connections_are_closed_and_refused(database, session_database_templates):
    kwargs = {k: v for k, v in database.items() if k != 'dbname'}
    with session_database_templates.admin(kwargs) as admin:
        templates = tuple(session_database_templates.entries.values())
        assert templates
        for name in templates:
            assert admin.execute('SELECT datallowconn,datistemplate FROM pg_database WHERE datname=%s', (name,)).fetchone() == (False, False)
            assert admin.execute('SELECT count(*) FROM pg_stat_activity WHERE datname=%s', (name,)).fetchone()[0] == 0
            with pytest.raises(psycopg.OperationalError):
                psycopg.connect(**kwargs, dbname=name, connect_timeout=5)
    # An interrupted test connection cannot touch baseline data; this clone stays writable.
    with psycopg.connect(**database) as connection:
        assert connection.execute('SELECT current_database()').fetchone()[0] == database['dbname']
