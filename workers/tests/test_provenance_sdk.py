"""Bridge unit tests only: fake SDK is not C2PA qualification evidence."""
from contextlib import nullcontext
from hashlib import sha256
from io import BytesIO
from struct import pack
from zlib import crc32
from types import SimpleNamespace
from uuid import uuid4

from PIL import Image
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.provenance.sdk import sign_staged, _file_hash


class Operator:
    def __init__(self, fail=False):
        self.checks = 0
        self.fail = fail

    def check_installation(self, policy):
        self.checks += 1

    def open_context(self, policy):
        return nullcontext(object())

    def open_signer(self, policy):
        return nullcontext(object())

    def verify_reader(self, reader, policy, lineage, manifest):
        assert reader.mime == 'image/png'
        if self.fail:
            raise ValueError('untrusted certificate')


def test_hash_reader_refuses_boolean_denial_before_and_after_file_io():
    class Reader(BytesIO):
        def read(self, size=-1):
            result=super().read(size)
            self.live=False
            return result
    reader=Reader(b'input');reader.live=False
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        _file_hash(reader,64,lambda:reader.live)
    assert reader.tell()==0
    reader.live=True
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        _file_hash(reader,64,lambda:reader.live)


class Builder:
    change = False
    oversize = False
    missing_store = False

    def __init__(self, definition, context):
        assert definition['assertions'][0]['label'] == 'org.photoeditor.lineage'
        Reader.lineage = definition['assertions'][0]['data']

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def sign(self, signer, mime, source, dest):
        if self.oversize:
            dest.seek(512 << 20)
            dest.write(b'x')
        if self.change:
            with Image.open(source) as image:
                image.load()
                image.putpixel((0, 0), (255, 0, 0))
                image.save(dest, format='PNG')
        else:
            dest.write(source.read())
        manifest = b'fake-binary-manifest-unit-test-only'
        if not self.missing_store:
            dest.seek(-12, 2)
            end = dest.read(12)
            dest.seek(-12, 2)
            dest.write(pack('>I', len(manifest)) + b'caBX' + manifest
                       + pack('>I', crc32(b'caBX' + manifest) & 0xffffffff) + end)
        return manifest


class Reader:
    lineage = None

    def is_embedded(self):
        return True

    def get_validation_state(self):
        return 'Trusted'

    def get_validation_results(self):
        return {'activeManifest': {'failure': [], 'informational': [], 'success': [
            {'code': code} for code in ('claimSignature.validated',
                'claimSignature.insideValidity', 'signingCredential.trusted',
                'assertion.dataHash.match', 'signingCredential.ocsp.notRevoked')]}}

    def get_active_manifest(self):
        return {'format': self.mime, 'assertions': [
            {'label': 'org.photoeditor.lineage', 'data': self.lineage}]}

    def __init__(self, mime, stream, context):
        self.mime = mime

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False


@pytest.mark.parametrize('mode', ['valid', 'pixels', 'trust', 'hash', 'exists', 'oversize', 'missing_store',
                                  'cancelled', 'cancelled_false', 'cancel_source_open',
                                  'cancel_output_open', 'substituted', 'source_substituted'])
def test_signer_bridge_rejects_changed_or_untrusted_output(tmp_path, mode, monkeypatch):
    source, output = tmp_path / 'source.png', tmp_path / 'signed.png'
    with Image.new('RGB', (8, 8), (20, 30, 40)) as image:
        image.save(source, format='PNG')
    digest = sha256(source.read_bytes()).hexdigest()
    if mode == 'hash':
        digest = '0' * 64
    if mode == 'exists':
        output.write_bytes(b'existing bytes')
    monkeypatch.setattr(Builder, 'change', mode == 'pixels')
    monkeypatch.setattr(Builder, 'oversize', mode == 'oversize')
    monkeypatch.setattr(Builder, 'missing_store', mode == 'missing_store')
    cancelled = False
    if mode in ('cancelled', 'cancelled_false'):
        original_sign = Builder.sign
        def cancel_during_sdk(self, *args):
            nonlocal cancelled
            cancelled = True
            return original_sign(self, *args)
        monkeypatch.setattr(Builder, 'sign', cancel_during_sdk)
    if mode in ('cancel_source_open', 'cancel_output_open'):
        import importlib
        import os
        bridge = importlib.import_module('photo_worker.operations.provenance.sdk')
        original_open = os.open
        opened = []
        def cancel_after_open(path, *args, **kwargs):
            nonlocal cancelled
            descriptor = original_open(path, *args, **kwargs)
            if str(path) == str(source if mode == 'cancel_source_open' else output):
                opened.append(descriptor)
                cancelled = True
            return descriptor
        monkeypatch.setattr(bridge.os, 'open', cancel_after_open)
    def active():
        if cancelled:
            if mode in ('cancelled_false', 'cancel_source_open', 'cancel_output_open'):
                return False
            from photo_worker.runtime.executor import TaskCancelled
            raise TaskCancelled('cancelled during SDK')
    operator = Operator(fail=mode == 'trust')
    if mode in ('substituted', 'source_substituted'):
        original_check = operator.check_installation
        def replace_staged_file(policy):
            original_check(policy)
            if operator.checks == 2:
                if mode == 'substituted':
                    output.rename(tmp_path/'original-signed.png')
                    output.write_bytes(b'foreign staged bytes')
                else:
                    source.rename(tmp_path/'original-source.png')
                    source.write_bytes(b'foreign source bytes')
        operator.check_installation = replace_staged_file
    invoke = lambda: sign_staged(str(source), str(output), 'image/png', digest,
                                canonical_bytes({'schema_version': 1, 'source_hashes': [digest], 'actions': []}),
                                canonical_bytes({'schema_version': 1, 'id': str(uuid4()),
                                    'revision': '1', 'sdk_lock_sha': 'a' * 64,
                                    'key_handle': 'operator-key', 'certificate_chain_sha': 'a' * 64,
                                    'timestamp_mode': 'off', 'timestamp_service_id': None,
                                    'allowed_mime': ['image/png'], 'trust_bundle_sha': 'a' * 64,
                                    'revocation_mode': 'required'}), operator,
                                sdk=SimpleNamespace(Builder=Builder, Reader=Reader), check_active=active)
    if mode == 'valid':
        result = invoke()
        assert result['signed_sha256'] == sha256(output.read_bytes()).hexdigest()
        assert result['signed_sha256'] != digest
        assert result['manifest'] != canonical_bytes({'schema_version': 1})
        assert operator.checks == 2
    else:
        with pytest.raises((ValueError, FileExistsError, RuntimeError)):
            invoke()
        if mode in ('cancel_source_open', 'cancel_output_open'):
            assert len(opened) == 1
            with pytest.raises(OSError):
                os.fstat(opened[0])
        if mode == 'exists':
            assert output.read_bytes() == b'existing bytes'
        elif mode == 'substituted':
            assert output.read_bytes() == b'foreign staged bytes'
        elif mode == 'source_substituted':
            assert source.read_bytes() == b'foreign source bytes'
            assert not output.exists()
        else:
            assert not output.exists()
