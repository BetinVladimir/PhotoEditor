"""Independent authority and live-key challenge orchestration; no qualification."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.key_inspector import KeyBoundInstallationInspector


def policy():
    return SigningPolicy.model_validate(dict(schema_version=1,id=str(uuid4()),revision='1',
        sdk_lock_sha='a'*64,key_handle='key',certificate_chain_sha='b'*64,
        timestamp_mode='off',timestamp_service_id=None,allowed_mime=['image/png'],
        trust_bundle_sha='c'*64,revocation_mode='off'))


@pytest.mark.parametrize('mode',['valid','native_denied','key_denied','revoked','truthy'])
def test_both_authorities_required_before_and_after_challenge(mode):
    candidate=policy()
    raw=canonical_bytes(candidate)
    calls=[]
    def native(copy,algorithm,cert):
        calls.append('native')
        assert canonical_bytes(copy) == raw
        copy.key_handle='mutated-copy'
        if mode == 'native_denied' or mode == 'revoked' and len(calls)>1:
            return False
        return 1 if mode == 'truthy' else True
    def key(handle,algorithm,cert):
        calls.append('key')
        assert (handle,algorithm,cert) == ('key','PS256',b'cert')
        return mode != 'key_denied'
    inspector=KeyBoundInstallationInspector(SimpleNamespace(check_available=key),native)
    assert inspector(candidate,'PS256',b'cert') is (mode == 'valid')
    assert canonical_bytes(candidate) == raw
    assert calls == (['native'] if mode in ('native_denied','truthy') else
                     ['native','key'] if mode == 'key_denied' else ['native','key','native'])
