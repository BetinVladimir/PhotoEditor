from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_hash, canonical_bytes
from photo_worker.contracts.images import WorkingImage, Canvas, ColorSpace, TransformChain
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_authored import prepare_authored_render
from photo_worker.v2.render_resources import render_resources
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from test_v26_render_style import setup


def fixture(tmp_path):
    artifacts, inputs, overrides = setup()
    inputs.source = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='d'*64),alpha=None,dtype='float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='a'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    inputs.recipe.recipe.protected_mask = None
    inputs.recipe.recipe.layer_stack = None
    inputs.layers = None
    inputs.protection = None
    for binding in (inputs.recipe.recipe.source,inputs.recipe.scalars.source):
        binding.canvas_hash=canonical_hash(inputs.source.canvas)
    inputs.recipe.recipe_sha256=canonical_hash(inputs.recipe.recipe)
    inputs.recipe=decode_frozen_render_recipe(canonical_bytes(inputs.recipe))
    path=tmp_path/'original.npy';np.save(path,np.zeros((1,2,3),np.float32))
    events=[]
    @contextmanager
    def materialize(ref,**kw):
        assert ref==inputs.source.pixels
        events.append('pixels-open')
        try:yield SimpleNamespace(path=path)
        finally:events.append('pixels-close')
    artifacts.input=materialize
    artifacts.scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0)
    peak=dict.fromkeys(('ram_bytes','scratch_bytes','vram_bytes','output_bytes'),0)
    arguments=dict(execution_profile={'resource_estimate':render_resources(2,1,mask_refs=0,mask_nodes=0,provider_peak=peak)},
        provider_peak=peak,supported_modules=frozenset({"color","strength"}),max_params_document_bytes=4096,max_override_document_bytes=4096,
        max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096)
    return artifacts,inputs,overrides,events,arguments,path


def test_complete_style_and_scalar_evidence_reaches_owned_pixel_provider(tmp_path):
    artifacts,inputs,overrides,events,arguments,path=fixture(tmp_path)
    def verify():events.append('qualified-runtime')
    @contextmanager
    def base(state,style,original,masks):
        assert state is inputs
        assert style.merged.strength==.4 and style.effective_delta.ev==.1
        assert style.overrides[0].accepted_candidate_ids==['preserved']
        events.append('base-open')
        try:yield AdjustmentPixels(np.full_like(original.rgb,.5),None)
        finally:events.append('base-close')
    try:
        with prepare_authored_render(artifacts,inputs,(),overrides,base,None,verify,**arguments) as prepared:
            assert prepared.render.composition.changes.changed_pixels==2
            np.testing.assert_array_equal(prepared.render.composition.pixels.rgb,.5)
            assert events==['qualified-runtime','pixels-open','base-open']
        assert events==['qualified-runtime','pixels-open','base-open','base-close','pixels-close']
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('failure',['admission','runtime','scalar'])
def test_metadata_gates_reject_before_pixel_materialization(tmp_path,failure):
    artifacts,inputs,overrides,events,arguments,_=fixture(tmp_path)
    if failure=='admission':arguments['execution_profile']={'resource_estimate':{}}
    if failure=='scalar':overrides[0].delta.ev=.2
    def verify():
        events.append('qualified-runtime')
        if failure=='runtime':raise ValueError('runtime rejected')
    try:
        with pytest.raises(OperationFailure if failure=='admission' else ValueError):
            with prepare_authored_render(artifacts,inputs,(),overrides,lambda *args:None,None,verify,**arguments):
                pytest.fail('invalid evidence admitted')
        assert not any(event.startswith('pixels') for event in events)
        if failure in ('admission','runtime'):assert artifacts.reads==[]
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('coverage',[frozenset({'color'}),{'color','strength'}])
def test_missing_or_untyped_provider_coverage_rejected_before_pixels(tmp_path,coverage):
    artifacts,inputs,overrides,events,arguments,_=fixture(tmp_path)
    arguments['supported_modules']=coverage
    try:
        with pytest.raises(ValueError,match='COMPLETE_PROVIDER_REQUIRED'):
            with prepare_authored_render(artifacts,inputs,(),overrides,lambda *args:None,None,
                    lambda:None,**arguments):pytest.fail('partial provider admitted')
        assert events==[]
    finally:artifacts.scratch.cleanup()


def test_scalar_enabled_beauty_participates_in_provider_coverage(tmp_path):
    from photo_worker.contracts.base import parse_json
    artifacts,inputs,overrides,events,arguments,_=fixture(tmp_path)
    base_ref=inputs.recipe.recipe.base_style
    style=parse_json(artifacts.bodies[base_ref.artifact_id])
    style['models']=[dict(id='sfd',weight_id='sfd-test'),dict(id='fan2d',weight_id='fan-test')]
    raw=canonical_bytes(style);base_ref.sha256=canonical_hash(style)
    inputs.recipe.scalars.base_style.sha256=base_ref.sha256
    artifacts.bodies[base_ref.artifact_id]=raw
    inputs.recipe.recipe.parameter_delta.retouch_delta=.1
    inputs.recipe.scalars.effective_delta.retouch_delta=.1
    inputs.recipe.recipe_sha256=canonical_hash(inputs.recipe.recipe)
    inputs.recipe=decode_frozen_render_recipe(canonical_bytes(inputs.recipe))
    try:
        with pytest.raises(ValueError,match='COMPLETE_PROVIDER_REQUIRED'):
            with prepare_authored_render(artifacts,inputs,(),overrides,lambda *args:None,None,
                    lambda:None,**arguments):pytest.fail('beauty omitted')
        assert events==[]
    finally:artifacts.scratch.cleanup()
