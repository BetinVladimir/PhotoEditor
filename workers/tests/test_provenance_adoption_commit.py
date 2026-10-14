from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, PreparedAdoption
from photo_worker.v2.provenance_adoption import PreparedSigningAdoption, VerifiedSigningResult
from photo_worker.v2.provenance_adoption_commit import signing_commit_statement
from photo_worker.contracts.base import canonical_bytes


def test_commit_binds_verified_bytes_and_exact_fence():
    tenant, task, result = uuid4(), uuid4(), uuid4()
    context = SimpleNamespace(operation='provenance.sign', version='1', attempt=2, fence=9, result_id=result)
    inventory = tuple({'ordinal': ordinal, 'artifact_id': str(uuid4()),
        'sha256': 'a'*64, 'purpose': 'b'*64} for ordinal in range(3))
    verified = VerifiedSigningResult(b'params', b'result', b'policy', b'lineage', 'c'*64, 42)
    digest = sha256(canonical_bytes(list(inventory))).hexdigest()
    prepared = PreparedSigningAdoption(verified, PreparedAdoption(inventory, digest))
    sql, args = signing_commit_statement(tenant, task, context, prepared)
    assert sql == 'SELECT photo.adopt_v31_signed(' + ','.join(['%s']*12) + ')'
    assert args[:5] == (tenant, task, 2, 9, result)
    assert args[5].obj == list(inventory)
    assert args[6:] == (digest, 'params', 'result', 'policy', 'lineage', 'c'*64)
    inventory[0]['sha256'] = 'f'*64
    assert args[5].obj[0]['sha256'] == 'a'*64
    with pytest.raises(AdoptionInvalid, match='SIGNING_COMMIT_INVENTORY_CHANGED'):
        signing_commit_statement(tenant, task, context, prepared)
    for key, value in [('operation', 'metadata.write'), ('version', '2'), ('attempt', True),
                       ('attempt', 0), ('fence', True), ('fence', 0), ('result_id', None)]:
        changed = SimpleNamespace(**{**vars(context), key: value})
        with pytest.raises(AdoptionInvalid):
            signing_commit_statement(tenant, task, changed, prepared)
