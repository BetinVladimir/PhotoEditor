"""Actual TaskArtifacts/index/adapter/staging with an in-memory transport port."""
from io import BytesIO
from hashlib import sha256
from uuid import uuid4
import sys,platform
from importlib import metadata
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.runtime.artifacts import ArtifactConfig
from photo_worker.operations.portrait.local_resources import local_portrait_resources
from photo_worker.operations.portrait.relight_adapter import relight_operations
from photo_worker.operations.portrait.relight_runtime_lock import RelightRuntimeLock,installed_component_hashes
from photo_worker.v2.input_closure import InputClosureEntry,InputClosureChunk,InputClosureManifest
from photo_worker.v2.contract_lock import SCHEMAS_SHA256
from test_relight_inputs import inputs
from test_portrait_manual_admission import context


@pytest.mark.parametrize('corrupt',[False,True])
def test_real_task_artifacts_manifest_only_relight_operation(tmp_path,monkeypatch,corrupt):
    from photo_worker.runtime import artifacts as artifact_module
    params,source,depth,subject,guard,calibration,*_=inputs()
    h,w=source.canvas.height,source.canvas.width
    blobs={};entries=[];staged={}
    def put(raw,contract):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());blobs[ref.artifact_id]=raw
        if contract:entries.append(InputClosureEntry(ref=ref,contract=contract))
        return ref
    def array(value):
        stream=BytesIO();np.save(stream,value,allow_pickle=False)
        return put(stream.getvalue(),'NpyArray')
    source=source.model_copy(update={'pixels':array(np.full((h,w,3),.4,np.float32)), 'alpha':None})
    depth=depth.model_copy(update={'artifact':array(np.full((h,w),.5,np.float32)),'invalid_mask':None})
    subject=subject.model_copy(update={'artifact':array(np.ones((h,w),np.float32))})
    protected=np.zeros((h,w),np.float32);protected[:,0]=1
    guard=guard.model_copy(update={'artifact':array(protected)})
    source_ref=put(canonical_bytes(source),'WorkingImage')
    params=params.model_copy(update={'base':params.base.model_copy(update={'source':source_ref}),
        'depth':put(canonical_bytes(depth),'RelativeDepth'),'subject':put(canonical_bytes(subject),'MaskRef'),
        'protected':put(canonical_bytes(guard),'MaskRef'),'calibration':put(canonical_bytes(calibration),'Calibration')})
    params_ref=put(canonical_bytes(params),'Params')
    ctx=context();ctx.lease.command.params=params_ref
    ctx.lease.command.operation='image.relight';ctx.lease.command.operation_version='1';ctx.lease.command.resource_class='cpu'
    entries.sort(key=lambda e:(e.ref.artifact_id.bytes,e.ref.sha256,e.contract))
    chunk=put(canonical_bytes(InputClosureChunk(schema_version=1,entries=entries)),None)
    digest=canonical_hash([entry.model_dump(mode='json') for entry in entries])
    manifest=put(canonical_bytes(InputClosureManifest(schema_version=1,task_id=ctx.lease.command.task_id,
        entry_count=len(entries),entries_sha=digest,chunks=[chunk])),None)
    ctx.lease.command.inputs=[manifest]
    lock=RelightRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),
        implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','opencv-python-headless')},components=installed_component_hashes())
    runtime_sha=canonical_hash(lock)
    profile=dict(backend='cpu',renderer_version='v2.2',contracts_sha=SCHEMAS_SHA256,v2_input_manifest=manifest.model_dump(mode='json'),
        v2_entry_count=len(entries),v2_entries_sha=digest,relight_runtime_sha=runtime_sha,resource_estimate=local_portrait_resources(w,h))
    ctx.lease.execution_profile=profile
    if corrupt:blobs[source.pixels.artifact_id]=b'x'+blobs[source.pixels.artifact_id][1:]
    class Gateway:
        def __init__(self,*a,**k):pass
        def open_stream(self,ref):
            raw=blobs[ref.artifact_id];stream=BytesIO(raw);stream.size=len(raw);return stream
        def upload(self,path,role,mime,*,expected_identity=None):
            assert expected_identity==(path.stat().st_dev,path.stat().st_ino)
            raw=path.read_bytes();ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
            staged[role]=(raw,ref);return ref
    monkeypatch.setattr(artifact_module,'Gateway',Gateway)
    monkeypatch.setattr(artifact_module,'TaskCredentials',lambda *a:None)
    calls=[]
    class Admission:
        def admit(self,local,*docs):
            calls.append(1)
            assert source.pixels in local.lease.command.inputs
            assert ctx.lease.command.inputs==[manifest]
    config=ArtifactConfig(tmp_path,'https://unused',None,profile['resource_estimate']['scratch_bytes'],10**8,10**8)
    operation=relight_operations(config,lambda *a:Admission(),runtime_lock=lock,expected_runtime_sha=runtime_sha)[0]
    if corrupt:
        with pytest.raises(RuntimeError,match='INPUT_INVALID'):operation.execute(ctx)
        assert not staged
    else:
        result=operation.execute(ctx)
        assert result.review_required and result.outputs==[staged['document'][1]] and len(calls)==2
        assert set(staged)=={'document','working_image','pixels','mask','mask_pixels','report'}
        output=np.load(BytesIO(staged['pixels'][0]),allow_pickle=False)
        np.testing.assert_array_equal(output[:,0],np.full_like(output[:,0],.4))
        assert np.any(output[:,1:]>.4)
    assert not list(tmp_path.rglob('input_index-*.bin'))
