from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
from photo_worker.v2.render_qualification_pin import RenderQualificationPin
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_profiled_replay import ProfiledRenderReplay
from photo_worker.v2.render_replay_reader import RenderReplayReader
from photo_worker.runtime.scratch import ScratchSpace
from test_v26_render_authored import fixture
from test_v26_render_runtime import lock


def setup(tmp_path):
    old,inputs,overrides,_,options,path=fixture(tmp_path)
    objects={}
    def put(raw,media):
        ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());objects[ref.artifact_id]=(raw,media);return ref
    for identifier,body in old.bodies.items():objects[identifier]=(body,'application/json')
    pixels=put(path.read_bytes(),'application/x-npy');path.unlink();old.scratch.cleanup()
    from photo_worker.contracts.images import WorkingImage
    source=WorkingImage.model_validate({**inputs.source.model_dump(mode='python'),
        'pixels':{'artifact_id':pixels.artifact_id,'sha256':pixels.sha256}})
    source_ref=put(canonical_bytes(source),'application/json')
    body=inputs.recipe.model_dump(mode='json')
    for name in ('recipe','scalars'):body[name]['source']['source']={'artifact_id':str(source_ref.artifact_id),'sha256':source_ref.sha256}
    body['recipe_sha256']=canonical_hash(body['recipe'])
    frozen=decode_frozen_render_recipe(canonical_bytes(body));recipe_ref=put(canonical_bytes(frozen),'application/json')
    runtime=lock();runtime_sha=canonical_hash(runtime)
    runtime_path=tmp_path/'runtime.json';runtime_path.write_bytes(canonical_bytes(runtime))
    profile=dict(options['execution_profile'],backend='cpu',renderer_version='v2.2',render_runtime_sha=runtime_sha,
        model_lock_sha=frozen.scalars.model_lock_sha,icc_lock_sha=frozen.scalars.icc_lock_sha,tile_size=512,alpha_mode='require_float32')
    pin=RenderQualificationPin(frozen.scalars.model_lock_sha,runtime_sha,'cpu','a'*64,'b'*64,'e'*64)
    scoped=RenderAdoptionInputs(recipe_ref,canonical_bytes(profile),(),pin)
    class Reader:
        opened=0;closed=0;reads=0
        def evidence(self,ref):
            raw,media=objects[ref.artifact_id]
            return SimpleNamespace(ref=ref,source='input',bytes=len(raw),media_type=media,role=None)
        def read_json(self,ref,limit):self.reads+=1;return objects[ref.artifact_id][0]
        def iter_bytes(self,ref,limit):
            self.opened+=1
            try:yield objects[ref.artifact_id][0]
            finally:self.closed+=1
    reader=Reader();reader.objects=objects;events=[]
    @contextmanager
    def scratch(actual):
        assert actual.recipe==scoped.recipe;events.append('scratch-open')
        space=ScratchSpace(tmp_path,uuid4(),uuid4(),1,100000,min_free_bytes=0)
        try:yield space
        finally:space.cleanup();events.append('scratch-close')
    def qualify(actual_pin):
        assert actual_pin==pin;events.append('controlled-qualification')
    @contextmanager
    def base(state,style,original,masks):
        assert style.merged.strength==.4 and style.effective_delta.ev==.1
        events.append('base-open')
        from photo_worker.v2.render_adjustments import AdjustmentPixels
        try:yield AdjustmentPixels(np.full_like(original.rgb,.5),None)
        finally:events.append('base-close')
    preparation={key:value for key,value in options.items() if key not in ('execution_profile','provider_peak')}
    def providers(artifacts,state,actual):
        assert not hasattr(artifacts,'stage') and actual.recipe==scoped.recipe
        return dict(operators=(),scalar_overrides=overrides,render_base=base,render_proposal=None,preparation=preparation)
    arguments=dict(backend='cpu',runtime_path=runtime_path,runtime_sha256=runtime_sha,
        model_lock_sha=frozen.scalars.model_lock_sha,icc_lock_sha=frozen.scalars.icc_lock_sha,
        provider_peak=options['provider_peak'],verify_qualification=qualify,load_providers=providers,make_scratch=scratch,qualification_pin=pin)
    return reader,scoped,frozen,arguments,events


def test_real_runtime_lock_and_existing_authored_preparation_hold_owners(tmp_path):
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen) as expected:
        assert not expected.rgb.flags.writeable
        np.testing.assert_array_equal(expected.rgb,.5)
        assert 'base-open' in events and 'base-close' not in events
    assert events[-2:]==['base-close','scratch-close']
    assert reader.opened==reader.closed==1
    assert not list(tmp_path.rglob('*.npy'))


@pytest.mark.parametrize('failure',['runtime','qualification','backend','model','loader_pin','consumer'])
def test_replay_rejects_bad_pins_and_cleans_up(tmp_path,failure):
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    if failure=='runtime':arguments['runtime_path'].write_bytes(b'{}')
    elif failure=='qualification':
        def reject(*args):raise RuntimeError('qualification unavailable')
        arguments['verify_qualification']=reject
    elif failure=='backend':arguments['backend']='cuda'
    elif failure=='model':arguments['model_lock_sha']='e'*64
    elif failure=='loader_pin':
        original=arguments['load_providers']
        def changed(*args):
            value=original(*args);value['preparation']['execution_profile']={};return value
        arguments['load_providers']=changed
    with pytest.raises(Exception):
        with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen):
            if failure=='consumer':raise RuntimeError('consumer failed')
    assert reader.opened==reader.closed and not list(tmp_path.rglob('*.npy'))
    if failure in ('runtime','qualification','backend','model'):assert reader.reads==0 and not reader.opened
    else:assert events[-1]=='scratch-close'


def test_conflicting_declared_source_parents_reject_before_provider(tmp_path):
    reader,scoped,frozen,arguments,events=setup(tmp_path);parents=[]
    for _ in range(2):
        raw=canonical_bytes(dict(schema_version=1,working_image=frozen.recipe.source.source.model_dump(mode='json'),
            report={'artifact_id':str(uuid4()),'sha256':'f'*64},
            changed_mask={'artifact_id':str(uuid4()),'sha256':'f'*64},generated_mask=None))
        ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());reader.objects[ref.artifact_id]=(raw,'application/json');parents.append(ref)
    scoped=RenderAdoptionInputs(scoped.recipe,scoped.execution_profile_jcs,tuple(parents),scoped.qualification_pin)
    with pytest.raises(AdoptionInvalid,match='SOURCE_PARENT_AMBIGUOUS'):
        with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen):pass
    assert 'base-open' not in events and events[-1]=='scratch-close'
    assert not list(tmp_path.rglob('*.npy'))


def test_provider_cannot_replace_authoritative_source_result(tmp_path):
    reader,scoped,frozen,arguments,events=setup(tmp_path);original=arguments['load_providers']
    def changed(*args):
        value=original(*args);value['preparation']['source_result']=None;return value
    arguments['load_providers']=changed
    with pytest.raises(AdoptionInvalid,match='PROVIDERS_INVALID'):
        with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen):pass
    assert 'base-open' not in events and events[-1]=='scratch-close'


@pytest.mark.parametrize('field',['device_profile_sha','dataset_sha','calibration_sha'])
def test_complete_qualification_drift_rejects_before_object_io(tmp_path,field):
    from dataclasses import replace
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    scoped=replace(scoped,qualification_pin=replace(scoped.qualification_pin,**{field:'f'*64}))
    with pytest.raises(AdoptionInvalid,match='QUALIFICATION_MISMATCH'):
        with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen):pass
    assert not reader.reads and not reader.opened and not events


def test_replay_refuses_loader_operator_inventory_drift_before_pixels(tmp_path):
    from photo_worker.v2.render_providers import RenderLayerProviders
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    original=arguments['load_providers']
    def changed(*args):
        value=original(*args)
        value['render_proposal']=RenderLayerProviders(())
        value['operators']=(object(),)
        return value
    arguments['load_providers']=changed
    with pytest.raises(AdoptionInvalid,match='REPLAY_PROVIDERS_INVALID'):
        with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen):
            raise AssertionError('Drifted provider inventory executed')
    assert reader.opened==0 and 'base-open' not in events
    assert events[-1]=='scratch-close'
    assert not list(tmp_path.rglob('*.npy'))


def test_replay_accepts_one_consistent_provider_inventory(tmp_path):
    from photo_worker.v2.render_providers import RenderLayerProviders
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    original=arguments['load_providers']
    def installed(*args):
        value=original(*args)
        providers=RenderLayerProviders(())
        value['render_proposal']=providers
        value['operators']=providers.operators
        return value
    arguments['load_providers']=installed
    with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen) as expected:
        np.testing.assert_array_equal(expected.rgb,.5)
    assert reader.opened==reader.closed==1 and events[-1]=='scratch-close'


def test_shared_assembly_drives_real_authored_replay(tmp_path):
    from photo_worker.v2.render_providers import RenderProviderAssembly
    reader,scoped,frozen,arguments,events=setup(tmp_path)
    original=arguments['load_providers']
    def load(artifacts,inputs,scope):
        supplied=original(artifacts,inputs,scope)
        def forbidden_measure(*args):
            raise AssertionError('Replay must not trust result measurement')
        assembly=RenderProviderAssembly(
            base_factory=lambda *args:supplied['render_base'],
            layer_factory=lambda *args:(),
            override_factory=lambda *args:supplied['scalar_overrides'],
            measure_factory=forbidden_measure,preparation=supplied['preparation'])
        return assembly.replay(artifacts,inputs,scope)
    arguments['load_providers']=load
    with ProfiledRenderReplay(**arguments)(RenderReplayReader(reader),scoped,frozen) as expected:
        np.testing.assert_array_equal(expected.rgb,.5)
    assert reader.opened==reader.closed==1 and events[-1]=='scratch-close'
