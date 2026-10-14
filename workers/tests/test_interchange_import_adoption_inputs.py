"""Authored typed import input inventory checks; execution deferred."""

from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence
from photo_worker.v2.interchange_import_adoption_inputs import read_import_adoption_input_inventory


def fixture():
    tenant, task, result = uuid4(), uuid4(), uuid4()
    first, second = uuid4(), uuid4()
    rows = [(0, first, 'a' * 64, 'Params', 'application/json'),
            (1, second, 'b' * 64, 'EditingRecipeV3', 'application/json')]

    class Connection:
        autocommit = True

        def execute(self, query, args):
            assert 'v30_import_adoption_input_inventory' in query
            assert args == (tenant, task, 1, 7, result)
            return SimpleNamespace(fetchall=lambda: list(rows))

    class Reader:
        def evidence(self, ref):
            return ArtifactEvidence(ref, 'application/json', 256, None, 'input')

    return Connection(), Reader(), rows, (tenant, task, 1, 7, result)


def test_import_input_inventory_rechecks_current_typed_scope():
    connection, reader, rows, scope = fixture()
    inventory = read_import_adoption_input_inventory(connection, reader, *scope)
    assert [entry[1] for entry in inventory.entries] == ['Params', 'EditingRecipeV3']
    inventory.recheck()
    rows[1] = (1, uuid4(), 'b' * 64, 'EditingRecipeV3', 'application/json')
    with pytest.raises(AdoptionInvalid, match='INPUTS_CHANGED'):
        inventory.recheck()


def test_import_input_inventory_rejects_missing_or_duplicate_scope():
    connection, reader, rows, scope = fixture()
    rows.pop()
    with pytest.raises(AdoptionInvalid, match='INPUTS_STALE'):
        read_import_adoption_input_inventory(connection, reader, *scope)
    rows.append(rows[0])
    with pytest.raises(AdoptionInvalid):
        read_import_adoption_input_inventory(connection, reader, *scope)
