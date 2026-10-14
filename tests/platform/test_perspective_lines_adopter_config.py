from types import SimpleNamespace
import pytest
from photo_worker.v2 import perspective_lines_adopter_config as module


def test_disabled_needs_no_directories_or_runtime():
    assert module.perspective_lines_adopter_from_environment(lambda k,d='':d) is None


@pytest.mark.parametrize('value',['TRUE','yes','1',''])
def test_invalid_enablement_refused(value):
    with pytest.raises(ValueError):module.perspective_lines_adopter_from_environment(lambda k,d='':value)


def test_enabled_config_binds_runtime_icc_and_resource_limits(monkeypatch,tmp_path):
    env=dict(PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED='true',
        PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES='536870912',
        PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES='1073741824',
        PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT=str(tmp_path),PHOTO_ICC_ROOT=str(tmp_path),
        PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG='/operator/runtime.json',
        PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA='a'*64)
    calls=[]
    runtime=SimpleNamespace(working_icc_sha='b'*64)
    monkeypatch.setattr(module,'load_perspective_lines_installation',lambda path,digest:calls.append((path,digest)) or runtime)
    monkeypatch.setattr(module,'ICCRegistry',lambda root:SimpleNamespace(resolve=lambda profile:SimpleNamespace(sha256='b'*64)))
    monkeypatch.setattr(module,'validate_perspective_lines_adoption_installation',lambda item:item)
    result=module.perspective_lines_adopter_from_environment(env.get)
    assert result.runtime is runtime and result.max_bytes==536870912 and result.ram_max_bytes==1073741824
    assert calls==[('/operator/runtime.json','a'*64)]
    for value in ('0','01','-1','1.5','9007199254740992'):
        env['PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES']=value
        with pytest.raises(ValueError,match='resource limit'):module.perspective_lines_adopter_from_environment(env.get)
    assert len(calls)==1
