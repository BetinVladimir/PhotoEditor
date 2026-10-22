"""Shared worker/API/scheduler connection attestation; authored, not executed."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.application_role import require_application_role


@pytest.mark.parametrize('role', ['photo_worker', 'photo_scheduler', 'photo_api'])
@pytest.mark.parametrize('reply', [None, (), (False,), (None,), (1,), ('true',), (True, False), (True,)])
def test_runtime_role_requires_exact_boolean_evidence(role, reply):
    queries = []
    def execute(query):
        queries.append(query)
        return SimpleNamespace(fetchone=lambda: reply)
    conn = SimpleNamespace(execute=execute)
    if reply == (True,) and type(reply[0]) is bool:
        require_application_role(conn, role)
    else:
        with pytest.raises(ValueError, match='APPLICATION_DATABASE_ROLE_ISOLATION_INVALID'):
            require_application_role(conn, role)
    assert len(queries) == 1
    assert "current_user='" + role + "'" in queries[0]
    assert "pg_has_role(session_user,privileged.oid,'MEMBER')" in queries[0]
    assert "has_schema_privilege(session_user,'photo','CREATE')" in queries[0]
    assert 'login.rolsuper' in queries[0]
    assert 'login.rolbypassrls' in queries[0]
    assert 'n.nspowner=privileged.oid' in queries[0]
    assert 'c.relowner=privileged.oid' in queries[0]
    assert 'p.prosecdef AND p.proowner=privileged.oid' in queries[0]


@pytest.mark.parametrize('role', ['postgres', 'photo_fn_owner', 'photo_runtime_owner', "photo_api' OR true--", '', None])
def test_unsupported_role_never_executes_sql(role):
    def execute(query):
        pytest.fail('invalid role reached database')
    with pytest.raises(ValueError, match='APPLICATION_DATABASE_ROLE_INVALID'):
        require_application_role(SimpleNamespace(execute=execute), role)
