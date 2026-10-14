from threading import Event
from uuid import uuid4

import pytest

from photo_worker.v2.signing_adopter_runtime import (
    SigningAdopterRuntimeLease, require_signing_adopter_runtime_schema,
)


class RuntimeConnection:
    autocommit = True
    def __init__(self):
        self.calls = []
    def execute(self, query, args=()):
        self.calls.append((query, args))
        return self
    def fetchone(self):
        query = self.calls[-1][0]
        return (True,) if 'announce_v31_signing_adopter_runtime' in query else (None,)


def test_signing_adopter_lease_announces_only_exact_pins_and_withdraws():
    connection, stop = RuntimeConnection(), Event()
    policy = str(uuid4())
    pins = ({'id': policy, 'sha': 'a' * 64},)
    with SigningAdopterRuntimeLease(connection, stop, 'photo', pins) as lease:
        lease.set_ready(False)
        lease.set_ready(True)
    queries = [query for query, _ in connection.calls]
    assert sum('announce_v31_signing_adopter_runtime' in query for query in queries) == 2
    assert sum('withdraw_v31_signing_adopter_runtime' in query for query in queries) == 2
    assert not stop.is_set()


@pytest.mark.parametrize('pins', [(), ({'id': str(uuid4()), 'sha': 'A' * 64},),
    ({'id': str(uuid4()), 'sha': 'a' * 64, 'extra': True},),
    ({'id': '00000000-0000-0000-0000-000000000000', 'sha': 'a' * 64},)])
def test_signing_adopter_lease_rejects_unqualified_pins(pins):
    with pytest.raises(ValueError, match='PINS'):
        SigningAdopterRuntimeLease(RuntimeConnection(), Event(), 'photo', pins)


def test_signing_adopter_runtime_requires_scheduler_role():
    class WrongRole(RuntimeConnection):
        def fetchone(self):
            return ('photo_api',)
    with pytest.raises(ValueError, match='ROLE'):
        require_signing_adopter_runtime_schema(WrongRole())
