from copy import deepcopy
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_descriptor_occurrences import PreparedDescriptorOccurrenceQualifier


def fixture():
    def ref(label):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(label.encode()).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(), source=ref('source'), canvas_hash='c' * 64)
    faces = ref('faces')
    frame = dict(schema_version=1, binding=binding, ordinal=7, captured_at=None,
        camera_hash=None, scene_embedding=[0.] * 384, luma_quantiles=[.1,.2,.3,.4,.5],
        neutral_rgb=None, sharpness=1., clipping_fraction=0., person_fraction=0., faces=faces)
    raw = canonical_bytes(dict(schema_version=1, snapshot_digest='d' * 64, frames=[frame]))
    descriptor = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    pin = dict(root=ref('root'), descriptor=descriptor, descriptor_body=raw,
        selected_binding=binding, analysis_id=uuid4(), model_lock_sha256='e' * 64, selected_ordinal=7)
    occurrence = dict(reference=faces, contract='FaceObservations', parent=descriptor,
        parent_contract='DescriptorShard', path=('frames','*','faces'),
        parent_body=parse_json(raw), binding=binding)
    return with_root(pin), occurrence


def test_descriptor_occurrence_retains_original_frame_and_owned_pins():
    pin, occurrence = fixture(); calls = []
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin],
        qualify_current=lambda event, evidence: (calls.append((event,evidence)), True)[1],
        qualify_other=lambda event: False)
    pin['selected_ordinal'] = 99
    assert policy(occurrence) is True
    assert calls[0][1]['frame_ordinal'] == 7
    assert calls[0][1]['frame_binding'] == occurrence['binding']
    calls[0][1]['selected_binding'].canvas_hash = 'f' * 64
    assert policy(occurrence) is True
    assert calls[1][1]['selected_binding'].canvas_hash == 'c' * 64


@pytest.mark.parametrize('mode', ['parent', 'body', 'path', 'ref', 'contract', 'revoked'])
def test_descriptor_occurrence_rejects_substitution(mode):
    pin, occurrence = fixture()
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin],
        qualify_current=lambda event, evidence: mode != 'revoked', qualify_other=lambda event: False)
    if mode == 'parent':
        occurrence['parent'] = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    elif mode == 'body':
        occurrence['parent_body']['snapshot_digest'] = 'a' * 64
    elif mode == 'path':
        occurrence['path'] = ('frames','*','binding','source')
    elif mode == 'ref':
        occurrence['reference'] = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    elif mode == 'contract':
        occurrence['contract'] = 'MaskRef'
    with pytest.raises(AdoptionInvalid):
        policy(occurrence)


def test_descriptor_root_requires_exact_frozen_parent():
    pin, occurrence = fixture()
    pin = with_root(pin)
    root_raw = pin['root_body']
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin], qualify_current=lambda *args: True,
        qualify_other=lambda event: False)
    occurrence['parent_body'] = parse_json(root_raw)
    occurrence.update(reference=pin['descriptor'], contract='DescriptorShard', parent=pin['root'],
        parent_contract='FrozenRenderRecipe', path=('learning','descriptor'))
    assert policy(occurrence) is True
    changed = deepcopy(occurrence)
    changed['parent_body']['learning']['ordinal'] = 6
    with pytest.raises(AdoptionInvalid, match='ROOT_BYTES'):
        policy(changed)
    occurrence['parent_contract'] = 'EditingRecipeV3' 
    with pytest.raises(AdoptionInvalid, match='ROOT'):
        policy(occurrence)


@pytest.mark.parametrize('mode', ['ordinal', 'hash', 'analysis', 'lock', 'unknown'])
def test_descriptor_pin_installation_rejects_unqualified_pins(mode):
    pin, _ = fixture()
    if mode == 'ordinal': pin['selected_ordinal'] = 6
    elif mode == 'hash': pin['descriptor_body'] += b' '
    elif mode == 'analysis': pin['analysis_id'] = '00000000-0000-0000-0000-000000000000'
    elif mode == 'lock': pin['model_lock_sha256'] = 'bad'
    elif mode == 'unknown': pin['approved'] = True
    with pytest.raises(ValueError):
        PreparedDescriptorOccurrenceQualifier(pins=[pin], qualify_current=lambda *args: True,
            qualify_other=lambda event: False)


@pytest.mark.parametrize('role', ['pixels', 'alpha', 'face_source'])
@pytest.mark.parametrize('mode', ['valid', 'hash', 'ref', 'role', 'authority'])
def test_descriptor_descendants_preserve_frame_context(role, mode):
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.operations.session.contracts import DescriptorShard
    from test_interchange_recipe_semantics import semantics_fixture
    _, binding, image = semantics_fixture()
    image.alpha = ArtifactRef(artifact_id=uuid4(), sha256='b' * 64)
    raw = canonical_bytes(image)
    binding.source = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    binding.canvas_hash = canonical_hash(image.canvas)
    face_raw = canonical_bytes(dict(schema_version=1, binding=binding, faces=[]))
    faces = ArtifactRef(artifact_id=uuid4(), sha256=sha256(face_raw).hexdigest())
    pin, _ = fixture()
    shard = DescriptorShard.model_validate_json(pin['descriptor_body'])
    shard.frames[0].binding = binding
    shard.frames[0].faces = faces
    pin['selected_binding'] = binding
    pin['descriptor_body'] = canonical_bytes(shard)
    pin['descriptor'] = ArtifactRef(artifact_id=uuid4(), sha256=sha256(pin['descriptor_body']).hexdigest())
    pin = with_root(pin)
    calls = []
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin],
        qualify_current=lambda event, evidence: (calls.append(evidence), mode != 'authority')[1],
        qualify_other=lambda event: True)
    if role == 'face_source':
        event = dict(reference=binding.source, contract='WorkingImage', parent=faces,
            parent_contract='FaceObservations', path=('binding','source'),
            parent_body=parse_json(face_raw), binding=binding)
    else:
        event = dict(reference=image.pixels if role == 'pixels' else image.alpha,
            contract='WorkingPixels' if role == 'pixels' else 'WorkingAlpha',
            parent=binding.source, parent_contract='WorkingImage', path=(role,),
            parent_body=parse_json(raw), binding=binding)
    if mode == 'hash':
        event['parent_body']['schema_version'] = 99
    elif mode == 'ref':
        event['reference'] = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    elif mode == 'role':
        event['contract'] = 'MaskPixels'
    if mode == 'valid':
        assert policy(event) is True
        assert len(calls) == 1 and calls[0]['frame_ordinal'] == 7
        assert calls[0]['frame_binding'] == binding
    else:
        with pytest.raises(AdoptionInvalid):
            policy(event)


def with_root(pin):
    from photo_worker.v2.render_recipe import EditingRecipeV3, FrozenRenderRecipe, FrozenRenderLearning
    from photo_worker.operations.learning.contracts import FrozenRecipe
    from photo_worker.operations.session.contracts import CorrectionVector
    from photo_worker.contracts.base import canonical_hash
    style = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    version = uuid4()
    delta = CorrectionVector(ev=0, log_r=0, log_g=0, log_b=0, saturation_delta=0, retouch_delta=0)
    recipe = EditingRecipeV3(schema_version=3, source=pin['selected_binding'], base_style=style,
        session_corrections=None, learning_version_id=version, overrides=[], layer_stack=None,
        renderer_version='v2.2', parameter_delta=delta)
    frozen = FrozenRenderRecipe(schema_version=1, recipe=recipe, recipe_sha256=canonical_hash(recipe),
        scalars=FrozenRecipe(schema_version=1, source=pin['selected_binding'], base_style=style,
            effective_delta=delta, renderer_version='v2.2', icc_lock_sha='b' * 64,
            model_lock_sha=pin['model_lock_sha256']),
        learning=FrozenRenderLearning(version_id=version,
            model=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64), descriptor=pin['descriptor'],
            analysis_id=pin['analysis_id'], ordinal=7))
    root_raw = canonical_bytes(frozen)
    pin['root'] = ArtifactRef(artifact_id=uuid4(), sha256=sha256(root_raw).hexdigest())
    pin['root_body'] = root_raw
    return pin


@pytest.mark.parametrize('mode', ['missing', 'hash', 'analysis', 'model_lock', 'source'])
def test_descriptor_constructor_requires_matching_frozen_root_bytes(mode):
    pin, _ = fixture()
    if mode == 'missing':
        del pin['root_body']
    elif mode == 'hash':
        pin['root_body'] += b' '
    else:
        root = parse_json(pin['root_body'])
        if mode == 'analysis': root['learning']['analysis_id'] = str(uuid4())
        elif mode == 'model_lock': root['scalars']['model_lock_sha'] = 'a' * 64
        elif mode == 'source':
            root['recipe']['source']['asset_version_id'] = str(uuid4())
            root['scalars']['source'] = deepcopy(root['recipe']['source'])
            from photo_worker.contracts.base import canonical_hash
            root['recipe_sha256'] = canonical_hash(root['recipe'])
        pin['root_body'] = canonical_bytes(root)
        pin['root'] = ArtifactRef(artifact_id=pin['root'].artifact_id,
            sha256=sha256(pin['root_body']).hexdigest())
    with pytest.raises(ValueError):
        PreparedDescriptorOccurrenceQualifier(pins=[pin], qualify_current=lambda *args: True,
            qualify_other=lambda event: True)


def test_descriptor_root_rejects_wrong_path_even_with_exact_parent():
    pin, event = fixture()
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin], qualify_current=lambda *args: True,
        qualify_other=lambda event: True)
    event.update(reference=pin['descriptor'], contract='DescriptorShard', parent=pin['root'],
        parent_contract='FrozenRenderRecipe', parent_body=parse_json(pin['root_body']),
        path=('invented', 'descriptor'))
    with pytest.raises(AdoptionInvalid, match='ROOT'):
        policy(event)



def test_shared_descriptor_is_qualified_under_its_exact_frozen_parent():
    first, event = fixture()
    second = with_root(deepcopy(first))
    calls = []
    policy = PreparedDescriptorOccurrenceQualifier(pins=[first, second],
        qualify_current=lambda event, evidence: (calls.append(evidence), True)[1],
        qualify_other=lambda event: False)
    for pin in (first, second):
        calls.clear()
        root_event = dict(reference=pin['descriptor'], contract='DescriptorShard',
            parent=pin['root'], parent_contract='FrozenRenderRecipe',
            path=('learning','descriptor'), parent_body=parse_json(pin['root_body']),
            binding=pin['selected_binding'])
        assert policy(root_event) is True
        assert len(calls) == 1 and calls[0]['root'] == pin['root']
    calls.clear()
    assert policy(event) is True
    assert len(calls) == 2
    assert {item['root'].artifact_id for item in calls} == {first['root'].artifact_id, second['root'].artifact_id}


def test_shared_descriptor_requires_current_authority_for_every_root():
    first, event = fixture()
    second = with_root(deepcopy(first))
    policy = PreparedDescriptorOccurrenceQualifier(pins=[first, second],
        qualify_current=lambda event, evidence: evidence['root'] != second['root'],
        qualify_other=lambda event: True)
    with pytest.raises(AdoptionInvalid, match='AUTHORITY'):
        policy(event)


def test_descriptor_installation_rejects_conflicting_root_artifact_identity():
    first, _ = fixture()
    second = with_root(deepcopy(first))
    second['root'] = ArtifactRef(artifact_id=first['root'].artifact_id,
        sha256=second['root'].sha256)
    with pytest.raises(ValueError, match='PIN_CONFLICT'):
        PreparedDescriptorOccurrenceQualifier(pins=[first, second],
            qualify_current=lambda *args: True, qualify_other=lambda event: True)


@pytest.mark.parametrize('result', [False, None, 1, 'true'])
def test_descriptor_current_authority_requires_boolean_true(result):
    pin, event = fixture()
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin],
        qualify_current=lambda *args: result, qualify_other=lambda event: True)
    with pytest.raises(AdoptionInvalid, match='AUTHORITY'):
        policy(event)


@pytest.mark.parametrize('port', ['qualify_current', 'qualify_other', 'check_active'])
def test_descriptor_installation_requires_all_callable_ports(port):
    pin, _ = fixture()
    args = dict(pins=[pin], qualify_current=lambda *args: True,
        qualify_other=lambda event: True, check_active=lambda: None)
    args[port] = None
    with pytest.raises(ValueError, match='INSTALLATION'):
        PreparedDescriptorOccurrenceQualifier(**args)


def test_descriptor_authority_mutation_cannot_change_next_occurrence():
    pin, event = fixture()
    original = deepcopy(event)
    seen = []
    def authority(owned_event, evidence):
        seen.append(deepcopy(owned_event))
        owned_event['parent_body']['frames'].clear()
        evidence['descriptor_body'] = b'{}'
        return True
    policy = PreparedDescriptorOccurrenceQualifier(pins=[pin],
        qualify_current=authority, qualify_other=lambda event: False)
    assert policy(event) is True
    assert policy(event) is True
    assert event == original and seen == [original, original]
