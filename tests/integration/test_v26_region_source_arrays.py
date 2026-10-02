"""Version-pinned MinIO -> actual TaskArtifacts materialization -> region mappings."""
from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
from io import BytesIO
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.inputs import prepare_region_source
from photo_worker.operations.region.resources import region_source_resources
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.scratch import ScratchSpace
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('alpha_dtype',['uint16','float32'])
def test_region_arrays_use_pinned_versions_and_preserve_hdr(lens_objects,tmp_path,alpha_dtype):
    client,bucket=lens_objects
    spec=spec_from_file_location('region_array_fixture',Path(__file__).parents[1]/'platform/test_region_preview_contract.py')
    module=module_from_spec(spec);spec.loader.exec_module(module)
    plan,image=module.region_fixture()
    locations={}
    def put(ref,body,media):
        key='region/'+ref['artifact_id']
        result=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media)
        locations[ref['artifact_id']]=(key,result.version_id,len(body))
        client.put_object(bucket,key,BytesIO(b'corrupt-latest'),14,content_type=media)
    rgb=np.full((300,400,3),1.5,np.float32)
    alpha=np.resize(np.array([0,1,32768,65535],np.uint16),(300,400))
    if alpha_dtype=='float32':alpha=alpha.astype(np.float32)/np.float32(65535)
    for field,array in [('pixels',rgb),('alpha',alpha)]:
        stream=BytesIO();np.save(stream,array,allow_pickle=False);body=stream.getvalue()
        ref=dict(artifact_id=image['pixels']['artifact_id'] if field=='pixels' else str(uuid4()),sha256=sha256(body).hexdigest())
        image[field]=ref;put(ref,body,'application/octet-stream')
    source_body=canonical_bytes(image)
    plan['source']['ref']['sha256']=canonical_hash(image)
    plan['source']['bytes']=len(source_body)
    plan['frozen_recipe']['source']['source']=plan['source']['ref'].copy()
    plan['recipe']['ref']['sha256']=canonical_hash(plan['frozen_recipe'])
    plan['recipe']['bytes']=len(canonical_bytes(plan['frozen_recipe']))
    replacements={ref['artifact_id']:ref for ref in [image['pixels'],plan['source']['ref'],plan['recipe']['ref']]}
    for entry in plan['inputs']:
        if entry['ref']['artifact_id'] in replacements:entry['ref']=replacements[entry['ref']['artifact_id']].copy()
    plan['inputs'].append(dict(ref=image['alpha'],contract='NpyArray'))
    plan['inputs'].sort(key=lambda entry:entry['ref']['artifact_id'])
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    events=[]
    streams=[]
    class Stream:
        def __init__(self,ref):
            key,version,self.size=locations[str(ref.artifact_id)]
            self.response=client.get_object(bucket,key,version_id=version)
            self.closed=False
            streams.append(self)
        def read(self,n):return self.response.read(n)
        def close(self):
            if not self.closed:self.response.close();self.response.release_conn();self.closed=True
    class Gateway:
        def open_stream(self,ref):events.append(str(ref.artifact_id));return Stream(ref)
    command=SimpleNamespace(inputs=[ArtifactRef.model_validate_json(canonical_bytes(entry['ref'])) for entry in plan['inputs']],params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command))
    budget=region_source_resources(400,300,has_alpha=True,alpha_mode='normalize_uint16')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,budget['scratch_bytes'],min_free_bytes=0) as scratch:
        artifacts=TaskArtifacts(ctx,Gateway(),scratch)
        with prepare_region_source(params,source_body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=budget['normalization_bytes']) as mapped:
            assert np.array_equal(mapped.rgb,rgb)
            expected=alpha.astype(np.float32)/np.float32(65535) if alpha_dtype=='uint16' else alpha
            assert np.array_equal(mapped.alpha,expected)
            rgb_map,alpha_map=mapped.rgb,mapped.alpha
        assert rgb_map._mmap.closed and alpha_map._mmap.closed
        with scratch.reserve(budget['scratch_bytes']):pass
        assert events==[image['pixels']['artifact_id'],image['alpha']['artifact_id']]
        for ref in [image['pixels'],image['alpha']]:
            artifact=ref['artifact_id']
            original=locations[artifact]
            key,version,size=original
            response=client.get_object(bucket,key,version_id=version)
            try:damaged=bytearray(response.read())
            finally:response.close();response.release_conn()
            damaged[-1]^=1
            changed=client.put_object(bucket,key,BytesIO(damaged),len(damaged),content_type='application/octet-stream')
            # Contradictory transport evidence: same declared ref, changed pinned bytes.
            locations[artifact]=(key,changed.version_id,size)
            with pytest.raises(ValueError,match='ARTIFACT_HASH_MISMATCH'):
                with prepare_region_source(params,source_body,artifacts,alpha_mode='normalize_uint16',max_normalization_bytes=budget['normalization_bytes']):
                    pytest.fail('damaged array was exposed to renderer')
            locations[artifact]=original
            assert all(stream.closed for stream in streams)
            with scratch.reserve(budget['scratch_bytes']):pass
    assert all(stream.closed for stream in streams)
