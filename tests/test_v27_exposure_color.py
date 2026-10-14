from pathlib import Path
import json
import shutil
import pytest
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.semantic.exposure_color import local_exposure_icc_verifier, WORKING_PROFILE_SHA
from photo_worker.operations.semantic.exposure_adapter import locked_local_exposure_installation

ROOT=Path(__file__).resolve().parents[1]/'resources/icc'


def test_exposure_color_exact_runtime_and_metadata():
    verify=local_exposure_icc_verifier(ICCRegistry(ROOT))
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256=WORKING_PROFILE_SHA,encoding='gamma24')
    assert verify(color) is True
    assert verify(color.model_copy(update={'icc_sha256':'0'*64})) is False
    assert verify(dict(profile_id='linear_srgb_v1',icc_sha256=WORKING_PROFILE_SHA,encoding='linear')) is False
    assert verify(dict(profile_id='work_rec2020_gamma24_v1',icc_sha256=WORKING_PROFILE_SHA,encoding='linear')) is False
    assert verify({'unexpected':True}) is False
    with pytest.raises(ValueError,match='ICC_UNQUALIFIED'):local_exposure_icc_verifier(None)


@pytest.mark.parametrize('change',['bytes','lock','missing','substitute'])
def test_exposure_color_rechecks_runtime_profile(tmp_path,change):
    root=tmp_path/'icc';shutil.copytree(ROOT,root)
    verify=local_exposure_icc_verifier(ICCRegistry(root))
    profile=root/'work_rec2020_gamma24_v1.icc'
    if change=='substitute':
        from hashlib import sha256
        raw=(root/'srgb_v1.icc').read_bytes();profile.write_bytes(raw)
        lock=json.loads((root/'profiles.lock.json').read_text());lock['profiles'][0].update(sha256=sha256(raw).hexdigest(),bytes=len(raw))
        (root/'profiles.lock.json').write_text(json.dumps(lock))
        # Generic registry accepts these internally consistent RGB ICC bytes;
        # exposure must still require the original versioned working profile.
        changed=ICCRegistry(root);assert changed.resolve('work_rec2020_gamma24_v1').data==raw
        with pytest.raises(ValueError,match='ICC_UNQUALIFIED'):local_exposure_icc_verifier(changed)
        return
    if change=='bytes':
        raw=bytearray(profile.read_bytes());raw[-1]^=1;profile.write_bytes(raw)
    elif change=='missing':profile.unlink()
    else:
        lock=json.loads((root/'profiles.lock.json').read_text())
        lock['profiles'][0]['sha256']='0'*64;(root/'profiles.lock.json').write_text(json.dumps(lock))
        # A newly loaded registry must not trust an altered checksum.
        with pytest.raises(ValueError,match='ICC_UNQUALIFIED'):local_exposure_icc_verifier(ICCRegistry(root))
        return
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256=WORKING_PROFILE_SHA,encoding='gamma24')
    with pytest.raises(ValueError,match='ICC_UNQUALIFIED'):verify(color)


def test_locked_installation_keeps_sql_and_runtime_mandatory():
    class Database:
        def admit_semantic_local_exposure(self,*args):return False
        def local_exposure_protection_origin(self,*args):return False
    installation=locked_local_exposure_installation(database=Database(),resolve_protection=lambda edit:None,
        verify_runtime=lambda edit,profile:False,icc_registry=ICCRegistry(ROOT))
    assert installation.verify_runtime(None,None) is False
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        locked_local_exposure_installation(database=object(),resolve_protection=lambda edit:None,
            verify_runtime=lambda edit,profile:True,icc_registry=ICCRegistry(ROOT))


def test_worker_and_backend_exposure_icc_pins_agree():
    import re
    source=(ROOT.parents[1]/'backend/internal/semanticreview/exposure_color.go').read_text()
    assert re.search(r'const exposureICCProfileSHA = "([0-9a-f]{64})"',source).group(1)==WORKING_PROFILE_SHA
    lock=json.loads((ROOT/'profiles.lock.json').read_text())
    assert [p['sha256'] for p in lock['profiles'] if p['id']=='work_rec2020_gamma24_v1']==[WORKING_PROFILE_SHA]
