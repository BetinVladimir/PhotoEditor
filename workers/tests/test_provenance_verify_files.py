"""Physical file binding with a fake Reader; not real C2PA acceptance evidence."""
from contextlib import nullcontext
from hashlib import sha256
from struct import pack
from zlib import crc32
from types import SimpleNamespace
from uuid import uuid4

from PIL import Image
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy, SigningLineage
from photo_worker.operations.provenance.verify_files import verify_signed_files


def test_verifier_refuses_uninstalled_sdk_before_file_or_operator_io(tmp_path):
    class Operator:
        def check_installation(self, _policy):
            pytest.fail('uninstalled SDK reached native operator')
    source, signed = tmp_path / 'missing-source.png', tmp_path / 'missing-signed.png'
    args = (source, signed, 'image/png', 'a' * 64, 'b' * 64,
        b'manifest', sha256(b'manifest').hexdigest(), b'{}', b'{}', Operator())
    with pytest.raises(ValueError, match='SIGNER_VERIFICATION_INPUT'):
        verify_signed_files(*args, sdk=None)
    with pytest.raises(ValueError, match='SIGNER_VERIFICATION_INPUT'):
        verify_signed_files(*args, sdk=SimpleNamespace(Reader=object()))


@pytest.mark.parametrize('mode', ['valid', 'hash', 'pixels', 'manifest', 'trust', 'mutation',
                                  'symlink', 'missing_store', 'source_swap', 'signed_swap', 'inactive_false'])
def test_independent_file_verification(tmp_path, mode):
    source, signed = tmp_path/'source.png', tmp_path/'signed.png'
    with Image.new('RGB', (4, 4), (10, 20, 30)) as image:
        image.save(source)
        if mode == 'pixels':
            image.putpixel((0, 0), (40, 20, 30))
        image.save(signed)
    if mode != 'missing_store':
        raw = signed.read_bytes()
        store = b'manifest'
        signed.write_bytes(raw[:-12] + pack('>I', len(store)) + b'caBX' + store
            + pack('>I', crc32(b'caBX' + store) & 0xffffffff) + raw[-12:])
    if mode == 'symlink':
        signed.unlink()
        signed.symlink_to(source)
    lineage_raw = canonical_bytes(dict(schema_version=1, source_hashes=['e'*64], actions=[]))
    policy_raw = canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off'))
    class Reader:
        def __init__(self, *args, **kwargs): pass
        def __enter__(self): return self
        def __exit__(self, *args): pass
        def is_embedded(self): return True
        def get_validation_state(self): return 'Untrusted' if mode == 'trust' else 'Trusted'
        def get_validation_results(self):
            return {'activeManifest': {'failure': [], 'informational': [], 'success': [
                {'code': code} for code in ('claimSignature.validated', 'claimSignature.insideValidity',
                    'signingCredential.trusted', 'assertion.dataHash.match')]}}
        def get_active_manifest(self):
            return {'format': 'image/png', 'assertions': [
                {'label': 'org.photoeditor.lineage', 'data': {'schema_version': 1, 'source_hashes': ['e'*64], 'actions': []}}]}
    installations=[]
    def installation(policy):
        installations.append(True)
        assert isinstance(policy, SigningPolicy)
    def certificate(reader, policy, lineage, manifest):
        assert isinstance(policy, SigningPolicy) and isinstance(lineage, SigningLineage)
        assert manifest == b'manifest'
        if mode == 'mutation':
            signed.write_bytes(b'changed-during-verification')
        if mode in ('source_swap', 'signed_swap'):
            target = source if mode == 'source_swap' else signed
            retained = target.read_bytes()
            target.rename(tmp_path/'old-inode')
            target.write_bytes(retained)
    operator = SimpleNamespace(check_installation=installation, open_context=lambda _: nullcontext(object()),
                               verify_reader=certificate)
    args = (source, signed, 'image/png', sha256(source.read_bytes()).hexdigest(),
        '0'*64 if mode == 'hash' else sha256(signed.read_bytes()).hexdigest(), b'manifest',
        '0'*64 if mode == 'manifest' else sha256(b'manifest').hexdigest(), lineage_raw, policy_raw, operator)
    before = source.read_bytes()
    activity_calls=0
    def active():
        nonlocal activity_calls
        activity_calls+=1
        if mode=='inactive_false' and activity_calls>=2:
            return False
    if mode == 'valid':
        result = verify_signed_files(*args, sdk=SimpleNamespace(Reader=Reader))
        assert len(result) == 64
    else:
        with pytest.raises((ValueError, OSError, RuntimeError)):
            verify_signed_files(*args, sdk=SimpleNamespace(Reader=Reader),check_active=active)
    if mode=='inactive_false':assert not installations
    assert source.read_bytes() == before
