from __future__ import annotations

from contextlib import contextmanager
import hashlib
import io
import json
from types import SimpleNamespace
from uuid import uuid4

import cv2
import numpy as np
import pytest

from photo_worker.algorithms.dust import FrameDust, detect_frame_dust, map_to_sensor, repeated_dust
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.dust import (DustCandidates, DustParams, DustRepairParams,
                                         DustReview, accepted_dust_candidates)
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.sensor.dust import execute
from photo_worker.operations.sensor.repair import execute_repair
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for as base_context


def bind_fixture_candidate_ids(proposal):
    from photo_worker.contracts.dust import dust_candidate_id
    proposal.candidates=[candidate.model_copy(update={'id':dust_candidate_id(
        proposal.camera_id,proposal.sensor_width,proposal.sensor_height,proposal.frames,
        candidate.sensor_x,candidate.sensor_y,candidate.radius_sensor_px)})
        for candidate in proposal.candidates]
    return proposal


def dust_frame(point=(64, 48), include=True):
    image = np.full((96, 128, 3), .72, np.float32)
    if include: cv2.circle(image, point, 3, (.42, .42, .42), -1)
    return image


def test_detector_and_sensor_mapping_find_compact_dark_region():
    found = detect_frame_dust(dust_frame())
    assert found and min(np.hypot(item.x - 64, item.y - 48) for item in found) < 1
    mapped = map_to_sensor(found[0], (96, 128), (100, 200, 256, 192))
    assert 100 <= mapped.x < 356 and 200 <= mapped.y < 392


def test_recurrence_requires_eighty_percent_and_is_ordered():
    common = FrameDust(100., 80., 2.)
    frames = [[common], [FrameDust(100.5, 79.7, 2.1)], [common], [common], [], [FrameDust(10, 10, 1)]]
    assert repeated_dust(frames, .8) == []
    repeated = repeated_dust(frames[:5], .8)
    assert len(repeated) == 1 and repeated[0][1:] == (4, 5)


class Artifacts:
    def __init__(self, root, repeated=True):
        root.mkdir(parents=True, exist_ok=True)
        self.paths = {}; self.manifests = {}; self.staged = []; refs = []
        for index in range(5):
            image_array = dust_frame(include=repeated or index == 0)
            path = root / f"dust-{index}.npy"; np.save(path, image_array)
            pixels = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            image = WorkingImage(pixels=pixels,
                canvas=Canvas(width=128, height=96, orientation=1, space_id=f"dust:{index}", source_sha256=pixels.sha256),
                color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="a" * 64, encoding="gamma24"),
                dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
            ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(image)).hexdigest())
            self.paths[pixels.artifact_id] = path; self.manifests[ref.artifact_id] = image; refs.append(ref)
        self.refs = refs
        self.raw = DustParams(frames=refs, camera_id="camera-1", sensor_width=256, sensor_height=192,
                              raw_crop={"x": 0, "y": 0, "width": 256, "height": 192},
                              orientation=1, recurrence=.8).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self, manifest, role):
        self.staged.append((role, manifest))
        return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())


def context(artifacts):
    ctx = base_context(artifacts.refs[0], "sensor.dust")
    ctx.lease.command.inputs = [*artifacts.refs, *[artifacts.manifests[ref.artifact_id].pixels for ref in artifacts.refs]]
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 536_870_912}
    return ctx


def test_sensor_adapter_emits_review_only_repeated_candidates(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); result = execute(ctx, artifacts)
    role, manifest = artifacts.staged[0]
    assert role == "dust_candidates" and len(manifest.candidates) >= 1 and result.review_required
    candidate = min(manifest.candidates, key=lambda item: abs(item.sensor_x - 129))
    assert candidate.observed_frames == candidate.eligible_frames == 5 and candidate.recurrence == 1
    assert candidate.decision == "pending" and ctx.snapshot_progress() == {"done": 6, "total": 6}
    DustCandidates.model_validate_json(canonical_bytes(manifest))


def test_one_off_dust_is_not_promoted_and_invalid_series_fails(tmp_path):
    one = Artifacts(tmp_path / "one", repeated=False); result = execute(context(one), one)
    assert not result.review_required and not one.staged[0][1].candidates
    assert one.staged[0][1].warnings == ["no_repeated_dust"]

    missing = Artifacts(tmp_path / "missing"); ctx = context(missing)
    ctx.lease.command.inputs.remove(missing.manifests[missing.refs[0].artifact_id].pixels)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.staged

    bad = Artifacts(tmp_path / "bad"); bad.raw["raw_crop"]["width"] = 300
    with pytest.raises(OperationFailure) as error: execute(context(bad), bad)
    assert error.value.error.code == "INPUT_INVALID" and not bad.staged


def test_sensor_dust_cancellation_does_not_stage(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 12: raise RuntimeError("cancelled")
    ctx.check_active = active
    with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, artifacts)
    assert not artifacts.staged


def test_dust_review_selection_is_bound_to_proposal_and_frame(tmp_path):
    artifacts = Artifacts(tmp_path); execute(context(artifacts), artifacts)
    proposal = artifacts.staged[0][1]; candidate = proposal.candidates[0]
    proposal_ref = ArtifactRef(artifact_id=uuid4(), sha256="d" * 64)
    review = DustReview(version="sensor_dust_review_v1", proposal=proposal_ref,
                        base=proposal.frames[0], revision=2,
                        accepted_candidate_ids=[candidate.id], rejected_candidate_ids=[])
    params = DustRepairParams(image=proposal.frames[1], proposal=proposal_ref, review=review)
    assert accepted_dust_candidates(params, proposal, review) == [candidate]
    foreign_base = review.model_copy(update={"base":ArtifactRef(artifact_id=uuid4(),sha256="f"*64)})
    with pytest.raises(ValueError,match="STALE_DUST_REVIEW"):
        accepted_dust_candidates(params.model_copy(update={"review":foreign_base}),proposal)
    substituted = review.model_copy(update={"accepted_candidate_ids":[]})
    with pytest.raises(ValueError,match="STALE_DUST_REVIEW"):
        accepted_dust_candidates(params,proposal,substituted)
    malformed = proposal.model_copy(deep=True)
    malformed.candidates[0] = malformed.candidates[0].model_copy(update={"recurrence":.5})
    with pytest.raises(ValueError,match="RECURRENCE_INVALID"):
        accepted_dust_candidates(params,malformed)

    with pytest.raises(ValueError, match="STALE_DUST_REVIEW"):
        accepted_dust_candidates(params.model_copy(update={"proposal": ArtifactRef(artifact_id=uuid4(), sha256="f" * 64)}), proposal, review)


class RepairArtifacts:
    def __init__(self, root, source, image, image_ref, proposal, proposal_ref, review):
        self.params_value = DustRepairParams(image=image_ref, proposal=proposal_ref, review=review)
        self.manifests = {image_ref.artifact_id: image, proposal_ref.artifact_id: proposal}
        self.paths = {image.pixels.artifact_id: source}; self.staged = []
        root.mkdir(parents=True)
        self.scratch = ScratchSpace(root, uuid4(), uuid4(), 1, 8_000_000, min_free_bytes=0)

    def params(self, model): return self.params_value
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self, path, reservation, role, media_type):
        payload = path.read_bytes(); ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(payload).hexdigest())
        self.staged.append((role, payload, ref)); return ref
    def stage_contract(self, value, role):
        payload = canonical_bytes(value); ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(payload).hexdigest())
        self.staged.append((role, payload, ref)); return ref


def test_dust_repair_applies_only_authorized_sensor_candidate(tmp_path):
    source = tmp_path / "source.npy"; rgb = dust_frame(); np.save(source, rgb)
    pixels = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(source.read_bytes()).hexdigest())
    image = WorkingImage(pixels=pixels, canvas=Canvas(width=128, height=96, orientation=1,
        space_id="dust:repair", source_sha256="a" * 64),
        color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="b" * 64, encoding="gamma24"),
        dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
    image_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(image)).hexdigest())
    proposal_ref = ArtifactRef(artifact_id=uuid4(), sha256="c" * 64)
    candidate_id = hashlib.sha256(b"candidate").hexdigest()
    proposal = DustCandidates(frames=[image_ref] + [ArtifactRef(artifact_id=uuid4(), sha256=f"{i+1:064x}") for i in range(4)], camera_id="camera-1", sensor_width=256,
        sensor_height=192, raw_crop={"x": 0, "y": 0, "width": 256, "height": 192},
        candidates=[{"id": candidate_id, "sensor_x": 129, "sensor_y": 97, "radius_sensor_px": 6,
                     "observed_frames": 5, "eligible_frames": 5, "recurrence": 1., "decision": "pending"}], warnings=[])
    # Runtime proposals require unique frames; use the target plus four unrelated refs.
    proposal.frames = [image_ref] + [ArtifactRef(artifact_id=uuid4(), sha256=f"{i+1:064x}") for i in range(4)]
    bind_fixture_candidate_ids(proposal)
    candidate_id=proposal.candidates[0].id
    review = DustReview(version="sensor_dust_review_v1", proposal=proposal_ref, base=image_ref, revision=2,
                        accepted_candidate_ids=[candidate_id], rejected_candidate_ids=[])
    artifacts = RepairArtifacts(tmp_path / "scratch", source, image, image_ref, proposal, proposal_ref, review)
    ctx = base_context(image_ref, "sensor.dust_repair"); ctx.lease.command.params = ArtifactRef(artifact_id=uuid4(), sha256="e" * 64)
    ctx.lease.command.inputs = [image_ref, pixels, proposal_ref, ctx.lease.command.params]
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 536_870_912}
    try:
        result = execute_repair(ctx, artifacts)
        from photo_worker.v2.render_dust import prepare_pinned_dust
        from photo_worker.v2.render_adjustments import AdjustmentPixels
        from photo_worker.v2.render_effects import effect_review_required
        artifacts.manifests[ctx.lease.command.params.artifact_id]=artifacts.params_value
        sealed={'resource_estimate':dict(pixel_count=rgb.shape[0]*rgb.shape[1],
            ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
        expected=np.load(io.BytesIO(next(payload for role,payload,_ in artifacts.staged if role=='working_rgb')))
        for protection in (None,lambda x,y,w,h:np.ones((h,w),np.float32)):
            with prepare_pinned_dust(artifacts,ctx.lease.command.params,AdjustmentPixels(rgb,None),
                    execution_profile=sealed,verify_runtime=lambda:None,read_protected=protection,
                    tile_size=256) as stage:
                np.testing.assert_array_equal(stage.pixels.rgb.view(np.uint32),
                    (rgb if protection else expected).view(np.uint32))
                assert effect_review_required(stage)==(protection is not None)
                assert stage.report.candidates[0].candidate_id==candidate_id
        assert not list(artifacts.scratch.directory.iterdir())
    finally:
        artifacts.scratch.cleanup()
    repaired = np.load(io.BytesIO(next(payload for role, payload, _ in artifacts.staged if role == "working_rgb")), allow_pickle=False)
    assert repaired.shape == rgb.shape and np.mean(repaired[45:52, 61:68]) > np.mean(rgb[45:52, 61:68])
    assert [role for role, _, _ in artifacts.staged] == ["working_rgb", "working_image", "dust_repair_report"]
    assert len(result.outputs) == 3 and ctx.snapshot_progress() == {"done": 3, "total": 3}


@pytest.mark.parametrize('observed,eligible,recurrence',[(6,5,1.),(4,5,1.),(5,5,.8)])
def test_dust_candidate_rejects_invented_recurrence(observed,eligible,recurrence):
    from photo_worker.contracts.dust import DustCandidate
    with pytest.raises(ValueError,match='RECURRENCE_INVALID'):
        DustCandidate(id='a'*64,sensor_x=10,sensor_y=10,radius_sensor_px=2,
            observed_frames=observed,eligible_frames=eligible,recurrence=recurrence)


def test_dust_proposal_rejects_duplicate_frames():
    from photo_worker.contracts.dust import DustCandidates
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    with pytest.raises(ValueError,match='SERIES_INVALID'):
        DustCandidates(frames=[ref]*5,camera_id='camera-1',sensor_width=20,sensor_height=20,
            raw_crop=dict(x=0,y=0,width=20,height=20),candidates=[],warnings=['no_repeated_dust'])


@pytest.mark.parametrize('cancel_after_yield',[False,True])
def test_shared_dust_repair_matches_existing_telea_and_closes_scratch(tmp_path,cancel_after_yield):
    from photo_worker.contracts.dust import DustCandidate,SensorCrop
    from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair
    source=dust_frame();original=source.copy()
    h,w=source.shape[:2]
    crop=SensorCrop(x=0,y=0,width=w,height=h)
    candidate=DustCandidate(id='a'*64,sensor_x=64,sensor_y=48,radius_sensor_px=3,
        observed_frames=5,eligible_frames=5,recurrence=1.)
    mask=np.zeros((h,w),np.uint8)
    cv2.circle(mask,(64,48),3,255,-1,lineType=cv2.LINE_8)
    expected=np.empty_like(source)
    for channel in range(3):
        expected[:,:,channel]=cv2.inpaint(source[:,:,channel],mask,5.,cv2.INPAINT_TELEA)
    np.clip(expected,0,1,out=expected)
    cancelled=False
    def active():
        if cancelled:raise InterruptedError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        def consume():
            nonlocal cancelled
            with prepare_dust_repair(source,crop,[candidate],scratch,check_active=active) as prepared:
                np.testing.assert_array_equal(np.load(prepared.path).view(np.uint32),expected.view(np.uint32))
                assert prepared.candidates[0].applied_pixels==29
                cancelled=cancel_after_yield
        if cancel_after_yield:
            with pytest.raises(InterruptedError):consume()
        else:consume()
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(source.view(np.uint32),original.view(np.uint32))


@pytest.mark.parametrize('variant',['alpha','rgb_changed','alpha_changed','budget','cancel','id_changed'])
def test_native_dust_alpha_binding_and_cleanup(tmp_path,variant):
    from photo_worker.contracts.dust import DustCandidate,SensorCrop
    from photo_worker.v2.render_dust import prepare_authored_dust
    from photo_worker.contracts.style import Restoration
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    rgb=np.full((32,32,3),.5,np.float32);rgb[15:18,15:18]=.1
    alpha=np.tile(np.linspace(0,1,32,dtype=np.float32),(32,1))
    paths={};contracts={}
    def array(value,name):
        path=tmp_path/(name+'.npy');np.save(path,value)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        paths[ref.artifact_id]=path;return ref
    def contract(value):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())
        contracts[ref.artifact_id]=value;return ref
    image=WorkingImage(pixels=array(rgb,'rgb'),alpha=array(alpha,'alpha'),dtype='float32',
        canvas=Canvas(width=32,height=32,orientation=1,space_id='dust',source_sha256='a'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='b'*64,encoding='gamma24'),
        transform_chain=TransformChain.create([]))
    image_ref=contract(image)
    candidate=DustCandidate(id='c'*64,sensor_x=16,sensor_y=16,radius_sensor_px=2,
        observed_frames=5,eligible_frames=5,recurrence=1.)
    proposal=DustCandidates(frames=[image_ref]+[ArtifactRef(artifact_id=uuid4(),sha256='d'*64) for _ in range(4)],
        camera_id='camera-1',sensor_width=32,sensor_height=32,raw_crop=SensorCrop(x=0,y=0,width=32,height=32),
        candidates=[candidate],warnings=[])
    bind_fixture_candidate_ids(proposal)
    candidate=proposal.candidates[0]
    proposal_ref=contract(proposal)
    review=DustReview(version='sensor_dust_review_v1',proposal=proposal_ref,base=image_ref,revision=1,
        accepted_candidate_ids=[candidate.id],rejected_candidate_ids=[])
    params_ref=contract(DustRepairParams(image=image_ref,proposal=proposal_ref,review=review))
    if variant=='id_changed':
        proposal.candidates[0]=proposal.candidates[0].model_copy(update={'sensor_x':17})
    actual_rgb=rgb.copy();actual_alpha=alpha.copy()
    if variant=='rgb_changed':actual_rgb[0,0,0]=.4
    if variant=='alpha_changed':actual_alpha[-1,-1]=0
    estimate=dict(pixel_count=1024,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)
    if variant=='budget':estimate['ram_bytes']=0
    cancelled=False
    def active():
        if cancelled:raise InterruptedError('cancelled')
    @contextmanager
    def input(ref,**kwargs):yield SimpleNamespace(path=paths[ref.artifact_id])
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch,input=input,contract=lambda ref,model:contracts[ref.artifact_id])
        def consume():
            nonlocal cancelled
            with prepare_authored_dust(artifacts,image_ref,Restoration(denoise='off',denoise_strength=0,
                    upscale='off',sensor_dust='review'),params_ref,AdjustmentPixels(actual_rgb,actual_alpha),
                    execution_profile={'resource_estimate':estimate},verify_runtime=lambda:None,check_active=active) as stage:
                np.testing.assert_array_equal(stage.pixels.alpha.view(np.uint32),alpha.view(np.uint32))
                assert stage.pixels.rgb[16,16,0]>.1
                cancelled=variant=='cancel'
        if variant in ('rgb_changed','alpha_changed'):
            with pytest.raises(ValueError,match='CURRENT_PIXELS_CHANGED'):consume()
        elif variant=='id_changed':
            with pytest.raises(ValueError,match='DUST_CANDIDATE_ID_INVALID'):consume()
        elif variant=='budget':
            with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):consume()
        elif variant=='cancel':
            with pytest.raises(InterruptedError):consume()
        else:consume()
        if variant=='alpha':
            from photo_worker.v2.render_dust import PinnedDustProviderFactory
            from photo_worker.v2.render_layers import RenderLayer
            from photo_worker.v2.render_layer_params import LoadedRenderLayer
            from photo_worker.v2.render_adjustments import compose_render_adjustments
            from photo_worker.v2.render_effects import effect_review_required
            layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',
                operation='sensor.dust_repair',operation_version='1',params=params_ref,mask={'type':'all'})
            factory=PinnedDustProviderFactory(runtime_sha256='a'*64,verify_runtime=lambda:None,
                validate_binding=lambda *args:None,protection_reader=lambda *args:None)
            from photo_worker.v2.render_providers import RenderLayerProviders
            callback=RenderLayerProviders(factory(artifacts,object(),
                {'resource_estimate':estimate,'tile_size':256},lambda:None))
            masks=SimpleNamespace(layer=lambda identity,x,y,w,h:np.ones((h,w),np.float32),
                protection=lambda *args:None)
            with compose_render_adjustments(rgb,alpha,(LoadedRenderLayer(layer,contracts[params_ref.artifact_id],'a'*64),),
                    masks,scratch,callback,color=image.color,max_tile_bytes=1024*1024,tile_size=256) as composed:
                assert composed.rgb[16,16,0]>.1
                np.testing.assert_array_equal(composed.alpha.view(np.uint32),alpha.view(np.uint32))
                assert composed.effects[0].dust_report.candidates[0].candidate_id==candidate.id
                assert effect_review_required(composed.effects[0]) is False
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(rgb[15:18,15:18],np.full((3,3,3),.1,np.float32))


def test_native_dust_zero_applied_evidence_cannot_declare_pass():
    from photo_worker.contracts.dust import DustRepairReport,DustRepairEvidence
    from photo_worker.v2.render_dust import RenderDustStage
    from photo_worker.v2.render_effects import effect_review_required
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    evidence=DustRepairEvidence(candidate_id='b'*64,center_x=1,center_y=1,radius=2,applied_pixels=0)
    report=DustRepairReport(image=ref,proposal=ref,review_revision=1,candidates=[evidence],warnings=[])
    assert effect_review_required(RenderDustStage(None,report,False)) is True
    evidence_positive=evidence.model_copy(update={'applied_pixels':1})
    positive=report.model_copy(update={'candidates':[evidence_positive]})
    assert effect_review_required(RenderDustStage(None,positive,False)) is False
    malformed=report.model_copy(update={'warnings':['no_accepted_candidates']})
    with pytest.raises(ValueError,match='EFFECT_EVIDENCE_INVALID'):
        effect_review_required(RenderDustStage(None,malformed,False))


@pytest.mark.parametrize('field,value',[('center_x',float('inf')),('center_y',float('nan')),
    ('radius',float('inf')),('applied_pixels',200000001)])
def test_dust_evidence_rejects_nonfinite_and_impossible_counts(field,value):
    from photo_worker.contracts.dust import DustRepairEvidence
    raw=dict(candidate_id='a'*64,center_x=1,center_y=1,radius=2,applied_pixels=1)
    raw[field]=value
    with pytest.raises(ValueError):DustRepairEvidence.model_validate(raw)


def test_large_dust_repair_refuses_fixed_ram_before_pixel_io():
    from photo_worker.contracts.dust import DustCandidate,SensorCrop
    from photo_worker.operations.sensor.prepare_repair import dust_repair_ram_bytes
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    proposal_ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    pixels=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    image=WorkingImage(pixels=pixels,alpha=None,dtype='float32',
        canvas=Canvas(width=8000,height=6000,orientation=1,space_id='large',source_sha256='d'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='e'*64,encoding='gamma24'),
        transform_chain=TransformChain.create([]))
    candidate=DustCandidate(id='f'*64,sensor_x=100,sensor_y=100,radius_sensor_px=2,
        observed_frames=5,eligible_frames=5,recurrence=1.)
    proposal=DustCandidates(frames=[ref]+[ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(4)],
        camera_id='camera-1',sensor_width=8000,sensor_height=6000,
        raw_crop=SensorCrop(x=0,y=0,width=8000,height=6000),candidates=[candidate],warnings=[])
    bind_fixture_candidate_ids(proposal)
    candidate=proposal.candidates[0]
    review=DustReview(version='sensor_dust_review_v1',proposal=proposal_ref,base=ref,revision=1,
        accepted_candidate_ids=[candidate.id],rejected_candidate_ids=[])
    params=DustRepairParams(image=ref,proposal=proposal_ref,review=review)
    def forbidden(*args,**kwargs):raise AssertionError('Pixel IO before RAM admission')
    artifacts=SimpleNamespace(params=lambda model:params,
        contract=lambda reference,model:image if reference==ref else proposal,input=forbidden)
    ctx=base_context(ref,'sensor.dust_repair');ctx.lease.command.inputs=[ref,pixels,proposal_ref]
    ctx.lease.execution_profile.clear()
    ctx.lease.execution_profile.update({'backend':'cpu','resource_estimate':{'ram_bytes':536870912}})
    assert dust_repair_ram_bytes(8000,6000)==6412435456
    with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
        execute_repair(ctx,artifacts)


@pytest.mark.parametrize('width,height,x,y,radius',[
    (31,19,15,9,4),(31,19,-2,0,5),(31,19,33,20,5),
    (31,19,4.5,6.5,2.5),(31,19,5.5,7.5,3.5),
    (701,533,350,260,300),(701,533,-100,250,1000),
    (900,3,850,1,25),(31,19,100,100,2)])
def test_dust_circle_count_matches_original(width,height,x,y,radius):
    from photo_worker.operations.sensor.prepare_repair import dust_circle_pixel_count
    yy,xx=np.ogrid[:height,:width]
    expected=int(np.count_nonzero((xx-round(x))**2+(yy-round(y))**2<=round(radius)**2))
    assert dust_circle_pixel_count(width,height,x,y,radius)==expected


def test_dust_circle_count_bounds_temporaries_and_cancels(monkeypatch):
    from photo_worker.operations.sensor.prepare_repair import dust_circle_pixel_count
    original=np.count_nonzero;shapes=[]
    def count(part):
        shapes.append(part.shape)
        return original(part)
    monkeypatch.setattr(np,'count_nonzero',count)
    assert dust_circle_pixel_count(1800,700,900,350,1000)>0
    assert shapes and all(h<=256 and w<=256 for h,w in shapes)
    calls=0
    def cancel():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        dust_circle_pixel_count(1800,700,900,350,1000,check_active=cancel)
    assert calls==4


@pytest.mark.parametrize('variant',['off','auto','source','invalid_style','cancel'])
def test_authored_dust_rejects_wrong_policy_source_and_mutated_contracts(variant):
    from photo_worker.v2.render_dust import prepare_authored_dust
    from photo_worker.contracts.style import Restoration
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    other=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    restoration=Restoration(denoise='off',denoise_strength=0,upscale='off',sensor_dust='review')
    if variant=='off':restoration.sensor_dust='off'
    if variant=='auto':restoration.sensor_dust='conservative_auto'
    if variant=='invalid_style':restoration=restoration.model_copy(update={'sensor_dust':'unknown'})
    reads=[]
    def contract(reference,model):
        reads.append(reference)
        return DustRepairParams(image=other)
    def forbidden(*args,**kwargs):raise AssertionError('Pixel IO before authored binding')
    def active():
        if variant=='cancel':raise InterruptedError('cancelled')
    artifacts=SimpleNamespace(contract=contract,input=forbidden)
    error=InterruptedError if variant=='cancel' else ValueError
    with pytest.raises(error):
        with prepare_authored_dust(artifacts,ref,restoration,other,None,
                execution_profile={},verify_runtime=forbidden,check_active=active):
            raise AssertionError('Unexpected execution')
    assert len(reads)==(1 if variant=='source' else 0)


@pytest.mark.parametrize('variant',['sensor_small','sensor_large','crop','camera','x_before',
    'x_after','y_before','y_after','infinite','recurrence','warnings'])
def test_dust_proposal_physical_invariants_revalidated_before_acceptance(variant):
    from photo_worker.contracts.dust import DustCandidate,SensorCrop,accepted_dust_candidates
    refs=[ArtifactRef(artifact_id=uuid4(),sha256=f'{i+1:064x}') for i in range(5)]
    proposal_ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    candidate=DustCandidate(id='b'*64,sensor_x=12,sensor_y=13,radius_sensor_px=2,
        observed_frames=5,eligible_frames=5,recurrence=1.)
    proposal=DustCandidates(frames=refs,camera_id='camera-1',sensor_width=30,sensor_height=30,
        raw_crop=SensorCrop(x=10,y=10,width=20,height=20),candidates=[candidate],warnings=[])
    review=DustReview(version='sensor_dust_review_v1',proposal=proposal_ref,base=refs[0],revision=1,
        accepted_candidate_ids=[candidate.id],rejected_candidate_ids=[])
    params=DustRepairParams(image=refs[0],proposal=proposal_ref,review=review)
    if variant=='sensor_small':proposal=proposal.model_copy(update={'sensor_width':7})
    elif variant=='sensor_large':proposal=proposal.model_copy(update={'sensor_height':200001})
    elif variant=='crop':proposal=proposal.model_copy(update={'raw_crop':proposal.raw_crop.model_copy(update={'width':21})})
    elif variant=='camera':proposal=proposal.model_copy(update={'camera_id':''})
    elif variant=='x_before':candidate=candidate.model_copy(update={'sensor_x':9.99})
    elif variant=='x_after':candidate=candidate.model_copy(update={'sensor_x':30})
    elif variant=='y_before':candidate=candidate.model_copy(update={'sensor_y':9.99})
    elif variant=='y_after':candidate=candidate.model_copy(update={'sensor_y':30})
    elif variant=='infinite':candidate=candidate.model_copy(update={'sensor_x':float('inf')})
    elif variant=='recurrence':candidate=candidate.model_copy(update={'observed_frames':3,'recurrence':.6})
    else:proposal=proposal.model_copy(update={'warnings':['no_repeated_dust']})
    # model_copy deliberately bypasses validation: exercise the real boundary.
    proposal=proposal.model_copy(update={'candidates':[candidate]})
    with pytest.raises(ValueError):accepted_dust_candidates(params,proposal)


def test_empty_dust_proposal_requires_explicit_no_repeated_warning():
    from photo_worker.contracts.dust import SensorCrop
    refs=[ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(5)]
    values=dict(frames=refs,camera_id='camera-1',sensor_width=20,sensor_height=20,
        raw_crop=SensorCrop(x=0,y=0,width=20,height=20),candidates=[])
    with pytest.raises(ValueError):DustCandidates(**values,warnings=[])
    assert DustCandidates(**values,warnings=['no_repeated_dust']).candidates==[]


@pytest.mark.parametrize('field',['id','sensor_x','radius_sensor_px','frames','camera_id'])
def test_content_bound_dust_candidate_identity_rejects_substitution(field):
    from photo_worker.contracts.dust import DustCandidate,SensorCrop,dust_candidate_id,verify_dust_candidate_ids
    refs=[ArtifactRef(artifact_id=uuid4(),sha256=f'{i+1:064x}') for i in range(5)]
    identity=dust_candidate_id('camera-1',30,30,refs,12.,13.,2.)
    candidate=DustCandidate(id=identity,sensor_x=12,sensor_y=13,radius_sensor_px=2,
        observed_frames=5,eligible_frames=5,recurrence=1.)
    proposal=DustCandidates(frames=refs,camera_id='camera-1',sensor_width=30,sensor_height=30,
        raw_crop=SensorCrop(x=0,y=0,width=30,height=30),candidates=[candidate],warnings=[])
    owned=verify_dust_candidate_ids(proposal)
    assert owned==proposal and owned is not proposal and owned.candidates[0] is not candidate
    if field=='frames':proposal=proposal.model_copy(update={'frames':list(reversed(refs))})
    elif field=='camera_id':proposal=proposal.model_copy(update={'camera_id':'camera-2'})
    else:
        value={'id':'f'*64,'sensor_x':14.,'radius_sensor_px':3.}[field]
        proposal=proposal.model_copy(update={'candidates':[candidate.model_copy(update={field:value})]})
    with pytest.raises(ValueError,match='DUST_CANDIDATE_ID_INVALID'):verify_dust_candidate_ids(proposal)



def test_dust_repair_selection_verifies_all_candidates_even_rejected(tmp_path):
    artifacts=Artifacts(tmp_path);execute(context(artifacts),artifacts)
    proposal=artifacts.staged[0][1]
    original=proposal.candidates[0]
    reference=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    review=DustReview(version='sensor_dust_review_v1',proposal=reference,base=proposal.frames[0],
        revision=1,accepted_candidate_ids=[],rejected_candidate_ids=[original.id])
    params=DustRepairParams(image=proposal.frames[0],proposal=reference,review=review)
    assert accepted_dust_candidates(params,proposal)==[]
    proposal.candidates[0]=original.model_copy(update={'radius_sensor_px':original.radius_sensor_px+.1})
    with pytest.raises(ValueError,match='DUST_CANDIDATE_ID_INVALID'):
        accepted_dust_candidates(params,proposal)


def test_large_dust_detection_refuses_insufficient_ram_before_pixel_io(tmp_path):
    from photo_worker.algorithms.dust import dust_detection_ram_bytes
    artifacts=Artifacts(tmp_path)
    for image in artifacts.manifests.values():
        image.canvas.width=8000;image.canvas.height=6000
    def forbidden(*args,**kwargs):raise AssertionError('Pixel IO before detector admission')
    artifacts.input=forbidden
    assert dust_detection_ram_bytes(8000,6000)==9484435456
    with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
        execute(context(artifacts),artifacts)
    assert not artifacts.staged


def original_repeated_dust_reference(frames,minimum_fraction):
    clusters=[]
    for frame_index,candidates in enumerate(frames):
        for candidate in candidates:
            best=None;distance_best=float('inf')
            for index,cluster in enumerate(clusters):
                if any(seen==frame_index for seen,_ in cluster):continue
                x=np.mean([item.x for _,item in cluster]);y=np.mean([item.y for _,item in cluster])
                distance=float(np.hypot(candidate.x-x,candidate.y-y))
                if distance<=2 and distance<distance_best:best,distance_best=index,distance
            if best is None:clusters.append([(frame_index,candidate)])
            else:clusters[best].append((frame_index,candidate))
    result=[]
    for cluster in clusters:
        observed=len({index for index,_ in cluster})
        if observed<int(np.ceil(minimum_fraction*len(frames))):continue
        result.append((FrameDust(float(np.mean([item.x for _,item in cluster])),
            float(np.mean([item.y for _,item in cluster])),float(np.mean([item.radius for _,item in cluster]))),observed,len(frames)))
    return sorted(result,key=lambda value:(value[0].y,value[0].x))


@pytest.mark.parametrize('fraction',[.8,1.])
def test_cached_dust_clustering_matches_original_means_order_and_ties(fraction):
    rng=np.random.default_rng(142)
    frames=[]
    for index in range(12):
        points=[FrameDust(float(10*j+rng.uniform(-.8,.8)),float(5*j+rng.uniform(-.8,.8)),
            float(rng.uniform(1,3))) for j in range(18) if (index+j)%7!=0]
        points += [FrameDust(300,20,2),FrameDust(302,20,3),FrameDust(301,20,1)]
        frames.append(points)
    assert repeated_dust(frames,fraction)==original_repeated_dust_reference(frames,fraction)


def test_dust_clustering_checks_cancellation_during_large_cluster_search(monkeypatch):
    first=[FrameDust(float(index*10),0,1) for index in range(600)]
    comparisons=0
    original=np.hypot
    def distance(x,y):
        nonlocal comparisons
        comparisons+=1
        return original(x,y)
    monkeypatch.setattr(np,'hypot',distance)
    def active():
        if comparisons>=256:raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError,match='cancelled'):
        repeated_dust([first,[FrameDust(7000,0,1)],[],[],[]],check_active=active)
    assert comparisons==256
