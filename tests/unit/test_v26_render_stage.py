from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import numpy as np
import pytest
from test_layer_mask_inputs import fixture
from test_layer_prepare import scratch
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.render_stage import stage_render_image


@pytest.mark.parametrize('changed',[False,True])
def test_stage_image_preserves_geometry_reuses_source_and_tracks_exact_bytes(tmp_path,changed):
    source,*_=fixture(tmp_path);space=scratch(tmp_path)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash=canonical_hash(source.canvas))
    documents=[];bodies=[]
    class Artifacts:
        def stage(self,path,owner,role,mime):
            assert mime=='application/x-npy';owner.validate_file(path)
            body=path.read_bytes();bodies.append(body)
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        def stage_contract(self,value,role):
            documents.append(value);body=canonical_bytes(value);bodies.append(body)
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    try:
        with space.reserve(10000) as reservation:
            mask=reservation.path('change','.npy');np.save(mask,np.ones((1,2),np.float32))
            rgb=reservation.path('rgb','.npy') if changed else None
            if rgb is not None:np.save(rgb,np.ones((1,2,3),np.float32))
            files=SimpleNamespace(rgb=rgb,alpha=None,generated=mask,reservation=reservation,changes=SimpleNamespace(path=mask,reservation=reservation,rgb_changed=changed,alpha_changed=False))
            result=stage_render_image(Artifacts(),source,binding,files)
            assert result.bytes_written==sum(map(len,bodies)) and len(result.outputs)==len(bodies)
            if changed:
                assert documents[0].canvas==source.canvas and documents[0].color==source.color and documents[0].transform_chain==source.transform_chain
            else:assert result.working_image==binding.source
            assert [doc.selector for doc in documents if hasattr(doc,'selector')]==['changed','generated']
    finally:space.cleanup()
