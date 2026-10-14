"""Qualification probe request boundaries; SQL integration is tested separately."""
from uuid import uuid4

import pytest

from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('row,expected', [(None, False), ((False,), False),
                                         ((1,), False), ((True,), True)])
def test_readiness_is_authenticated_read_only_probe(row, expected):
    db = TaskDatabase('unused')
    worker = uuid4()
    pins = tuple(letter * 64 for letter in 'abcd')
    calls = []
    def query(sql, args):
        calls.append((sql, args))
        return row
    db._query = query
    assert db.signer_runtime_ready(worker, *pins) is expected
    assert calls == [('SELECT photo.signer_runtime_ready(%s,%s,%s,%s,%s)', (worker, *pins))]


@pytest.mark.parametrize('invalid', ['', 'A'*64, 'x'*64, None, 1])
def test_invalid_installed_pin_never_reaches_database(invalid):
    db = TaskDatabase('unused')
    db._query = lambda *args: pytest.fail('invalid pin reached SQL')
    with pytest.raises(ValueError):
        db.signer_runtime_ready(uuid4(), invalid, 'b'*64, 'c'*64, 'd'*64)
