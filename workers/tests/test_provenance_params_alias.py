from uuid import uuid4

import pytest
from pydantic import ValidationError

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import ProvenanceSignParams, decode_provenance


@pytest.mark.parametrize('pair', [(0, 1), (0, 2), (1, 2)])
def test_signing_input_roles_cannot_alias(pair):
    refs = [dict(artifact_id=str(uuid4()), sha256='a' * 64) for _ in range(3)]
    refs[pair[1]]['artifact_id'] = refs[pair[0]]['artifact_id']
    raw = canonical_bytes(dict(schema_version=1, export=refs[0], lineage=refs[1],
                               policy=refs[2], signing_policy_id=str(uuid4())))
    with pytest.raises(ValidationError, match='PROVENANCE_PARAMS_ALIAS'):
        decode_provenance(raw, ProvenanceSignParams, 1 << 20)


def test_distinct_signing_roles_may_have_equal_content_hashes():
    refs = [dict(artifact_id=str(uuid4()), sha256='a' * 64) for _ in range(3)]
    raw = canonical_bytes(dict(schema_version=1, export=refs[0], lineage=refs[1],
                               policy=refs[2], signing_policy_id=str(uuid4())))
    result = decode_provenance(raw, ProvenanceSignParams, 1 << 20)
    assert len({result.export.artifact_id, result.lineage.artifact_id, result.policy.artifact_id}) == 3
