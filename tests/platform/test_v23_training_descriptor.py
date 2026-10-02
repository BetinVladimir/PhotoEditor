from types import SimpleNamespace
from uuid import UUID, uuid4

import pytest

from photo_worker.operations.learning.descriptor import validate_training_descriptor
from photo_worker.operations.learning.model import scene12_features
from photo_worker.operations.session.contracts import DescriptorShard


def _case():
    ref = {'artifact_id': uuid4(), 'sha256': 'a' * 64}
    binding = {'asset_version_id': uuid4(), 'source': ref,
               'canvas_hash': 'b' * 64}
    faces = {'artifact_id': uuid4(), 'sha256': 'c' * 64}
    frame = {'schema_version': 1, 'binding': binding, 'ordinal': 0,
             'captured_at': None, 'camera_hash': None,
             'scene_embedding': [0.0] * 384,
             'luma_quantiles': [.1, .2, .3, .4, .5],
             'neutral_rgb': None, 'sharpness': 1.0,
             'clipping_fraction': .01, 'person_fraction': .2,
             'faces': faces}
    shard = DescriptorShard.model_validate({
        'schema_version': 1, 'snapshot_digest': 'd' * 64,
        'frames': [frame]})
    case = SimpleNamespace(descriptor_ordinal=0,
                           descriptor_analysis_id=uuid4(),
                           binding=shard.frames[0].binding,
                           faces=shard.frames[0].faces)
    features = scene12_features((.1, .2, .3, .4, .5), None, .01, 1, .2)
    return SimpleNamespace(render_case=case, features=list(features)), shard


def test_training_features_are_recomputed_from_pinned_shard():
    item, shard = _case()
    validate_training_descriptor(item, shard)
    item.features[0] = .9
    with pytest.raises(ValueError, match='TRAINING_FEATURES_MISMATCH'):
        validate_training_descriptor(item, shard)


def test_training_descriptor_binds_faces_and_analysis():
    item, shard = _case()
    item.render_case.faces = shard.frames[0].binding.source
    with pytest.raises(ValueError, match='TRAINING_DESCRIPTOR_MISMATCH'):
        validate_training_descriptor(item, shard)
    item.render_case.faces = shard.frames[0].faces
    item.render_case.descriptor_analysis_id = UUID(int=0)
    with pytest.raises(ValueError, match='TRAINING_DESCRIPTOR_MISMATCH'):
        validate_training_descriptor(item, shard)
