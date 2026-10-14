from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain,MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime import restore_render_arrays as m


@pytest.mark.parametrize('mode',['reuse','produced','wrong_source','produced_failure','fixture_false','activity_false'])
def test_nested_array_routing_preserves_exact_source_reuse_and_output_authority(monkeypatch,mode):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=WorkingImage(pixels=ref(),alpha=ref(),dtype='float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    raw=canonical_bytes(source)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),canvas_hash=canonical_hash(source.canvas))
    image=source.model_copy(update={'pixels':ref()}) if mode in ('produced','produced_failure') else source
    mask=MaskRef(artifact=ref(),canvas=source.canvas,transform_chain=source.transform_chain,selector='changed',confidence=1,model_lock_id=None,protected=False)
    calls=[]
    def reused(*args,**kwargs):calls.append(('reuse',args[-1]));return 'reuse'
    def produced(*args,**kwargs):
        calls.append(('produced',args[-1]))
        if mode=='produced_failure':raise ValueError('producer missing')
        return 'produced'
    monkeypatch.setattr(m,'verify_fixture_source_array',reused)
    monkeypatch.setattr(m,'verify_fixture_produced_array',produced)
    def run():return m.verify_fixture_render_arrays(object(),object(),('photos',),uuid4(),object(),binding,
        b'{}' if mode=='wrong_source' else raw,canonical_bytes(image),canonical_bytes(mask),None,
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('reuse','produced'):
        assert len(run())==3
        assert calls==[('produced' if mode=='produced' else 'reuse','pixels'),('reuse','alpha'),('produced','mask_pixels')]
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('wrong_source','fixture_false','activity_false'):assert not calls
