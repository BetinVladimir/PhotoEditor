"""Authored startup/SQL authority scenarios; no database connection required."""
from types import SimpleNamespace

import pytest

from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.service import WorkerConfigurationError, _require_removal_sql_authority


@pytest.mark.parametrize('method,total', [
    ('removal_preview_runtime_ready', 3), ('shown_removal_runtime_ready', 2),
])
def test_removal_requires_every_sql_gate(method, total):
    database = TaskDatabase('not-connected')
    calls = []
    def query(sql, args):
        calls.append(args[0])
        return ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, False, False)
    database._query = query
    assert getattr(database, method)() is True
    assert len(calls) == total and len(set(calls)) == total
    expected = {
        'removal_preview_runtime_ready': (
            'photo.admit_semantic_removal_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_removal_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.semantic_removal_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
        ),
        'shown_removal_runtime_ready': (
            'photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
            'photo.v27_shown_removal_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
        ),
    }
    assert tuple(calls) == expected[method]


@pytest.mark.parametrize('method,total', [
    ('removal_preview_runtime_ready', 3), ('shown_removal_runtime_ready', 2),
])
@pytest.mark.parametrize('bad_row', [
    None,
    ('photo_fn_owner', True),
    ('photo_api', True, 'search_path=pg_catalog, photo', True, False, False),
    ('photo_fn_owner', False, 'search_path=pg_catalog, photo', True, False, False),
    ('photo_fn_owner', True, '', True, False, False),
    ('photo_fn_owner', True, None, True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, public, photo', True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photo, public', True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog_fake, photo', True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photograph', True, False, False),
    ('photo_fn_owner', True, 'search_path=photo, pg_catalog', True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photo, pg_temp', True, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photo', False, False, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, True, False),
    ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, False, True),
])
def test_removal_refuses_missing_or_unsafe_gate_at_every_position(method, total, bad_row):
    for failed_position in range(total):
        database = TaskDatabase('not-connected')
        calls = []
        def query(sql, args):
            calls.append(args[0])
            if len(calls) - 1 == failed_position:
                return bad_row
            return ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, False, False)
        database._query = query
        assert getattr(database, method)() is False
        assert len(calls) == failed_position + 1


@pytest.mark.parametrize('preview,shown', [(False, False), (True, False), (False, True), (True, True)])
def test_startup_checks_only_enabled_installations(preview, shown):
    calls = []
    def ready(name):
        calls.append(name)
        return True
    database = SimpleNamespace(removal_preview_runtime_ready=lambda: ready('preview'),
                               shown_removal_runtime_ready=lambda: ready('shown'))
    _require_removal_sql_authority(database, preview_enabled=preview, shown_enabled=shown)
    assert calls == (['preview'] if preview else []) + (['shown'] if shown else [])


@pytest.mark.parametrize('gate', ['preview', 'shown'])
@pytest.mark.parametrize('result', [False, None, 1, 'true'])
def test_startup_refuses_non_true_sql_authority(gate, result):
    database = SimpleNamespace(removal_preview_runtime_ready=lambda: result,
                               shown_removal_runtime_ready=lambda: result)
    with pytest.raises(WorkerConfigurationError, match='SQL authority is not installed'):
        _require_removal_sql_authority(database, preview_enabled=gate == 'preview',
                                      shown_enabled=gate == 'shown')


def test_startup_propagates_database_failure_instead_of_registering():
    def unavailable():
        raise OSError('database unavailable')
    database = SimpleNamespace(removal_preview_runtime_ready=unavailable)
    with pytest.raises(OSError, match='database unavailable'):
        _require_removal_sql_authority(database, preview_enabled=True, shown_enabled=False)
