"""Independent replay cannot read submitted output pixels or unpinned bytes."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.region_replay import RegionReplayArtifacts,NativeRegionReplay
from photo_worker.v2.region_adopter_config import RegionAdopterConfig


class Reader:
    def __init__(self,body,source='input',media='application/octet-stream'):
        self.body=body;self.ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        self.source=source;self.media=media;self.corrupt=False
    def evidence(self,ref):
        assert ref.artifact_id==self.ref.artifact_id and ref.sha256==self.ref.sha256
        return SimpleNamespace(ref=ref,source=self.source,media_type=self.media,bytes=len(self.body))
    def read_json(self,ref,max_bytes):return self.body
    def iter_bytes(self,ref,max_bytes):yield b'x'*len(self.body) if self.corrupt else self.body


def test_region_replay_materializes_exact_inputs_and_releases_owned_files(tmp_path):
    reader=Reader(b'qualified bytes')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts=RegionReplayArtifacts(reader,scratch,lambda:None)
        with artifacts.input(reader.ref,suffix='.npy') as local:
            assert local.path.read_bytes()==reader.body
        assert not list(scratch.directory.iterdir())
        assert not hasattr(artifacts,'stage') and not hasattr(artifacts,'stage_contract')


@pytest.mark.parametrize('change',['output','corrupt','media','suffix'])
def test_region_replay_rejects_output_or_changed_bytes_before_native_use(tmp_path,change):
    reader=Reader(b'qualified bytes')
    if change=='output':reader.source='output'
    if change=='corrupt':reader.corrupt=True
    if change=='media':reader.media='image/jpeg'
    suffix='.zip' if change=='suffix' else '.npy'
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts=RegionReplayArtifacts(reader,scratch,lambda:None)
        with pytest.raises(AdoptionInvalid):
            with artifacts.input(reader.ref,suffix=suffix):pytest.fail('unqualified input exposed')
        assert not list(scratch.directory.iterdir())


def test_region_replay_requires_real_installed_renderer(tmp_path):
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):NativeRegionReplay(SimpleNamespace(),tmp_path,1024)
    assert RegionAdopterConfig.from_env({}) is None
    with pytest.raises(ValueError,match='exact CUDA/Metal'):
        RegionAdopterConfig.from_env({'PHOTO_REGION_ADOPTER_ENABLED':'true','PHOTO_REGION_ADOPTER_BACKEND':'cpu'})
