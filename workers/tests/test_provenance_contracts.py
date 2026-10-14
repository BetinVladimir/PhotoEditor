from copy import deepcopy
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy, SigningLineage, decode_provenance


def policy_fixture():
    return {'schema_version': 1, 'id': str(uuid4()), 'revision': '1',
            'sdk_lock_sha': 'a' * 64, 'key_handle': 'operator-key',
            'certificate_chain_sha': 'b' * 64, 'timestamp_mode': 'off',
            'timestamp_service_id': None, 'allowed_mime': ['image/jpeg', 'image/png'],
            'trust_bundle_sha': 'c' * 64, 'revocation_mode': 'required'}


@pytest.mark.parametrize('mode', ['valid', 'timestamp', 'missing', 'unknown', 'bool',
                                  'revision', 'zero_id', 'mime', 'key', 'timestamp_missing'])
def test_closed_signing_policy_contract(mode):
    policy = policy_fixture()
    if mode == 'timestamp':
        policy.update(timestamp_mode='required', timestamp_service_id='operator-tsa')
    elif mode == 'missing':
        del policy['revocation_mode']
    elif mode == 'unknown':
        policy['private_key'] = 'client-provided'
    elif mode == 'bool':
        policy['schema_version'] = True
    elif mode == 'revision':
        policy['revision'] = '9223372036854775808'
    elif mode == 'zero_id':
        policy['id'] = '00000000-0000-0000-0000-000000000000'
    elif mode == 'mime':
        policy['allowed_mime'] = ['image/png', 'image/png']
    elif mode == 'key':
        policy['key_handle'] = 'bad\nkey'
    elif mode == 'timestamp_missing':
        policy['timestamp_mode'] = 'required'
    raw = canonical_bytes(policy)
    if mode in ('valid', 'timestamp'):
        assert decode_provenance(raw, SigningPolicy, 1 << 20).id.int != 0
    else:
        with pytest.raises(ValueError):
            decode_provenance(raw, SigningPolicy, 1 << 20)


def test_lineage_preserves_claims_and_requires_explicit_null_fields():
    lineage = {'schema_version': 1, 'source_hashes': ['a' * 64], 'actions': [
        {'operation': 'export.encode', 'version': '1', 'params_sha': 'b' * 64,
         'model_lock_sha': None, 'generated': False, 'review_id': None}]}
    result = decode_provenance(canonical_bytes(lineage), SigningLineage, 8 << 20)
    assert result.actions[0].generated is False
    for field in ('model_lock_sha', 'generated', 'review_id'):
        changed = deepcopy(lineage)
        del changed['actions'][0][field]
        with pytest.raises(ValueError):
            decode_provenance(canonical_bytes(changed), SigningLineage, 8 << 20)
    duplicate = b'{"schema_version":1,"schema_version":1,"source_hashes":[],"actions":[]}'
    with pytest.raises(ValueError):
        decode_provenance(duplicate, SigningLineage, 8 << 20)
