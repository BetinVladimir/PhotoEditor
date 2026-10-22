"""Real native repair through the installed render layer provider, no SQL authority."""
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_image_color_adapter import Artifacts
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.style import Restoration
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.conservative import DustAutoRepairParams
from photo_worker.contracts.dust import DustCandidate, DustCandidates, SensorCrop, dust_candidate_id
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_conservative import PinnedConservativeProviderFactory, conservative_render_provider_peak
from photo_worker.v2.render_resources import render_resources
from photo_worker.v2.render_layers import RenderLayer
from photo_worker.v2.render_layer_params import LoadedRenderLayer
from photo_worker.v2.render_adjustments import AdjustmentPixels


@pytest.mark.parametrize('protected', [False, True])
def test_native_dust_layer_executes_and_preserves_nonselected_pixels(tmp_path, protected):
    a = Artifacts(tmp_path, False)
    a.rgb = np.full((100, 100, 3), .4, np.float32)
    a.rgb[50, 50] = .1
    pixels = a.array(tmp_path, 'dust-source', a.rgb)
    canvas = a.image.canvas.model_copy(update={'width':100, 'height':100, 'source_sha256':pixels.sha256})
    a.image = a.image.model_copy(update={'pixels':pixels, 'canvas':canvas, 'alpha':None})
    a.image_ref = a.add(a.image)
    config = Restoration(denoise='off', denoise_strength=0, upscale='off', sensor_dust='conservative_auto')
    values = a.resolved.model_dump(exclude={'hash'})
    values['params']['image'] = a.image_ref
    values['modules']['extensions']['restoration'] = config
    resolved = ResolvedStyle.create(**values)
    mask = MaskRef(artifact=a.array(tmp_path, 'dust-protection', np.full((100,100), float(protected), np.float32)),
                   canvas=canvas, transform_chain=a.image.transform_chain, selector='manual', confidence=1,
                   protected=True, model_lock_id=None)
    frames = [a.image_ref] + [ArtifactRef(artifact_id=uuid4(), sha256=f'{i:064x}') for i in range(1,10)]
    candidate_id = dust_candidate_id('test-camera',100,100,frames,50.,50.,1.)
    candidate = DustCandidate(id=candidate_id, sensor_x=50, sensor_y=50, radius_sensor_px=1,
                             observed_frames=10, eligible_frames=10, recurrence=1)
    proposal = DustCandidates(frames=frames,camera_id='test-camera',sensor_width=100,sensor_height=100,
                              raw_crop=SensorCrop(x=0,y=0,width=100,height=100),candidates=[candidate],warnings=[])
    params = DustAutoRepairParams(policy='conservative_auto_v1',image=a.image_ref,proposal=a.add(proposal),
                                  resolved=a.add(resolved),protected=[mask])
    layer = RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='sensor.dust_auto_repair',
                        operation_version='1',params=a.add(params),mask={})
    runtime = 'c'*64
    profile = dict(backend='cpu',resource_estimate=render_resources(100,100,mask_refs=0,mask_nodes=0,
                                      provider_peak=conservative_render_provider_peak(100,100)))
    verified=[]
    providers = PinnedConservativeProviderFactory(runtime_sha256=runtime,verify_runtime=lambda:verified.append(True),
                                                  validate_binding=lambda *args:None)(a,None,profile,lambda:None)
    provider = next(p for p in providers if p.operator.operation==layer.operation)
    a.require_declared_input=lambda ref:None
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with provider.prepare(LoadedRenderLayer(layer,params,runtime),AdjustmentPixels(a.rgb,None)) as result:
            output=np.array(result.pixels.rgb,copy=True)
            report=result.effect.conservative_report
            assert report.applied_candidate_ids == ([] if protected else [candidate_id])
            assert np.array_equal(output[:15],a.rgb[:15])
            if protected:assert np.array_equal(output,a.rgb)
            else:assert not np.array_equal(output,a.rgb)
        assert list(scratch.directory.iterdir())==[]
    assert verified


@pytest.mark.parametrize('mode', ['red', 'pigmented', 'protected'])
@pytest.mark.parametrize('alpha_kind', ['none', 'float32', 'uint16'])
def test_native_defect_layer_preserves_marks_and_protection(tmp_path, mode, alpha_kind):
    from photo_worker.contracts.conservative import DefectAutoRepairParams
    from photo_worker.contracts.defects import DefectCandidate, DefectCandidates, DefectProposeParams, DefectProposeReport, candidate_id
    from photo_worker.contracts.images import Rect
    from photo_worker.contracts.style import Defects
    a = Artifacts(tmp_path, False)
    a.rgb = np.full((100,100,3), .5, np.float32)
    a.rgb[49:52,49:52] = [.7,.45,.45] if mode != 'pigmented' else [.2,.15,.1]
    pixels = a.array(tmp_path, 'defect-source', a.rgb)
    canvas = a.image.canvas.model_copy(update={'width':100,'height':100,'source_sha256':pixels.sha256})
    alpha = None if alpha_kind=='none' else np.full((100,100),.5,np.float32) if alpha_kind=='float32' else np.full((100,100),10000,np.uint16)
    alpha_ref = None if alpha is None else a.array(tmp_path,'defect-alpha',alpha)
    current_alpha = None if alpha is None else alpha if alpha_kind=='float32' else alpha.astype(np.float32)/np.float32(65535)
    a.image = a.image.model_copy(update={'pixels':pixels,'canvas':canvas,'alpha':alpha_ref})
    a.image_ref = a.add(a.image)
    config = Defects(enabled=True, algorithm='defectrepair_telea_v1', policy='conservative_auto',
                     strength=.8, diameter_face_fraction=[.002,.012], preserve_permanent_marks=True)
    values = a.resolved.model_dump(exclude={'hash'})
    values['params']['image'] = a.image_ref
    values['modules']['defects'] = config
    values['strength'] = 1.
    resolved = ResolvedStyle.create(**values)
    protection = np.zeros((100,100),np.float32)
    if mode == 'protected':protection[50,50] = 1
    mask = MaskRef(artifact=a.array(tmp_path,'defect-protection',protection),canvas=canvas,
                   transform_chain=a.image.transform_chain,selector='manual',confidence=1,protected=True,model_lock_id=None)
    labels = np.zeros((100,100),np.uint16);labels[49:52,49:52] = 1
    label_ref = a.array(tmp_path,'defect-labels',labels)
    bbox = Rect(x=49,y=49,width=3,height=3)
    identity = candidate_id(pixels,label_ref,1,bbox,3.)
    candidate = DefectCandidate(id=identity,label=1,bbox=bbox,diameter=3,score=.99)
    proposal = DefectCandidates(image=a.image_ref,source_pixels=pixels,label_map=label_ref,canvas=canvas,
                                transform_chain=a.image.transform_chain,candidates=[candidate])
    proposal_ref = a.add(proposal)
    report = DefectProposeReport(params=DefectProposeParams(image=a.image_ref,faces=[]),proposal=proposal_ref,
                                 protection=mask,label_png=label_ref,faces=[],warnings=['no_faces'])
    params = DefectAutoRepairParams(policy='conservative_auto_v1',image=a.image_ref,proposal=proposal_ref,
                                    proposal_report=a.add(report),resolved=a.add(resolved))
    layer = RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='defects.auto_repair',
                        operation_version='1',params=a.add(params),mask={})
    runtime='c'*64
    profile=dict(backend='cpu',alpha_mode='normalize_uint16' if alpha_kind=='uint16' else 'require_float32',resource_estimate=render_resources(100,100,mask_refs=0,mask_nodes=0,
                                    provider_peak=conservative_render_provider_peak(100,100),alpha_mode='normalize_uint16' if alpha_kind=='uint16' else 'require_float32'))
    verified=[]
    providers=PinnedConservativeProviderFactory(runtime_sha256=runtime,verify_runtime=lambda:verified.append(True),
                                                validate_binding=lambda *args:None)(a,None,profile,lambda:None)
    provider=next(p for p in providers if p.operator.operation==layer.operation)
    a.require_declared_input=lambda ref:None
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with provider.prepare(LoadedRenderLayer(layer,params,runtime),AdjustmentPixels(a.rgb,current_alpha)) as result:
            output=np.array(result.pixels.rgb,copy=True)
            if current_alpha is not None:np.testing.assert_array_equal(result.pixels.alpha,current_alpha)
            evidence=result.effect.conservative_report
            assert evidence.applied_candidate_ids == ([identity] if mode=='red' else [])
            assert np.array_equal(output[labels==0],a.rgb[labels==0])
            if mode=='red':assert not np.array_equal(output,a.rgb)
            else:assert np.array_equal(output,a.rgb)
        assert list(scratch.directory.iterdir())==[]
    assert verified
