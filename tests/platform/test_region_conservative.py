from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import Defects,Disabled
from photo_worker.operations.region.conservative import conservative_entry


def setup(policy='conservative_auto'):
    config=Defects(enabled=True,algorithm='defectrepair_telea_v1',policy=policy,strength=.5,
        diameter_face_fraction=[.002,.012],preserve_permanent_marks=True)
    effective=SimpleNamespace(defects=config,strength=.7,extensions=SimpleNamespace(restoration=None))
    resolved=SimpleNamespace(modules=SimpleNamespace(defects=config),strength=.7)
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64).model_dump(mode='json')
    raw=dict(policy='conservative_auto_v1',image=ref(),proposal=ref(),proposal_report=ref(),resolved=ref())
    image=SimpleNamespace(canvas='same',transform_chain='same',alpha=None)
    return effective,(SimpleNamespace(operation='defects.auto_repair'),raw,image,image,{'resolved':resolved})


def test_automatic_stage_has_exact_effective_policy_and_original_params():
    effective,entry=setup()
    params,source=conservative_entry(effective,[entry],'defects.auto_repair')
    assert params.image.model_dump(mode='json')==entry[1]['image'] and source is entry[2]


def test_review_policy_never_impersonates_automatic_authorization():
    effective,entry=setup('review')
    with pytest.raises(ValueError,match='CONSERVATIVE_STAGE_UNEXPECTED'):
        conservative_entry(effective,[entry],'defects.auto_repair')


@pytest.mark.parametrize('mode',['missing','duplicate','strength','policy','geometry','disabled'])
def test_stale_or_missing_automatic_evidence_refuses(mode):
    effective,entry=setup();entries=[entry]
    if mode=='missing':entries=[]
    elif mode=='duplicate':entries*=2
    elif mode=='strength':entry[4]['resolved'].strength=.6
    elif mode=='policy':entry[4]['resolved'].modules.defects=entry[4]['resolved'].modules.defects.model_copy(update={'strength':.4})
    elif mode=='geometry':entries=[(*entry[:3],SimpleNamespace(canvas='other',transform_chain='same',alpha=None),entry[4])]
    else:effective.defects=Disabled()
    with pytest.raises(ValueError):conservative_entry(effective,entries,'defects.auto_repair')


def test_dust_reuses_pinned_engine_and_refuses_different_current_rgb(tmp_path):
    import numpy as np
    from test_image_color_adapter import Artifacts
    from photo_worker.contracts.style import Restoration
    from photo_worker.contracts.images import MaskRef
    from photo_worker.contracts.resolved import ResolvedStyle
    from photo_worker.operations.region.conservative import prepare_region_conservative
    a=Artifacts(tmp_path,False)
    a.image=a.image.model_copy(update={'alpha':None});a.image_ref=a.add(a.image)
    config=Restoration(denoise='off',denoise_strength=0,upscale='off',sensor_dust='conservative_auto')
    values=a.resolved.model_dump(exclude={'hash'});values['params']['image']=a.image_ref;values['modules']['extensions']['restoration']=config
    resolved=ResolvedStyle.create(**values)
    mask=MaskRef(artifact=a.array(tmp_path,'protected-dust',np.ones(a.rgb.shape[:2],np.float32)),
        canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='manual',confidence=1,protected=True,model_lock_id=None)
    params=dict(policy='conservative_auto_v1',image=a.image_ref.model_dump(mode='json'),proposal=None,
        resolved=a.add(resolved).model_dump(mode='json'),protected=[mask.model_dump(mode='json')])
    effective=SimpleNamespace(defects=Disabled(),strength=resolved.strength,extensions=SimpleNamespace(restoration=config))
    verified=[(SimpleNamespace(operation='sensor.dust_auto_repair'),params,a.image,a.image,{'resolved':resolved})]
    a.require_declared_input=lambda ref:None
    checked=[]
    options=dict(profile={'backend':'mps','resource_estimate':{'ram_bytes':1<<30}},verify_runtime=lambda:checked.append(True),check_active=lambda:None)
    with prepare_region_conservative(a,effective,verified,'sensor.dust_auto_repair',a.rgb,None,**options) as actual:
        assert actual is a.rgb
    assert checked
    altered=a.rgb.copy();altered[0,0]=0 if np.any(altered[0,0]) else 1
    with pytest.raises(ValueError,match='CONSERVATIVE_CURRENT_SOURCE_INVALID'):
        with prepare_region_conservative(a,effective,verified,'sensor.dust_auto_repair',altered,None,**options):
            pytest.fail('Different predecessor RGB was accepted')
