import hashlib
import json
from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
from types import SimpleNamespace
from uuid import uuid4
import pytest
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure
from photo_worker.operations.asset.adapters import _execute
from photo_worker.operations.asset.inspect import _exif_value

class Artifacts:
    def __init__(self,path,source):
        self.path=path;self.source=source;self.staged=[];self.opens=0
        self.raw=dict(source=source.model_dump(mode='json'),expected_bytes=path.stat().st_size,suffix=path.suffix)
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    @contextmanager
    def input(self,ref,*,suffix):
        self.opens+=1
        assert ref==self.source and suffix==self.path.suffix
        yield SimpleNamespace(path=self.path)
    def stage_contract(self,manifest,role):
        self.staged.append((role,manifest))
        return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())

def context_for(source,operation):
    now=datetime.now(timezone.utc)
    command=TaskCommand.model_validate_json(json.dumps(dict(schema_version=1,event_id=str(uuid4()),tenant_id=str(uuid4()),run_id=str(uuid4()),task_id=str(uuid4()),attempt=1,
        operation=operation,operation_version='1',resource_class='media' if operation=='asset.verify' else 'cpu',correlation_id=str(uuid4()),inputs=[source.model_dump(mode='json')],
        params=dict(artifact_id=str(uuid4()),sha256='a'*64),model=None,timeout_seconds=300,traceparent='')))
    return ExecutionContext(Lease(command,uuid4(),1,now+timedelta(seconds=60),now+timedelta(seconds=300),{'backend':'cpu'},'b'*64))

@pytest.mark.parametrize('verify',[True,False])
def test_asset_adapter_real_decode_and_metadata(tmp_path,verify):
    path=tmp_path/'input.jpg'
    exif=Image.Exif();exif[274]=6
    Image.new('RGB',(32,16),(120,80,30)).save(path,exif=exif)
    source=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    artifacts=Artifacts(path,source);ctx=context_for(source,'asset.verify' if verify else 'asset.inspect')
    result=_execute(ctx,artifacts,verify)
    role,manifest=artifacts.staged[0]
    metadata=manifest.metadata if verify else manifest
    assert role==('verified' if verify else 'metadata')
    assert metadata.width==32 and metadata.height==16 and metadata.orientation==6
    assert metadata.source==source and metadata.format=='jpeg' and metadata.bit_depth==8
    assert len(result.outputs)==1 and result.bytes_read==path.stat().st_size
    assert ctx.snapshot_progress()==dict(done=3,total=3)
    assert hashlib.sha256(path.read_bytes()).hexdigest()==source.sha256


def test_asset_adapter_rejects_bad_input_before_staging(tmp_path):
    path=tmp_path/'bad.png';path.write_bytes(b'not an image')
    source=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    for verify in (True,False):
        artifacts=Artifacts(path,source);ctx=context_for(source,'asset.verify' if verify else 'asset.inspect')
        with pytest.raises(OperationFailure) as error:_execute(ctx,artifacts,verify)
        assert error.value.error.code=='INPUT_INVALID' and not error.value.error.retryable and not artifacts.staged
    artifacts=Artifacts(path,source);ctx=context_for(source,'asset.verify');ctx.lease.command.inputs=[]
    with pytest.raises(OperationFailure):_execute(ctx,artifacts,True)
    assert artifacts.opens==0
    artifacts=Artifacts(path,source);ctx=context_for(source,'asset.verify');artifacts.raw['suffix']='../../bad.png'
    with pytest.raises(OperationFailure):_execute(ctx,artifacts,True)
    assert artifacts.opens==0

def test_camera_identity_fields_are_normalized_without_serial_guessing():
    class Header:
        def get_fields(self):return ['exif-ifd0-Make']
        def get(self,key):return '  Canon   (Canon, ASCII)'
    assert _exif_value(Header(),['exif-ifd0-Make'])=='Canon'
    assert _exif_value(Header(),['exif-ifd0-BodySerialNumber']) is None


def test_asset_adapter_rejects_forged_mime_without_staging(tmp_path):
    path=tmp_path/'forged.png'
    Image.new('RGB',(12,12),(1,2,3)).save(path,format='JPEG')
    source=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    for verify in (True,False):
        artifacts=Artifacts(path,source);ctx=context_for(source,'asset.verify' if verify else 'asset.inspect')
        with pytest.raises(OperationFailure) as error:_execute(ctx,artifacts,verify)
        assert error.value.error.code=='INPUT_INVALID'
        assert not artifacts.staged


@pytest.mark.parametrize('alpha',[False,True])
def test_decode_adapter_publishes_exact_pixels_and_manifest(tmp_path,alpha):
    from pathlib import Path
    import numpy as np
    import pyvips
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.asset.adapters import _decode
    registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')
    bands=4 if alpha else 3
    pixels=np.random.default_rng(11).integers(0,65536,size=(9,17,bands),dtype=np.uint16)
    image=pyvips.Image.new_from_memory(pixels.data,17,9,bands,'ushort').copy(interpretation='rgb16')
    image.set_type(pyvips.GValue.blob_type,'icc-profile-data',registry.resolve('work_rec2020_gamma24_v1').data)
    path=tmp_path/'input.png';image.pngsave(str(path),bitdepth=16)
    source=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    artifacts=Artifacts(path,source);ctx=context_for(source,'asset.decode');staged={}
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2*1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        def stage(path,reservation,role,media_type):
            reservation.validate_file(path)
            assert path.stat().st_mode&0o777==0o600
            assert media_type=='application/octet-stream'
            staged[role]=np.load(path,allow_pickle=False)
            return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        artifacts.stage=stage
        result=_decode(ctx,artifacts,registry)
        assert list(scratch.directory.iterdir())==[]
    assert len(result.outputs)==(3 if alpha else 2)
    assert np.array_equal(np.round(staged['working_rgb']*65535).astype(np.uint16),pixels[...,:3])
    if alpha:assert np.array_equal(staged['alpha'],pixels[...,3])
    role,manifest=artifacts.staged[0]
    assert role=='working_image' and manifest.pixels==result.outputs[0]
    assert manifest.canvas.space_id=='oriented:'+source.sha256
    assert manifest.dtype=='float32' and manifest.color.encoding=='gamma24'
    assert manifest.canvas.width==17 and manifest.canvas.height==9
    assert manifest.alpha==(result.outputs[1] if alpha else None)
    assert ctx.snapshot_progress()==dict(done=4,total=4)


def test_decode_missing_icc_and_budget_fail_before_upload(tmp_path):
    from pathlib import Path
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.runtime.scratch import ScratchSpace,DiskQuota
    from photo_worker.operations.asset.adapters import _decode
    registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')
    path=tmp_path/'input.png';Image.new('RGB',(20,20)).save(path)
    source=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    root=tmp_path/'scratch';root.mkdir()
    for budget,error in [(10,DiskQuota),(2*1048576,OperationFailure)]:
        artifacts=Artifacts(path,source)
        with ScratchSpace(root,uuid4(),uuid4(),1,budget,min_free_bytes=0) as scratch:
            artifacts.scratch=scratch
            with pytest.raises(error):_decode(context_for(source,'asset.decode'),artifacts,registry)
            assert not artifacts.staged and list(scratch.directory.iterdir())==[]
