from hashlib import sha256
from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.v2.interchange_export_style import validate_export_style


def style_fixture():
    root = Path(__file__).resolve().parents[2]
    value = parse_json((root / 'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    value['quality']['status'] = 'needs_review'
    return value


def test_export_style_preserves_complete_processing_document():
    value = style_fixture();raw = canonical_bytes(value)
    models = {model['id']: model['weight_id'] for model in value['models']}
    style = validate_export_style(raw, sha256(raw).hexdigest(), models)
    assert canonical_bytes(style) == raw


@pytest.mark.parametrize('mode', ['hash', 'canonical', 'example', 'curve', 'unknown', 'model'])
def test_export_style_rejects_substitution_and_invalid_processing(mode):
    value = style_fixture()
    models = {model['id']: model['weight_id'] for model in value['models']}
    if mode == 'example': value['quality']['status'] = 'example'
    if mode == 'curve': value['global']['master_curve'] = [1, 0, 0, 0, 0, 0, 0]
    if mode == 'unknown': value['unchecked_processing'] = True
    if mode == 'model':
        value['models'].append({'id': 'uninstalled', 'weight_id': 'foreign'})
    raw = canonical_bytes(value)
    if mode == 'canonical': raw += b'\n'
    digest = 'a' * 64 if mode == 'hash' else sha256(raw).hexdigest()
    with pytest.raises(ValueError): validate_export_style(raw, digest, models)


def test_export_override_preserves_absolute_modules():
    from photo_worker.v2.interchange_export_style import validate_export_override
    value = {'schema_version': 1, 'input_sha256': 'a' * 64,
             'modules': {'strength': 0.7}, 'revision': 1}
    raw = canonical_bytes(value)
    result = validate_export_override(raw, sha256(raw).hexdigest(), {})
    assert result.modules.strength == 0.7
    assert result.input_sha256 == value['input_sha256']
    assert canonical_bytes(value) == raw


def test_export_override_qualifies_manual_geometry():
    from uuid import uuid4
    from photo_worker.contracts.images import Canvas, TransformChain
    from photo_worker.v2.interchange_export_style import validate_export_override
    polygon = {'kind': 'polygon', 'sequence': 0, 'mode': 'protect',
               'points': [{'x': 0.0, 'y': 0.0}, {'x': 1.0, 'y': 0.0}, {'x': 0.0, 'y': 1.0}]}
    canvas = Canvas(width=32, height=24, orientation=1, space_id='canonical', source_sha256='a' * 64)
    face = {'object_id': str(uuid4()), 'points': [{'x': 0.5, 'y': 0.5, 'confidence': 1.0}] * 68,
            'canvas': canvas.model_dump(mode='json'), 'transform_chain': TransformChain.create([]).model_dump(mode='json')}
    value = {'schema_version': 1, 'input_sha256': 'a' * 64, 'modules': {'strength': 0.7},
             'protected_polygons': [polygon], 'manual_landmarks': face, 'revision': 1}
    def qualify():
        raw = canonical_bytes(value)
        return validate_export_override(raw, sha256(raw).hexdigest(), {})
    assert qualify().manual_landmarks is not None
    for change in ('mode', 'sequence', 'point', 'landmark_source'):
        altered = parse_json(canonical_bytes(value))
        if change == 'mode': altered['protected_polygons'][0]['mode'] = 'add'
        if change == 'sequence': altered['protected_polygons'].append(altered['protected_polygons'][0])
        if change == 'point': altered['protected_polygons'][0]['points'][0]['x'] = 2
        if change == 'landmark_source': altered['manual_landmarks']['canvas']['source_sha256'] = 'b' * 64
        raw = canonical_bytes(altered)
        with pytest.raises(ValueError): validate_export_override(raw, sha256(raw).hexdigest(), {})


@pytest.mark.parametrize('mode', ['hash', 'canonical', 'strength', 'module',
                                 'empty', 'revision', 'unknown', 'policy'])
def test_export_override_refuses_invalid_replacement_modules(mode):
    from photo_worker.v2.interchange_export_style import validate_export_override
    value = {'schema_version': 1, 'input_sha256': 'a' * 64,
             'modules': {'strength': 0.7}, 'revision': 1}
    policy = {}
    if mode == 'strength': value['modules']['strength'] = 1.1
    if mode == 'module': value['modules'] = {'unchecked': {}}
    if mode == 'empty': value['modules'] = {}
    if mode == 'revision': value['revision'] = 0
    if mode == 'unknown': value['unchecked'] = True
    if mode == 'policy': policy = {'model': 4}
    raw = canonical_bytes(value)
    if mode == 'canonical': raw += b'\n'
    digest = 'b' * 64 if mode == 'hash' else sha256(raw).hexdigest()
    with pytest.raises(ValueError): validate_export_override(raw, digest, policy)


def composition_fixture():
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
    value = style_fixture()
    source = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(), sha256='d' * 64),
        canvas=Canvas(width=32, height=24, orientation=1, space_id='canonical', source_sha256='a' * 64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1', icc_sha256=value['color']['icc_sha256'], encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    def pin(body):
        raw = canonical_bytes(body)
        return {'sha256': sha256(raw).hexdigest(), 'body': parse_json(raw, max_bytes=8 << 20)}
    patches = [{'schema_version': 1, 'input_sha256': 'a' * 64,
                'modules': {'strength': strength}, 'revision': index + 1}
               for index, strength in enumerate([0.2, 0.8])]
    return {'schema_version': 1, 'source': pin(source), 'base': pin(value),
            'overrides': [pin(patch) for patch in patches]}, {m['id']: m['weight_id'] for m in value['models']}


def test_export_composition_preserves_order_and_authored_bytes():
    from photo_worker.v2.interchange_export_style import validate_export_composition
    value, models = composition_fixture()
    models = {'style_weights': models, 'operation_locks': {'mobilesam': 'a' * 64}}
    raw = canonical_bytes(value)
    merged = validate_export_composition(raw, sha256(raw).hexdigest(), models)
    assert merged.strength == 0.8
    assert canonical_bytes(value) == raw
    value['overrides'].reverse()
    reversed_raw = canonical_bytes(value)
    assert validate_export_composition(reversed_raw, sha256(reversed_raw).hexdigest(), models).strength == 0.2


@pytest.mark.parametrize('mode', ['outer_hash', 'document_hash', 'source', 'icc', 'unknown', 'override_source', 'count'])
def test_export_composition_rejects_substituted_or_incoherent_inputs(mode):
    from photo_worker.v2.interchange_export_style import validate_export_composition
    value, models = composition_fixture()
    models = {'style_weights': models, 'operation_locks': {'mobilesam': 'a' * 64}}
    if mode == 'document_hash': value['base']['sha256'] = 'b' * 64
    if mode in {'source', 'icc', 'override_source'}:
        target = value['overrides'][0] if mode == 'override_source' else value['source']
        if mode == 'source': target['body']['dtype'] = 'uint16'
        if mode == 'icc': target['body']['color']['icc_sha256'] = 'b' * 64
        if mode == 'override_source': target['body']['input_sha256'] = 'b' * 64
        target['sha256'] = sha256(canonical_bytes(target['body'])).hexdigest()
    if mode == 'unknown': value['unchecked'] = True
    if mode == 'count': value['overrides'] *= 65
    raw = canonical_bytes(value)
    digest = 'b' * 64 if mode == 'outer_hash' else sha256(raw).hexdigest()
    with pytest.raises(ValueError): validate_export_composition(raw, digest, models)
