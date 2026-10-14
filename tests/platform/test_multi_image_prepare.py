from pathlib import Path
from contextlib import contextmanager
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.multi_image_prepare import prepare_multi_image
from photo_worker.runtime.scratch import ScratchSpace


def fixture():
    image=WorkingImage.model_validate_json((Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json').read_bytes())
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(image)),canvas_hash=canonical_hash(image.canvas)) for _ in range(2)]
    return image,bindings


@pytest.mark.parametrize('operation,projection,threshold,policy',[
    ('panorama','planar',float('nan'),'review'),
    ('panorama','cylindrical',float('inf'),'review'),
    ('focus_stack','planar',.5,'ignore'),
])
def test_invalid_motion_configuration_refuses_before_source_conversion(operation,projection,threshold,policy):
    image,bindings=fixture()
    def unexpected_icc(_):pytest.fail('source conversion checked ICC')
    with pytest.raises(ValueError,match='MULTI_MOTION_INVALID'):
        with prepare_multi_image([image]*2,bindings,[],[],0,None,
                operation=operation,projection=projection,max_pixels=200000000,
                ram_max_bytes=2**40,verify_icc=unexpected_icc,registration_error_px=1.,
                match_inlier_min=.8,motion_threshold=threshold,motion_policy=policy):
            pytest.fail('unexpected source conversion')


@pytest.mark.parametrize('operation,projection',[('focus_stack','planar'),('panorama','cylindrical')])
def test_source_to_encoded_lifetime_with_controlled_registration(tmp_path,monkeypatch,operation,projection):
    import photo_worker.operations.optical.multi_image_prepare as module
    from photo_worker.operations.optical.multi_prepare import prepare_planar_stack,AutomaticMultiPrepared
    @contextmanager
    def automatic(frames,alphas,reference,scratch,**kwargs):
        for key in ('registration_error_px','match_inlier_min'):kwargs.pop(key)
        with prepare_planar_stack(frames,alphas,[np.eye(3)]*2,reference,scratch,**kwargs) as prepared:
            yield AutomaticMultiPrepared(prepared,(),False)
    monkeypatch.setattr(module,'prepare_automatic_planar_stack',automatic)
    import photo_worker.operations.optical.cylindrical_stack as cylindrical_module
    monkeypatch.setattr(cylindrical_module,'prepare_automatic_planar_stack',automatic)
    image,bindings=fixture();rgb=np.full((100,100,3),4.,np.float32);alpha=np.ones((100,100),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,32*1024**2,min_free_bytes=0) as scratch:
        with prepare_multi_image([image]*2,bindings,[rgb]*2,[alpha]*2,0,scratch,
                operation=operation,projection=projection,max_pixels=10000,ram_max_bytes=2**40,verify_icc=lambda _:True,
                registration_error_px=1.,match_inlier_min=.8,motion_threshold=.5,tile=16) as result:
            encoded=np.load(result.encoded.rgb);opacity=np.load(result.encoded.alpha)
            np.testing.assert_allclose(encoded[opacity>0],4.,atol=1e-6)
            assert result.bindings==tuple(bindings) and result.geometry_changed==(projection=='cylindrical')
            from hashlib import sha256
            import json
            from photo_worker.operations.optical.multi_result import stage_multi_result
            from photo_worker.contracts.base import canonical_bytes
            class Artifacts:
                def __init__(self):self.records={}
                def save(self,data,role,decoded=None):
                    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(data).hexdigest())
                    self.records[str(ref.artifact_id)]=(data,role,decoded)
                    return ref
                def stage(self,path,reservation,role,media_type):
                    reservation.validate_file(path)
                    assert media_type=='application/x-npy'
                    return self.save(path.read_bytes(),role)
                def stage_contract(self,value,role):
                    data=canonical_bytes(value)
                    return self.save(data,role,json.loads(data))
            artifacts=Artifacts();staged=stage_multi_result(artifacts,result,tile=16)
            from photo_worker.contracts.images import DenseWarpTransform
            source_children=[image.pixels]
            if image.alpha is not None:source_children.append(image.alpha)
            source_children.extend(step.inverse_field for step in image.transform_chain.steps
                                   if isinstance(step,DenseWarpTransform))
            assert staged.required_inputs[0]==bindings[0].source
            assert {(r.artifact_id,r.sha256) for r in staged.required_inputs}=={
                (r.artifact_id,r.sha256) for r in [*(b.source for b in bindings),*source_children]}
            root=artifacts.records[str(staged.document.artifact_id)][2]
            assert root['generated_mask'] is None and staged.review_required
            report=artifacts.records[root['report']['artifact_id']][2]
            assert report['sources']==[b.model_dump(mode='json') for b in bindings]
            registrations=[artifacts.records[r['artifact_id']][2] for r in report['registrations']]
            assert [r['source'] for r in registrations]==report['sources']
            if projection=='planar':assert all(r['inverse_field'] is None for r in registrations)
            else:
                field_ids=[r['inverse_field']['artifact_id'] for r in registrations]
                assert len(set(field_ids))==2
                assert all(artifacts.records[key][1]=='warp_field' for key in field_ids)
                image_document=artifacts.records[root['working_image']['artifact_id']][2]
                dense=image_document['transform_chain']['steps'][-2]
                assert dense['pixel_centers']=='half_pixel' and dense['inverse_field']['artifact_id']==field_ids[0]
            # Walk the actual ImageResult closure, with declared originals as
            # authorized leaves. Every staged output must be reachable.
            leaves={str(b.source.artifact_id):b.source.sha256 for b in bindings};visited=set()
            def walk(value):
                if isinstance(value,dict):
                    if set(value)=={'artifact_id','sha256'}:
                        key=value['artifact_id']
                        if key in leaves:assert value['sha256']==leaves[key];return
                        data,_role,document=artifacts.records[key]
                        assert sha256(data).hexdigest()==value['sha256']
                        if key not in visited:
                            visited.add(key)
                            if document is not None:walk(document)
                    else:
                        for child in value.values():walk(child)
                elif isinstance(value,list):
                    for child in value:walk(child)
            walk(staged.document.model_dump(mode='json'))
            assert visited=={str(ref.artifact_id) for ref in staged.outputs}
            assert staged.bytes_written==sum(len(record[0]) for record in artifacts.records.values())
            path=result.encoded.rgb
        assert not path.exists() and not list(scratch.directory.iterdir())


def test_binding_mismatch_precedes_source_allocation():
    image,bindings=fixture();bindings[0].source.sha256='b'*64
    with pytest.raises(ValueError,match='MULTI_SOURCE_MISMATCH'):
        with prepare_multi_image([image]*2,bindings,[],[],0,None,operation='panorama',max_pixels=10000,
                ram_max_bytes=2**40,verify_icc=lambda _:True,registration_error_px=1.,match_inlier_min=.8,
                motion_threshold=.5):pytest.fail('unexpected result')
