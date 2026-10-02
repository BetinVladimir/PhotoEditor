from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.contracts.style import StyleV2
from pathlib import Path
from photo_worker.v2.render_style import PreparedRenderStyle
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_color import prepare_color_base
from photo_worker.v2.render_beauty import prepare_beauty_stage,render_beauty_params
from photo_worker.v2.render_portrait import prepare_portrait_base
from test_beauty_prepare import inputs
from test_beauty import params


@pytest.mark.parametrize("protected",[False,True])
def test_color_then_real_face_beauty_equivalence_and_evidence(tmp_path,protected):
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    value.quality.status='needs_review'
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    data['strength']=.4;effective=StyleV2.model_validate(data)
    zero=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    style=PreparedRenderStyle(value,effective,effective,(),zero)
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None)))
    from photo_worker.v2.render_portrait import portrait_provider_peak
    from photo_worker.v2.render_resources import render_resources
    if protected:
        state.protection=SimpleNamespace(protected=True)
        state.recipe.recipe.protected_mask=ArtifactRef(artifact_id=uuid4(),sha256="a"*64)
    peak=portrait_provider_peak(image,style,tuple(faces),has_protection=protected)
    profile={'resource_estimate':render_resources(256,256,mask_refs=int(protected),mask_nodes=int(protected),provider_peak=peak)}
    masks=SimpleNamespace(protection=(lambda x,y,w,h:np.full((h,w),.001,np.float32)) if protected else None)
    original=AdjustmentPixels(rgb,None)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,30*1024**2,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch)
        with prepare_color_base(artifacts,state,replace(style,effective=value.model_copy(update={'strength':.4})),original,person_reliable=False,read_protected=masks.protection) as color:
            with prepare_beauty_stage(artifacts,image,color,render_beauty_params(style),tuple(faces)) as beauty:
                expected=beauty.pixels.rgb.copy();evidence=beauty.faces
        with prepare_portrait_base(artifacts,state,style,original,masks,faces=tuple(faces),person_reliable=False,execution_profile=profile) as result:
            np.testing.assert_array_equal(result.pixels.rgb.view(np.uint32),(rgb if protected else expected).view(np.uint32))
            assert result.beauty_evidence==evidence and result.review_required
        assert not list(scratch.directory.iterdir())


def test_actual_face_roi_and_final_protection_costs_are_admitted(tmp_path):
    from photo_worker.v2.render_portrait import portrait_provider_peak
    import pytest
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    value=StyleV2.model_validate(data)
    style=SimpleNamespace(effective=value)
    plain=portrait_provider_peak(image,style,tuple(faces),has_protection=False)
    protected=portrait_provider_peak(image,style,tuple(faces),has_protection=True)
    assert protected['scratch_bytes']-plain['scratch_bytes']==16*256*256+16384
    assert protected['ram_bytes']>protected['scratch_bytes'] and protected['vram_bytes']==0
    with pytest.raises(ValueError,match='RESOURCES_INVALID'):
        portrait_provider_peak(image,style,(faces[0],faces[0]),has_protection=True)


def test_missing_portrait_resource_budget_rejects_before_scratch_allocation(tmp_path):
    from photo_worker.runtime.executor import OperationFailure
    import pytest
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    style=SimpleNamespace(effective=StyleV2.model_validate(data))
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None)))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,30*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            with prepare_portrait_base(SimpleNamespace(scratch=scratch),state,style,AdjustmentPixels(rgb,None),
                    SimpleNamespace(protection=None),faces=tuple(faces),person_reliable=False,
                    execution_profile={'resource_estimate':{}}):pass
        assert not list(scratch.directory.iterdir())
