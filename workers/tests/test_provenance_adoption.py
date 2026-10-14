"""Physical closure coordination with explicit native-verifier double."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import ArtifactEvidence, ArtifactRef, AdoptionInvalid
import photo_worker.v2.provenance_adoption as module


def test_signing_result_false_activity_refuses_before_object_read():
    class Reader:
        def evidence(self, _ref):
            raise AssertionError('object evidence read after cancellation')
    with pytest.raises(AdoptionInvalid, match='SIGNING_ADOPTION_INACTIVE'):
        module.verify_signing_result(Reader(), object(), object(), object(), object(),
                                     check_active=lambda: False)


def test_signing_result_requires_process_sdk_before_object_read():
    class Reader:
        def evidence(self, _ref):
            raise AssertionError('uninstalled SDK reached object reader')
    with pytest.raises(AdoptionInvalid, match='SIGNING_ADOPTION_SDK_REQUIRED'):
        module.verify_signing_result(Reader(), object(), object(), object(), object())


@pytest.mark.parametrize('mode', ['valid', 'extra_output', 'binary_corruption', 'wrong_role',
                                 'alias', 'mime', 'evidence_changed', 'native_rejected'])
def test_signing_adoption_requires_exact_physical_closure(tmp_path, monkeypatch, mode):
    bodies, evidence = {}, {}
    def store(body, media, source, role=None):
        value = ArtifactRef(uuid4(), sha256(body).hexdigest())
        bodies[value] = body
        evidence[value] = ArtifactEvidence(value, media, len(body), role, source)
        return value
    def wire(value):
        return {'artifact_id': str(value.artifact_id), 'sha256': value.sha256}
    policy_id = uuid4()
    policy = store(canonical_bytes(dict(schema_version=1, id=str(policy_id), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off')), 'application/json', 'input')
    lineage = store(canonical_bytes(dict(schema_version=1, source_hashes=['d'*64], actions=[])),
                    'application/json', 'input')
    original = store(b'original-fixture', 'image/png', 'input')
    signed = store(b'signed-fixture', 'image/jpeg' if mode == 'mime' else 'image/png',
                   'output', 'manifest' if mode == 'wrong_role' else 'signed_export')
    manifest = store(b'manifest-fixture', 'application/c2pa', 'output', 'manifest')
    if mode == 'alias':
        signed = original
    params = store(canonical_bytes(dict(schema_version=1, export=wire(original), lineage=wire(lineage),
                    policy=wire(policy), signing_policy_id=str(policy_id))), 'application/json', 'input')
    root = store(canonical_bytes(dict(schema_version=1, signed_export=wire(signed), manifest=wire(manifest))),
                 'application/json', 'output', 'document')
    outputs = {root, signed, manifest}
    if mode == 'extra_output':
        outputs.add(store(b'extra', 'application/octet-stream', 'output', 'extra'))
    native_calls = []
    sdk = SimpleNamespace(Reader=lambda *_args: object())
    def verify(source_path, signed_path, mime, source_sha, signed_sha, manifest_bytes,
               manifest_sha, lineage_bytes, policy_bytes, operator, **kwargs):
        native_calls.append(True)
        assert source_path.read_bytes() == bodies[original]
        assert signed_path.read_bytes() == bodies[signed]
        assert manifest_bytes == bodies[manifest]
        assert lineage_bytes == bodies[lineage] and policy_bytes == bodies[policy]
        assert kwargs['sdk'] is sdk
        if mode == 'native_rejected':
            raise ValueError('untrusted')
        if mode == 'evidence_changed':
            item = evidence[signed]
            evidence[signed] = ArtifactEvidence(item.ref, item.media_type, item.bytes+1, item.role, item.source)
        return 'f'*64
    monkeypatch.setattr(module, 'verify_signed_files', verify)
    class Reader:
        def evidence(self, value): return evidence[value]
        def read_json(self, value, maximum): return bodies[value]
        def staged_outputs(self): return set(outputs)
        def iter_bytes(self, value, maximum):
            yield b'corrupted' if mode == 'binary_corruption' and value == signed else bodies[value]
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2 << 20, min_free_bytes=0) as scratch:
        if mode == 'valid':
            result = module.verify_signing_result(Reader(), params, root, scratch, object(), sdk=sdk)
            assert result.validation_sha == 'f'*64
            assert result.bytes_written == sum(len(bodies[value]) for value in outputs)
            assert result.params_jcs == bodies[params] and result.result_jcs == bodies[root]
        else:
            with pytest.raises(AdoptionInvalid):
                module.verify_signing_result(Reader(), params, root, scratch, object(), sdk=sdk)
    assert len(native_calls) == (1 if mode in ('valid', 'evidence_changed', 'native_rejected') else 0)
