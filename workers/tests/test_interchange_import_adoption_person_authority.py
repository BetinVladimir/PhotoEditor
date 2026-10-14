"""Imported person evidence stays bound to the current scheduler grant."""

from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adoption_person_authority import (
    SQLImportAdoptionPersonAuthority)
from test_interchange_person_mask_confirmation import _mask_and_binding


def test_import_person_authority_reads_exact_current_scope():
    calls = []
    class Connection:
        autocommit = True
        def execute(self, sql, params):
            calls.append((sql, params))
            return SimpleNamespace(fetchone=lambda: (7,))
    scope = (uuid4(), uuid4(), 1, 3, uuid4())
    context = SimpleNamespace(attempt=1, fence=3, result_id=scope[4])
    mask, binding = _mask_and_binding()
    authority = SQLImportAdoptionPersonAuthority(Connection(), lambda: None)
    assert authority(scope, context, mask, binding) == 7
    assert calls[0][1] == scope + (binding.asset_version_id,
        mask.artifact.artifact_id, mask.artifact.sha256)
    with pytest.raises(AdoptionInvalid, match='SCOPE'):
        authority(scope, SimpleNamespace(attempt=1, fence=4, result_id=scope[4]),
                  mask, binding)
    assert len(calls) == 1


def test_import_person_authority_rejects_missing_current_grant():
    connection = SimpleNamespace(autocommit=True,
        execute=lambda *_args: SimpleNamespace(fetchone=lambda: (None,)))
    authority = SQLImportAdoptionPersonAuthority(connection, lambda: None)
    scope = (uuid4(), uuid4(), 1, 3, uuid4())
    context = SimpleNamespace(attempt=1, fence=3, result_id=scope[4])
    mask, binding = _mask_and_binding()
    with pytest.raises(AdoptionInvalid, match='UNAVAILABLE'):
        authority(scope, context, mask, binding)
