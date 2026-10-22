"""Native input-only color→ICC→encoding replay; not device qualification."""
from hashlib import sha256
from io import BytesIO
import json
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef as CRef,canonical_bytes,canonical_hash
from photo_worker.contracts.style import Disabled,StyleV2
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.calibration import Qualification,PinnedRegionCalibrationCatalog,region_workload_sha
from photo_worker.operations.region.installation import install_native_region_color
from photo_worker.operations.region.presentation import prepare_region_presentation,write_region_presentation
from photo_worker.operations.region.result import build_region_manifest
from photo_worker.operations.learning.color_render import render_materialized_style
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.region_replay import NativeRegionReplay
from test_region_preview_adoption import RegionReader
from test_region_calibration import calibrated
from test_region_preview_presentation import ROOT,ICC_LOCK,fixture
from test_region_runtime_lock import lock
from test_style_resolve import profile as style_fixture


def native_case(tmp_path,staged=False):
    runtime=lock();path=tmp_path/'runtime.json';path.write_bytes(canonical_bytes(runtime))
    raw,execution,params,_=calibrated(runtime_sha=canonical_hash(runtime))
    plan=params.model_dump(mode='json');_,body=fixture();image=json.loads(body)
    style=style_fixture().model_dump(mode='json',by_alias=True)
    for name in ('local','beauty','defects','geometry'):style[name]=Disabled().model_dump(mode='json')
    style['models']=[]
    if staged:style['extensions']['background']=dict(mode='solid',asset_id=None,solid_rgb=[.2,.4,.6],blur_strength=.5,protect_subject=True)
    style=StyleV2.model_validate(style)
    style_ref=CRef(artifact_id=uuid4(),sha256=canonical_hash(style))
    rgb=np.linspace(.1,.8,400*300*3,dtype=np.float32).reshape(300,400,3)
    binary=BytesIO();np.save(binary,rgb,allow_pickle=False);pixels=binary.getvalue()
    image['pixels']['sha256']=sha256(pixels).hexdigest()
    source=CRef(artifact_id=uuid4(),sha256=canonical_hash(image))
    recipe=plan['frozen_recipe'];recipe['source']['source']=source.model_dump(mode='json');recipe['base_style']=style_ref.model_dump(mode='json')
    recipe_ref=CRef(artifact_id=uuid4(),sha256=canonical_hash(recipe))
    plan['source']['ref']=source.model_dump(mode='json');plan['source']['bytes']=len(canonical_bytes(image))
    plan['recipe']['ref']=recipe_ref.model_dump(mode='json');plan['recipe']['bytes']=len(canonical_bytes(recipe))
    plan['request']['recipe']=recipe_ref.model_dump(mode='json')
    plan['inputs']=[dict(ref=r.model_dump(mode='json'),contract=c) for r,c in [(source,'WorkingImage'),(recipe_ref,'FrozenRecipe'),(style_ref,'StyleV2')]]
    plan['inputs'].append(dict(ref=image['pixels'],contract='NpyArray'));plan['inputs'].sort(key=lambda i:i['ref']['artifact_id'])
    extra=[];stage_expected=None
    if staged:
        plan,extra,stage_expected=staged_background_plan(tmp_path,plan,image,style,rgb,source,style_ref)
        execution['resource_estimate']['scratch_bytes']+=32*120000+8192+1048576+16384
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    catalog=json.loads(raw);entry=catalog['profiles'][0];entry['workload_sha']=region_workload_sha(params.frozen_recipe)
    raw=canonical_bytes(catalog);execution['region_qualification'].update(catalog_sha=sha256(raw).hexdigest(),calibration_sha=canonical_hash(entry),workload_sha=entry['workload_sha'])
    q=Qualification.model_validate(entry['qualification'])
    renderer=install_native_region_color(qualification=q,icc_root=ROOT,verify_qualification=lambda actual:actual==q,runtime_path=path,
        calibration_catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest()),max_input_bytes=16*1024*1024)
    # Standalone shared math supplies the expected derivative; no fake renderer.
    color_style=style.model_copy(deep=True);color_style.extensions.background=None
    expected=np.empty_like(rgb);render_materialized_style(rgb,expected,color_style,person_reliable=False)
    if stage_expected is not None:expected=stage_expected
    presentation_options={}
    if staged:
        bound=params.validate_source(canonical_bytes(image))
        presentation_options=dict(target_canvas=bound.canvas,target_chain=bound.transform_chain,
            load_inverse_field=lambda *_:pytest.fail('same-canvas stage requested dense field'))
    presentation=prepare_region_presentation(params,canonical_bytes(image),expected,None,ROOT,runtime_icc_lock_sha=ICC_LOCK,**presentation_options)
    encoded=tmp_path/'expected.jpg';size=write_region_presentation(encoded,presentation)
    derivative=CRef(artifact_id=uuid4(),sha256=sha256(encoded.read_bytes()).hexdigest())
    manifest=build_region_manifest(params,presentation,derivative,size)
    reader=RegionReader();ref=lambda r:ArtifactRef(r.artifact_id,r.sha256)
    params_ref=ArtifactRef(uuid4(),canonical_hash(params));root=ArtifactRef(uuid4(),canonical_hash(manifest))
    reader.add(params_ref,body=params.model_dump(mode='json'),source='input')
    reader.add(ref(source),body=image,source='input');reader.add(ref(style_ref),body=style.model_dump(mode='json',by_alias=True),source='input')
    reader.add(ref(recipe_ref),body=recipe,source='input');reader.add(root,body=manifest.model_dump(mode='json'),role='document')
    pixel_ref=ArtifactRef.parse(image['pixels']);reader.rows[pixel_ref]=ArtifactEvidence(pixel_ref,'application/octet-stream',len(pixels),None,'input');reader.body[pixel_ref]=pixels
    for extra_ref,extra_body,contract in extra:
        key=ref(extra_ref)
        if contract=='NpyArray':
            reader.rows[key]=ArtifactEvidence(key,'application/octet-stream',len(extra_body),None,'input');reader.body[key]=extra_body
        else:reader.add(key,body=extra_body,source='input')
    # Output derivative bytes deliberately absent from reader.
    scratch=tmp_path/'scratch';scratch.mkdir()
    return NativeRegionReplay(renderer,scratch,execution['resource_estimate']['scratch_bytes']),reader,params_ref,root,execution,pixel_ref


def test_full_native_input_only_color_icc_and_encoded_replay(tmp_path):
    replay,reader,params,root,execution,pixels=native_case(tmp_path)
    replay.verify(reader,params,root,task_id=uuid4(),attempt=1,fence=7,profile=execution,check_active=lambda:None)
    assert not any(p.is_file() or p.is_symlink() for p in replay.scratch_root.rglob('*'))


def test_full_native_refuses_forged_derivative_and_changed_input(tmp_path):
    replay,reader,params,root,execution,pixels=native_case(tmp_path)
    body=json.loads(reader.body[root]);body['derivative']['sha256']='0'*64
    forged=ArtifactRef(uuid4(),canonical_hash(body));reader.add(forged,body=body,role='document')
    with pytest.raises(AdoptionInvalid,match='PIXELS_MISMATCH'):
        replay.verify(reader,params,forged,task_id=uuid4(),attempt=1,fence=7,profile=execution,check_active=lambda:None)
    reader.body[pixels]=reader.body[pixels][:-1]+b'x'
    with pytest.raises(AdoptionInvalid):
        replay.verify(reader,params,root,task_id=uuid4(),attempt=1,fence=7,profile=execution,check_active=lambda:None)


def staged_background_plan(tmp_path,plan,image,style,rgb,source,style_ref):
    """Original adopted-document inventory uses actual shared prefix math."""
    from copy import deepcopy
    from photo_worker.algorithms.style import resolve_style
    from photo_worker.contracts.resolved import ResolvedStyle,ResolvedModules,ResolveParams
    from photo_worker.contracts.images import MaskRef,WorkingImage
    from photo_worker.contracts.background import ComposeParams
    from photo_worker.operations.background.prepare import prepare_background
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.runtime.arrays import read_array
    extra=[]
    def doc(value,contract):
        body=value.model_dump(mode='json') if hasattr(value,'model_dump') else value
        reference=CRef(artifact_id=uuid4(),sha256=canonical_hash(body));extra.append((reference,body,contract))
        plan['inputs'].append(dict(ref=reference.model_dump(mode='json'),contract=contract));return reference
    def array(values):
        stream=BytesIO();np.save(stream,values,allow_pickle=False);body=stream.getvalue()
        reference=CRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest());extra.append((reference,body,'NpyArray'))
        plan['inputs'].append(dict(ref=reference.model_dump(mode='json'),contract='NpyArray'));return reference
    resolved=ResolvedStyle.create(params=ResolveParams(style=style_ref,image=source),mode='fixed',fixed=style.global_,
        absolute_lut=None,local=style.local,strength=style.strength,
        modules=ResolvedModules(beauty=style.beauty,defects=style.defects,geometry=style.geometry,extensions=style.extensions,models=style.models),
        neighbors=[],distance=None,uncovered_fraction=None,reasons=[],needs_review=False)
    resolved_ref=doc(resolved,'ResolvedStyle')
    global_pixels=resolve_style(style,person_reliable=False).global_pass(rgb)
    color_style=style.model_copy(deep=True);color_style.extensions.background=None
    local_pixels=np.empty_like(rgb);render_materialized_style(rgb,local_pixels,color_style,person_reliable=False)
    source_image=WorkingImage.model_validate_json(canonical_bytes(image));stages=[]
    current=source
    def stage(operation,params,pixels):
        nonlocal current
        params_ref=doc(params,'Params');target=deepcopy(image);target['pixels']=array(pixels).model_dump(mode='json')
        target_ref=doc(target,'WorkingImage')
        stages.append(dict(operation=operation,operation_version=1,task_id=str(uuid4()),result_id=str(uuid4()),attempt=1,fencing_token=1,
            params=params_ref.model_dump(mode='json'),source=current.model_dump(mode='json'),output=target_ref.model_dump(mode='json'),
            source_canvas=image['canvas'],target_canvas=image['canvas']))
        current=target_ref
    stage('image.color',dict(image=current.model_dump(mode='json'),resolved=resolved_ref.model_dump(mode='json')),global_pixels)
    stage('image.local',dict(image=current.model_dump(mode='json'),base_aligned=source.model_dump(mode='json'),resolved=resolved_ref.model_dump(mode='json')),local_pixels)
    subject=np.full(rgb.shape[:2],.75,np.float32)
    mask=MaskRef(artifact=array(subject),canvas=source_image.canvas,transform_chain=source_image.transform_chain,selector='subject',confidence=1.,protected=False,model_lock_id=None)
    compose=ComposeParams(image=current,subject=mask,mode='solid',solid_rgb=[.2,.4,.6])
    scratch_root=tmp_path/'expected-background';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        with prepare_background(local_pixels,subject,None,None,compose,scratch) as native:
            with read_array(native.rgb,rgb.shape,'float32',lambda:None,unit_interval=True) as result:expected=result.copy()
    stage('background.compose',compose,expected)
    plan['execution']=dict(schema_version=1,source=source.model_dump(mode='json'),source_canvas_hash=canonical_hash(source_image.canvas),
        style=style_ref.model_dump(mode='json'),stages=stages)
    plan['inputs'].sort(key=lambda item:item['ref']['artifact_id'])
    return plan,extra,expected


def test_full_native_staged_color_local_background_replay(tmp_path):
    replay,reader,params,root,execution,pixels=native_case(tmp_path,staged=True)
    replay.verify(reader,params,root,task_id=uuid4(),attempt=2,fence=9,profile=execution,check_active=lambda:None)
    assert not any(p.is_file() or p.is_symlink() for p in replay.scratch_root.rglob('*'))
