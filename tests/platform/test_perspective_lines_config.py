from hashlib import sha256
from types import SimpleNamespace

import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import perspective_lines_config as module
from photo_worker.runtime.service import WorkerConfigurationError


def test_disabled_does_not_read_config_or_runtime():
    assert module.perspective_lines_installation_from_environment(SimpleNamespace(),lambda k,d='':d) is None


@pytest.mark.parametrize('enabled',['yes','TRUE','1',''])
def test_invalid_enablement_refused(enabled):
    with pytest.raises(WorkerConfigurationError):
        module.perspective_lines_installation_from_environment(SimpleNamespace(),lambda k,d='':enabled)


def test_cpu_requirement_precedes_file_io():
    with pytest.raises(WorkerConfigurationError,match='CPU'):
        module.perspective_lines_installation_from_environment(
            SimpleNamespace(resource_class='gpu',backend='metal',icc_root=None),
            lambda k,d='':'true' if k=='PHOTO_PERSPECTIVE_LINES_ENABLED' else d)


def test_file_identity_size_and_digest_gates(tmp_path):
    path=tmp_path/'config.json';path.write_bytes(b'{}')
    link=tmp_path/'link.json';link.symlink_to(path)
    with pytest.raises(OSError):module.load_perspective_lines_installation(link,sha256(b'{}').hexdigest())
    with pytest.raises(ValueError,match='BYTES_MISMATCH'):module.load_perspective_lines_installation(path,'a'*64)
    alias=tmp_path/'hard.json';alias.hardlink_to(path)
    with pytest.raises(ValueError,match='FILE_INVALID'):module.load_perspective_lines_installation(path,sha256(b'{}').hexdigest())
    alias.unlink()
    with path.open('wb') as stream:stream.truncate(module.MAX_CONFIG_BYTES+1)
    with pytest.raises(ValueError,match='FILE_INVALID'):module.load_perspective_lines_installation(path,'a'*64)


def test_exact_config_owned_and_runtime_verified(monkeypatch,tmp_path):
    checks=[]
    class Lock:
        @staticmethod
        def model_validate_json(raw):return bytes(raw)
    monkeypatch.setattr(module,'PerspectiveLinesRuntimeLock',Lock)
    monkeypatch.setattr(module,'verify_installed_runtime',lambda lock,digest:checks.append((lock,digest)))
    raw=canonical_bytes(dict(schema_version=1,runtime_lock={'schema_version':1},runtime_sha='a'*64,working_icc_sha='b'*64))
    path=tmp_path/'config.json';path.write_bytes(raw)
    installation=module.load_perspective_lines_installation(path,sha256(raw).hexdigest())
    assert installation.runtime_sha=='a'*64 and installation.working_icc_sha=='b'*64
    assert checks==[(canonical_bytes({'schema_version':1}),'a'*64)]
    for changed in [raw+b' ',canonical_bytes({'schema_version':True}),canonical_bytes({'schema_version':1,'extra':True})]:
        path.write_bytes(changed)
        with pytest.raises(ValueError,match='CONFIG_INVALID'):
            module.load_perspective_lines_installation(path,sha256(changed).hexdigest())
    assert len(checks)==1
