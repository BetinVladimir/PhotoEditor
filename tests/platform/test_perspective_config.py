from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.perspective_config import load_perspective_options,perspective_installation_from_environment,MAX_CONFIG_BYTES
from photo_worker.runtime.service import WorkerConfigurationError


def test_disabled_configuration_needs_no_objects_or_runtime():
    assert perspective_installation_from_environment(SimpleNamespace(),lambda name,default='':default) is None


@pytest.mark.parametrize('value',['1','yes','TRUE',''])
def test_enablement_requires_explicit_boolean_string(value):
    with pytest.raises(WorkerConfigurationError):
        perspective_installation_from_environment(SimpleNamespace(),lambda name,default='':value if name=='PHOTO_PERSPECTIVE_ENABLED' else default)


def test_symlink_and_oversized_file_refused(tmp_path):
    original=tmp_path/'config.json';original.write_bytes(b'{}');link=tmp_path/'link.json';link.symlink_to(original)
    with pytest.raises(OSError):load_perspective_options(link,sha256(b'{}').hexdigest())
    with original.open('wb') as stream:stream.truncate(MAX_CONFIG_BYTES+1)
    with pytest.raises(ValueError,match='FILE_INVALID'):load_perspective_options(original,'a'*64)


def test_sha_and_unknown_keys_refused_before_runtime(tmp_path):
    path=tmp_path/'config.json';raw=canonical_bytes({'schema_version':1,'extra':True});path.write_bytes(raw)
    with pytest.raises(ValueError,match='BYTES_MISMATCH'):load_perspective_options(path,'a'*64)
    with pytest.raises(ValueError,match='CONFIG_INVALID'):load_perspective_options(path,sha256(raw).hexdigest())


def test_cpu_requirement_precedes_configuration_read():
    config=SimpleNamespace(resource_class='gpu',backend='metal',icc_root=None)
    with pytest.raises(WorkerConfigurationError,match='CPU'):
        perspective_installation_from_environment(config,lambda name,default='':'true' if name=='PHOTO_PERSPECTIVE_ENABLED' else default)
