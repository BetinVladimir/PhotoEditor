from copy import deepcopy
import json
from uuid import UUID
import pytest
from photo_worker.operations.learning.contracts import RenderCase, TrainingExample


def decode(model, value):
    # These fixtures represent wire JSON UUID strings, not Python-mode UUIDs.
    return model.model_validate_json(json.dumps(value, default=str))


def fixture():
    ref = dict(artifact_id=str(UUID(int=1)),sha256='a'*64)
    return dict(binding=dict(asset_version_id=str(UUID(int=2)),source=deepcopy(ref),canvas_hash='b'*64),
        working_image=deepcopy(ref),base_recipe=deepcopy(ref),approved_recipe=deepcopy(ref),
        base_document=deepcopy(ref),approved_document=deepcopy(ref),descriptor_shard=deepcopy(ref),
        descriptor_analysis_id=str(UUID(int=3)),descriptor_ordinal=0,protected=deepcopy(ref),
        faces=deepcopy(ref),approval_id=str(UUID(int=4)),source_lineage_sha='c'*64)


@pytest.mark.parametrize('field', ['working_image','base_recipe','approved_recipe',
    'base_document','approved_document','descriptor_shard','protected','faces'])
def test_zero_root_is_refused(field):
    case = fixture()
    case[field]['artifact_id'] = str(UUID(int=0))
    with pytest.raises(ValueError,match='INVALID_HOLDOUT_RENDER_IDENTITY'):
        decode(RenderCase, case)


@pytest.mark.parametrize('field', ['approval_id','descriptor_analysis_id','asset_version_id'])
def test_zero_render_identity_is_refused(field):
    case = fixture()
    target = case['binding'] if field == 'asset_version_id' else case
    target[field] = str(UUID(int=0))
    with pytest.raises(ValueError,match='INVALID_HOLDOUT_RENDER_IDENTITY'):
        decode(RenderCase, case)


def test_foreign_working_image_is_refused():
    case = fixture()
    case['working_image']['artifact_id'] = str(UUID(int=99))
    with pytest.raises(ValueError,match='INVALID_HOLDOUT_RENDER_IDENTITY'):
        decode(RenderCase, case)


@pytest.mark.parametrize('field', ['event_id','session_id','subject_group_id'])
def test_zero_example_identity_is_refused(field):
    example = dict(event_id=UUID(int=5),session_id=UUID(int=6),subject_group_id=UUID(int=7),
        split='holdout',features=[0]*12,target=dict(ev=0,log_r=0,log_g=0,log_b=0,
        saturation_delta=0,retouch_delta=0),render_case=fixture())
    assert decode(TrainingExample, example)
    example[field] = UUID(int=0)
    with pytest.raises(ValueError,match='INVALID_TRAINING_EXAMPLE_IDENTITY'):
        decode(TrainingExample, example)


def test_background_pin_is_optional_and_preserved_in_render_case():
    case = fixture()
    assert 'background_asset' not in decode(RenderCase, case).model_dump(mode='json', by_alias=True)
    pin = dict(schema_version=1, portable_asset_id='installed', style_version_id=str(UUID(int=10)),
        style=dict(artifact_id=str(UUID(int=11)), sha256='d'*64),
        source=dict(artifact_id=str(UUID(int=12)), sha256='e'*64), expected_bytes=10,
        media_type='image/png', suffix='.png', assume_profile=None, work_icc_sha='f'*64)
    case['background_asset'] = pin
    decoded = decode(RenderCase, case)
    assert decoded.model_dump(mode='json', by_alias=True)['background_asset'] == pin
    pin['source']['artifact_id'] = str(UUID(int=0))
    with pytest.raises(ValueError):decode(RenderCase, case)
