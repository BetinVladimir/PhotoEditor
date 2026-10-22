"""Result installer keeps qualification and SQL admission in the owned scope."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2 import shown_background_cleanup_installation as module


@pytest.mark.parametrize('qualified', [True, False, None])
def test_shown_cleanup_installer_qualification_precedes_sql_admission(monkeypatch, qualified):
    tenant, task = uuid4(), uuid4()
    context = SimpleNamespace(attempt=1, fence=2, result_id=uuid4())
    ref = ArtifactRef(uuid4(), 'a' * 64)
    events = []
    reader = object()
    def sql_install(connection, supplied_tenant, supplied_task, owner, supplied_reader, *, check_active):
        assert (supplied_tenant, supplied_task, supplied_reader) == (tenant, task, reader)
        assert owner == context and owner is not context
        def sql_admission(raw):
            assert owner.attempt == 1 and raw == b'owned'
            events.append('sql')
            return True
        return ref, sql_admission
    monkeypatch.setattr(module, 'sql_shown_background_cleanup_adoption_admission', sql_install)
    def qualification(supplied_tenant, supplied_task, owner, supplied_reader, raw):
        assert (supplied_tenant, supplied_task, supplied_reader, raw) == (tenant, task, reader, b'owned')
        assert owner.attempt == 1
        owner.attempt = 99
        events.append('qualification')
        return qualified
    install = module.sql_shown_background_cleanup_adoption_installation(SimpleNamespace(autocommit=True),
        verify_qualification=qualification, check_active=lambda: True)
    result = install(tenant, task, context, reader)
    context.attempt = 44
    assert result.params == ref
    assert result.verify_admission(b'owned') is (qualified is True)
    assert events == (['qualification', 'sql'] if qualified is True else ['qualification'])


@pytest.mark.parametrize('qualified', [True, False])
def test_shown_cleanup_installer_cancellation_after_qualification_stops_sql(monkeypatch, qualified):
    active = True
    def sql_install(*args, **kwargs):
        def forbidden_sql(raw):
            raise AssertionError('SQL admission after cancellation')
        return ArtifactRef(uuid4(), 'a' * 64), forbidden_sql
    monkeypatch.setattr(module, 'sql_shown_background_cleanup_adoption_admission', sql_install)
    def qualification(*args):
        nonlocal active
        active = False
        return qualified
    install = module.sql_shown_background_cleanup_adoption_installation(SimpleNamespace(autocommit=True),
        verify_qualification=qualification, check_active=lambda: active)
    result = install(uuid4(), uuid4(), SimpleNamespace(attempt=1), object())
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        result.verify_admission(b'owned')
