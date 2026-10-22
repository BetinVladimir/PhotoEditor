"""Pure fixture-policy tests: no PostgreSQL, services or migrated PASS reuse."""
import importlib.util
from pathlib import Path
import pytest

spec = importlib.util.spec_from_file_location('photo_database_templates', Path(__file__).parent/'integration/database_templates.py')
templates = importlib.util.module_from_spec(spec)
spec.loader.exec_module(templates)


def seed(tmp_path):
    folder = tmp_path/'backend/db/migrations'
    folder.mkdir(parents=True)
    for version in (1, 196, 197, 2570):
        (folder/f'{version:05d}_fixture.sql').write_text(f'SELECT {version};\n-- +goose Down\nSELECT 0;')
    return folder


def test_head_and_scaffold_select_exact_current_up_bytes(tmp_path):
    seed(tmp_path)
    _, full = templates.migration_inventory(tmp_path, None, False)
    _, early = templates.migration_inventory(tmp_path, 197, False)
    _, scaffold = templates.migration_inventory(tmp_path, None, True)
    _, combined = templates.migration_inventory(tmp_path, 1, True)
    assert len(full) == 4 and len(early) == 3 and len(scaffold) == 2 and len(combined) == 1
    assert all(b'goose Down' not in body for _, body in full)


@pytest.mark.parametrize('change', ['bytes', 'added', 'renamed', 'deleted'])
def test_inventory_changes_invalidate_session_instead_of_reusing_baseline(tmp_path, change):
    folder = seed(tmp_path)
    owner = templates.SessionDatabaseTemplates(tmp_path)
    fingerprint, _ = templates.migration_inventory(tmp_path, None, False)
    path = folder/'00196_fixture.sql'
    if change == 'bytes': path.write_text('SELECT 99;')
    elif change == 'added': (folder/'02571_new.sql').write_text('SELECT 1;')
    elif change == 'renamed': path.rename(folder/'00196_renamed.sql')
    else: path.unlink()
    with pytest.raises(templates.TemplateIsolationError, match='source changed'):
        owner.assert_source(None, False, fingerprint)
    assert owner.invalid


def test_templates_refuse_xdist_and_nondedicated_server(tmp_path, monkeypatch):
    monkeypatch.setenv('PYTEST_XDIST_WORKER', 'gw0')
    with pytest.raises(templates.TemplateIsolationError, match='serial'):
        templates.SessionDatabaseTemplates(tmp_path)
    monkeypatch.delenv('PYTEST_XDIST_WORKER')
    owner = templates.SessionDatabaseTemplates(tmp_path)
    with pytest.raises(templates.TemplateIsolationError, match='dedicated'):
        owner.admin({'host': 'production.invalid', 'port': 5432, 'user': 'redacted'})


def test_selected_head_ignores_unselected_future_migrations(tmp_path):
    folder = seed(tmp_path)
    before, _ = templates.migration_inventory(tmp_path, 196, False)
    (folder/'02571_future.sql').write_text('SELECT 1;')
    after, _ = templates.migration_inventory(tmp_path, 196, False)
    assert before == after


def test_change_between_tests_cannot_silently_create_second_current_head_baseline(tmp_path):
    folder = seed(tmp_path)
    owner = templates.SessionDatabaseTemplates(tmp_path)
    (folder/'02571_future.sql').write_text('SELECT 1;')
    current, _ = templates.migration_inventory(tmp_path, None, False)
    with pytest.raises(templates.TemplateIsolationError, match='source changed'):
        owner.assert_source(None, False, current)
    assert owner.invalid and not owner.entries


def test_migration_transaction_and_no_transaction_semantics_are_preserved():
    from contextlib import contextmanager
    events = []
    class Connection:
        @contextmanager
        def transaction(self):
            events.append('begin')
            yield
            events.append('commit')
        def execute(self, query):
            events.append(query)
    templates.apply_inventory(Connection(), [('00001_x.sql', b'SELECT 1;'),
        ('00002_x.sql', b'-- +goose NO TRANSACTION\nSELECT 2;')])
    assert events == ['begin', 'SELECT 1;', 'commit', '-- +goose NO TRANSACTION\nSELECT 2;']


def test_cluster_fingerprint_tracks_arbitrary_login_flags_without_password_hashes():
    class Rows:
        def __init__(self, rows): self.rows = rows
        def fetchall(self): return self.rows
    class Admin:
        def __init__(self, superuser): self.superuser = superuser; self.queries = []
        def execute(self, query):
            self.queries.append(query)
            return Rows([('arbitrary_runtime_login', self.superuser)] if 'FROM pg_roles ORDER' in query else [])
    ordinary, privileged = Admin(False), Admin(True)
    assert templates.role_fingerprint(ordinary) != templates.role_fingerprint(privileged)
    assert all('rolpassword' not in query.lower() for query in ordinary.queries)
    assert all("LIKE 'photo" not in query for query in ordinary.queries)
