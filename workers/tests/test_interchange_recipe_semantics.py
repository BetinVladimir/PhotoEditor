import json
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.override import OverrideV1, OverrideModules
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_recipe_semantics import PreparedRecipeDocumentQualifier
from test_interchange_import_stage import adopted_fixture


def semantics_fixture(runtime=lambda *args: True, confirmation=None):
    _, sources, _, raw = adopted_fixture()
    image = WorkingImage.model_validate_json(raw)
    source = sources.entries[0]
    binding = VersionBinding(asset_version_id=source.asset_version_id, source=source.master,
                             canvas_hash=canonical_hash(image.canvas))
    qualifier = PreparedRecipeDocumentQualifier(canonical_images=[(binding, image)],
        work_icc_sha=image.color.icc_sha256, qualify_runtime=runtime,
        qualify_confirmation=confirmation)
    return qualifier, binding, image


def test_import_semantics_owns_canonical_images_and_preserves_override_source():
    qualifier, binding, image = semantics_fixture()
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
                          modules=OverrideModules(strength=0.5), revision=1)
    value = json.loads(canonical_bytes(override))
    assert qualifier('Override', value, binding) is True
    value['input_sha256'] = '0'*64
    with pytest.raises(ValueError, match='SOURCE_CHANGED'):
        qualifier('Override', value, binding)
    image.canvas.source_sha256 = '0'*64
    assert qualifier('Override', json.loads(canonical_bytes(override)), binding) is True


def test_import_semantics_rejects_mask_geometry_and_foreign_binding():
    qualifier, binding, image = semantics_fixture()
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64), canvas=image.canvas,
        transform_chain=image.transform_chain, object_id=None, selector='manual', confidence=1,
        model_lock_id=None, protected=False)
    assert qualifier('MaskRef', json.loads(canonical_bytes(mask)), binding) is True
    mask.canvas.source_sha256 = '0'*64
    with pytest.raises((AdoptionInvalid, ValueError)):
        qualifier('MaskRef', json.loads(canonical_bytes(mask)), binding)
    binding.asset_version_id = uuid4()
    with pytest.raises(AdoptionInvalid, match='BINDING'):
        qualifier('WorkingImage', json.loads(canonical_bytes(image)), binding)


@pytest.mark.parametrize('runtime_result', [False, None, 1])
def test_import_semantics_requires_exact_runtime_qualification(runtime_result):
    qualifier, binding, image = semantics_fixture(lambda *args: runtime_result)
    with pytest.raises(AdoptionInvalid, match='RUNTIME'):
        qualifier('WorkingImage', json.loads(canonical_bytes(image)), binding)


def test_import_semantics_requires_installed_icc_and_forbids_builtin_override():
    _, binding, image = semantics_fixture()
    with pytest.raises(ValueError, match='CANONICAL_IMAGE'):
        PreparedRecipeDocumentQualifier(canonical_images=[(binding, image)], work_icc_sha='0'*64,
            qualify_runtime=lambda *args: True)
    with pytest.raises(ValueError, match='VALIDATORS'):
        PreparedRecipeDocumentQualifier(canonical_images=[(binding, image)], work_icc_sha=image.color.icc_sha256,
            qualify_runtime=lambda *args: True, extra_validators={'MaskRef': lambda value: value})


@pytest.mark.parametrize('change', ['none', 'foreign', 'duplicate_identity', 'duplicate_ordinal', 'missing_target'])
def test_import_semantics_qualifies_complete_shared_correction_plan(change):
    from hashlib import sha256
    from photo_worker.operations.session.contracts import CorrectionPlan, CorrectionItem, CorrectionVector
    _, first, image = semantics_fixture()
    second_image = image.model_copy(deep=True)
    second_image.pixels = ArtifactRef(artifact_id=uuid4(), sha256='b'*64)
    second = VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(second_image)).hexdigest()), canvas_hash=canonical_hash(second_image.canvas))
    qualifier = PreparedRecipeDocumentQualifier(canonical_images=[(first, image), (second, second_image)],
        work_icc_sha=image.color.icc_sha256, qualify_runtime=lambda *args: True)
    delta = CorrectionVector(ev=0, log_r=0, log_g=0, log_b=0, saturation_delta=0, retouch_delta=0)
    items = [CorrectionItem(binding=first, ordinal=0, delta=delta, disposition='no_change', reason=None),
             CorrectionItem(binding=second, ordinal=1, delta=delta, disposition='no_change', reason=None)]
    if change == 'foreign':
        items[1].binding = second.model_copy(deep=True);items[1].binding.asset_version_id = uuid4()
    elif change == 'duplicate_identity':
        items[1].binding = first.model_copy(deep=True)
    elif change == 'duplicate_ordinal':
        items[1].ordinal = 0
    elif change == 'missing_target':
        items = items[1:]
    plan = CorrectionPlan(schema_version=1, snapshot_digest='f'*64, items=items)
    if change == 'none':
        assert qualifier('CorrectionPlan', json.loads(canonical_bytes(plan)), first) is True
    else:
        with pytest.raises(AdoptionInvalid, match='CORRECTION'):
            qualifier('CorrectionPlan', json.loads(canonical_bytes(plan)), first)


def test_runtime_callback_cannot_replace_qualified_mask_before_array_registration():
    captured = []
    def runtime(contract, typed, binding):
        typed.artifact.sha256 = '0' * 64
        typed.canvas.source_sha256 = '0' * 64
        binding.asset_version_id = uuid4()
        return True
    qualifier, binding, image = semantics_fixture(runtime)
    class Arrays:
        def register_mask(self, registered_binding, mask):
            captured.append((registered_binding.model_copy(deep=True), mask.model_copy(deep=True)))
            registered_binding.asset_version_id = uuid4()
            mask.canvas.source_sha256 = '1' * 64
    qualifier.arrays = Arrays()
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64), canvas=image.canvas,
        transform_chain=image.transform_chain, object_id=None, selector='manual', confidence=1,
        model_lock_id=None, protected=False)
    original = canonical_bytes(mask)
    assert qualifier('MaskRef', json.loads(original), binding) is True
    assert captured[0][0] == binding
    assert canonical_bytes(captured[0][1]) == original
    assert canonical_bytes(mask) == original
    assert qualifier('MaskRef', json.loads(original), binding) is True


def test_extra_validator_cannot_rewrite_caller_or_its_qualification_baseline():
    qualifier, binding, _ = semantics_fixture()
    def validator(value):
        value['strength'] = 0.9
        return value
    qualifier.extra['Params'] = validator
    value = {'schema_version': 1, 'strength': 0.5}
    with pytest.raises(AdoptionInvalid, match='FIELDS'):
        qualifier('Params', value, binding)
    assert value == {'schema_version': 1, 'strength': 0.5}


def test_runtime_mutation_does_not_escape_to_extra_document_caller():
    def runtime(contract, typed, binding):
        typed['strength'] = 0.9
        return True
    qualifier, binding, _ = semantics_fixture(runtime)
    qualifier.extra['Params'] = lambda value: value
    value = {'schema_version': 1, 'strength': 0.5}
    assert qualifier('Params', value, binding) is True
    assert value == {'schema_version': 1, 'strength': 0.5}


@pytest.mark.parametrize('operation',['hair','clothing'])
def test_import_local_params_require_complete_schema_and_current_binding(operation):
    from photo_worker.contracts.portrait_local import PortraitHairParams,PortraitClothingParams
    qualifier,binding,_=semantics_fixture()
    values=dict(schema_version=1,base=binding,strength=.5,mask=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),
        protected=ArtifactRef(artifact_id=uuid4(),sha256='d'*64),hue_delta_degrees=0,saturation_factor=1)
    if operation=='hair':typed=PortraitHairParams(**values,detail=.4)
    else:typed=PortraitClothingParams(**values,wrinkle_strength=.4)
    value=json.loads(canonical_bytes(typed))
    assert qualifier('Params',value,binding) is True
    value['base']['asset_version_id']=str(uuid4())
    with pytest.raises(AdoptionInvalid,match='PARAMS_SOURCE'):qualifier('Params',value,binding)
    value=json.loads(canonical_bytes(typed));value['unexpected']=1
    with pytest.raises(ValueError):qualifier('Params',value,binding)


@pytest.mark.parametrize('decision', ['accepted_candidate_ids', 'rejected_candidate_ids'])
def test_import_override_foreign_review_rejected_before_runtime(decision):
    calls = []
    qualifier, binding, image = semantics_fixture(lambda *args: calls.append(args) or True)
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
        modules=OverrideModules(strength=0.5), revision=1,
        **{decision: ['foreign-candidate']})
    with pytest.raises(AdoptionInvalid, match='FOREIGN_REVIEW'):
        qualifier('Override', json.loads(canonical_bytes(override)), binding)
    assert calls == []


@pytest.mark.parametrize('confirmed', [False, True])
def test_import_override_requires_fresh_mask_confirmation(confirmed):
    from photo_worker.contracts.override import OverrideMaskBinding
    calls = []
    qualifier, binding, image = semantics_fixture(lambda *args: calls.append(args) or True)
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64),
        canvas=image.canvas.model_copy(deep=True),
        transform_chain=image.transform_chain.model_copy(deep=True), object_id=None,
        selector='manual', confidence=1, model_lock_id=None, protected=False)
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
        modules=OverrideModules(strength=0.5), revision=1,
        mask_refs=[OverrideMaskBinding(binding_id='teeth', purpose='portrait_teeth',
            mask=mask, confirmed=confirmed, strength=0.5)])
    value = json.loads(canonical_bytes(override))
    before = canonical_bytes(value)
    if confirmed:
        with pytest.raises(AdoptionInvalid, match='MASK_CONFIRMATION'):
            qualifier('Override', value, binding)
        assert calls == []
    else:
        assert qualifier('Override', value, binding) is True
        assert len(calls) == 1
    assert canonical_bytes(value) == before


def test_import_person_confirmation_rechecked_after_runtime_and_rejects_revoke():
    from photo_worker.contracts.override import OverrideMaskBinding
    observations = []
    def consent(mask, binding):
        observations.append(('consent', mask.artifact.artifact_id, binding.asset_version_id))
        return len(observations) == 1
    def runtime(*_):
        observations.append(('runtime', None, None))
        return True
    qualifier, binding, image = semantics_fixture(runtime, consent)
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64),
        canvas=image.canvas.model_copy(deep=True),
        transform_chain=image.transform_chain.model_copy(deep=True), object_id=None,
        selector='person', confidence=1, model_lock_id='d'*64, protected=True)
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
        modules=OverrideModules(strength=0.5), revision=1,
        mask_refs=[OverrideMaskBinding(binding_id='protect', purpose='protected',
            mask=mask, confirmed=True, strength=1)])
    with pytest.raises(AdoptionInvalid, match='MASK_CONFIRMATION'):
        qualifier('Override', json.loads(canonical_bytes(override)), binding)
    assert [item[0] for item in observations] == ['consent', 'runtime', 'consent']


def test_import_person_confirmation_rechecked_after_array_phase():
    from photo_worker.contracts.override import OverrideMaskBinding
    checks = []
    def consent(mask, binding):
        checks.append((mask.artifact.artifact_id, binding.asset_version_id))
        return len(checks) < 3
    qualifier, binding, image = semantics_fixture(confirmation=consent)
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64),
        canvas=image.canvas.model_copy(deep=True),
        transform_chain=image.transform_chain.model_copy(deep=True), object_id=None,
        selector='person', confidence=1, model_lock_id='d'*64, protected=True)
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
        modules=OverrideModules(strength=0.5), revision=1,
        mask_refs=[OverrideMaskBinding(binding_id='protect', purpose='protected',
            mask=mask, confirmed=True, strength=1)])
    with pytest.raises(AdoptionInvalid, match='MASK_CONFIRMATION'):
        qualifier('Override', json.loads(canonical_bytes(override)), binding)
    assert len(checks) == 3


@pytest.mark.parametrize('schema', ['DenoiseParams','DustRepairParams','PortraitParams'])
@pytest.mark.parametrize('mode', ['valid','source','runtime','extra'])
def test_exact_operator_schema_checks_source_and_retains_runtime_authority(schema, mode):
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.contracts.dust import DustRepairParams
    from photo_worker.contracts.portrait_detail import PortraitParams
    calls=[]
    qualifier,binding,_=semantics_fixture(lambda *args: (calls.append(args), mode!='runtime')[1])
    if schema=='DenoiseParams':
        params=DenoiseParams(image=binding.source,strength=.5,model_lock_id='a'*64)
    elif schema=='DustRepairParams': params=DustRepairParams(image=binding.source)
    else: params=PortraitParams(image=binding.source,resolved=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),regions=[],protected=[])
    value=json.loads(canonical_bytes(params))
    if mode=='source': value['image']['artifact_id']=str(uuid4())
    if mode=='extra': value['unknown']=True
    if mode=='valid':
        assert qualifier.qualify_params(schema,value,binding) is True
        assert type(calls[0][1]).__name__==schema and calls[0][0]=='Params'
    else:
        with pytest.raises(ValueError): qualifier.qualify_params(schema,value,binding)
        assert len(calls)==(1 if mode=='runtime' else 0)


def test_explicit_params_schema_never_uses_field_heuristics_for_another_model():
    qualifier,binding,_=semantics_fixture()
    with pytest.raises(ValueError):
        qualifier.qualify_params('PortraitHairParams',{'image':binding.source.model_dump(mode='json')},binding)


@pytest.mark.parametrize('mode', ['valid','image','pixels','canvas','runtime'])
def test_defect_candidates_keep_exact_source_geometry_and_runtime_authority(mode):
    from photo_worker.contracts.defects import DefectCandidates
    calls=[]
    qualifier,binding,image=semantics_fixture(lambda *args:(calls.append(args),mode!='runtime')[1])
    proposal=DefectCandidates(image=binding.source,source_pixels=image.pixels,
        label_map=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),canvas=image.canvas,
        transform_chain=image.transform_chain,candidates=[])
    if mode=='image':proposal.image=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    if mode=='pixels':proposal.source_pixels=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    if mode=='canvas':proposal.canvas.source_sha256='f'*64
    value=json.loads(canonical_bytes(proposal))
    if mode=='valid':assert qualifier('DefectCandidates',value,binding) is True
    else:
        with pytest.raises(ValueError):qualifier('DefectCandidates',value,binding)
    assert len(calls)==(1 if mode in ('valid','runtime') else 0)


@pytest.mark.parametrize('mode', ['valid','source','runtime'])
def test_defect_review_never_substitutes_for_current_runtime_authority(mode):
    from photo_worker.contracts.defects import DefectReview
    calls=[]
    qualifier,binding,_=semantics_fixture(lambda *args:(calls.append(args),mode!='runtime')[1])
    review=DefectReview(proposal=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),image=binding.source,
        label_map=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),revision=1,
        accepted_candidate_ids=['c'*64],rejected_candidate_ids=[])
    if mode=='source':review.image=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    if mode=='valid':
        assert qualifier('DefectReview',json.loads(canonical_bytes(review)),binding) is True
        assert calls[0][1].accepted_candidate_ids==['c'*64]
    else:
        with pytest.raises(ValueError):qualifier('DefectReview',json.loads(canonical_bytes(review)),binding)
    assert len(calls)==(0 if mode=='source' else 1)


@pytest.mark.parametrize('allowed', [True,False,1])
def test_dust_candidates_require_explicit_current_runtime_authority(allowed):
    from photo_worker.contracts.dust import DustCandidates, SensorCrop
    calls=[]
    qualifier,binding,_=semantics_fixture(lambda *args:(calls.append(args),allowed)[1])
    proposal=DustCandidates(frames=[ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for _ in range(5)],
        camera_id='camera_1',sensor_width=16,sensor_height=16,
        raw_crop=SensorCrop(x=0,y=0,width=16,height=16),candidates=[],warnings=['no_repeated_dust'])
    if allowed is True:
        assert qualifier('DustCandidates',json.loads(canonical_bytes(proposal)),binding) is True
    else:
        with pytest.raises(AdoptionInvalid,match='RUNTIME'):
            qualifier('DustCandidates',json.loads(canonical_bytes(proposal)),binding)
    assert len(calls)==1 and isinstance(calls[0][1],DustCandidates)


@pytest.mark.parametrize('allowed', [True, False])
def test_resolved_override_masks_register_only_after_runtime_authority(allowed):
    from pathlib import Path
    from photo_worker.contracts.override import OverrideMaskBinding
    qualifier, binding, image = semantics_fixture(lambda *args: allowed)
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64), canvas=image.canvas,
        transform_chain=image.transform_chain, object_id=None, selector='manual', confidence=1,
        model_lock_id=None, protected=True)
    override = OverrideV1(schema_version=1, input_sha256=image.canvas.source_sha256,
        modules=OverrideModules(strength=.5), revision=1,
        mask_refs=[OverrideMaskBinding(binding_id='protection', purpose='protected', mask=mask)])
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/resolved-binding-python.json'
    value = json.loads(fixture.read_bytes())
    value['params']['image'] = json.loads(canonical_bytes(binding.source))
    value['params']['override'] = json.loads(canonical_bytes(override))
    value['hash'] = canonical_hash({key: item for key, item in value.items() if key != 'hash'})
    registered = []
    class Arrays:
        def register_mask(self, registered_binding, registered_mask):
            registered.append((registered_binding, registered_mask))
    qualifier.arrays = Arrays()
    if allowed:
        assert qualifier('ResolvedStyle', value, binding) is True
        assert registered == [(binding, mask)]
        value['params']['override']['mask_refs'].clear()
        assert registered[0][1] == mask
    else:
        with pytest.raises(AdoptionInvalid, match='RUNTIME'):
            qualifier('ResolvedStyle', value, binding)
        assert registered == []


@pytest.mark.parametrize('mode', ['valid', 'foreign', 'missing', 'runtime'])
def test_stats_document_requires_exact_source_and_runtime(mode):
    qualifier, binding, image = semantics_fixture(lambda *args: mode != 'runtime')
    value = dict(feature_version='stats20_v1', values=[0.] * 20,
        coverage_bins=[False] * 512, mask_confidence=1., warnings=[],
        source_image=json.loads(canonical_bytes(binding.source)))
    if mode == 'foreign': value['source_image']['artifact_id'] = str(uuid4())
    if mode == 'missing': value['source_image'] = None
    if mode == 'valid': assert qualifier('Stats20', value, binding) is True
    else:
        with pytest.raises(AdoptionInvalid): qualifier('Stats20', value, binding)


@pytest.mark.parametrize('authority', [True, False, 1])
def test_resolved_lut_registration_requires_strict_runtime_approval(authority):
    from pathlib import Path
    qualifier, binding, image = semantics_fixture(lambda *args: authority)
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/resolved-binding-python.json'
    value = json.loads(fixture.read_bytes())
    value.update(mode='group_fixed', reasons=[], needs_review=False)
    value['params'].update(image=json.loads(canonical_bytes(binding.source)), group_prototype_id='group')
    value['absolute_lut'] = dict(artifact=dict(artifact_id=str(uuid4()), sha256='a' * 64),
        size=65, dtype='float32', order='RGB_C', profile_id='work_rec2020_gamma24_v1')
    value['hash'] = canonical_hash({key: item for key, item in value.items() if key != 'hash'})
    registered = []
    class Arrays:
        def register_lut(self, owned_binding, resolved): registered.append((owned_binding, resolved))
    qualifier.arrays = Arrays()
    if authority is True:
        assert qualifier('ResolvedStyle', value, binding) is True
        assert len(registered) == 1 and registered[0][0] == binding
        value['absolute_lut']['size'] = 33
        assert registered[0][1].absolute_lut.size == 65
    else:
        with pytest.raises(AdoptionInvalid, match='RUNTIME'): qualifier('ResolvedStyle', value, binding)
        assert registered == []

@pytest.mark.parametrize('mode', ['valid', 'source', 'runtime'])
def test_upscale_prepared_params_require_current_source_and_runtime(mode):
    from photo_worker.contracts.restoration import UpscaleParams
    qualifier, binding, image = semantics_fixture(lambda *args: mode != 'runtime')
    params = UpscaleParams(image=binding.source, scale=2, model_id='realesrgan_x2', model_lock_id='a' * 64)
    value = json.loads(canonical_bytes(params))
    if mode == 'source': value['image']['artifact_id'] = str(uuid4())
    if mode == 'valid':
        assert qualifier.qualify_params('UpscaleParams', value, binding) is True
    else:
        with pytest.raises(AdoptionInvalid): qualifier.qualify_params('UpscaleParams', value, binding)


@pytest.mark.parametrize('mode', ['valid', 'source', 'canvas', 'selector', 'runtime'])
def test_stats_params_source_mask_runtime_and_array_registration(mode):
    from photo_worker.contracts.statistics import StatsParams
    qualifier, binding, image = semantics_fixture(lambda *args: mode != 'runtime')
    mask = MaskRef(artifact=ArtifactRef(artifact_id=uuid4(), sha256='c'*64),
        canvas=image.canvas, transform_chain=image.transform_chain, selector='person',
        confidence=.8, model_lock_id='a'*64, protected=True)
    value = json.loads(canonical_bytes(StatsParams(image=binding.source, person=mask)))
    if mode == 'source': value['image']['artifact_id'] = str(uuid4())
    if mode == 'canvas': value['person']['canvas']['source_sha256'] = 'f'*64
    if mode == 'selector': value['person']['selector'] = 'manual'
    registered = []
    class Arrays:
        def register_mask(self, owned_binding, owned_mask):
            registered.append((owned_binding, owned_mask))
    qualifier.arrays = Arrays()
    if mode == 'valid':
        assert qualifier.qualify_params('StatsParams', value, binding) is True
        assert registered == [(binding, mask)]
        value['person']['confidence'] = .1
        assert registered[0][1].confidence == .8
    else:
        with pytest.raises((AdoptionInvalid, ValueError)):
            qualifier.qualify_params('StatsParams', value, binding)
        assert registered == []
