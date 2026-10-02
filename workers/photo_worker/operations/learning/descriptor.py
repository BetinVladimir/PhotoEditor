"""Verify that frozen scene12 features came from the pinned pre-edit shard."""
from __future__ import annotations

from math import isclose

from photo_worker.operations.session.contracts import DescriptorShard

from .contracts import TrainingExample
from .model import scene12_features


def validate_training_descriptor(item: TrainingExample,
                                 shard: DescriptorShard) -> None:
    case = item.render_case
    matched = [frame for frame in shard.frames
               if frame.ordinal == case.descriptor_ordinal and
               frame.binding == case.binding]
    if len(matched) != 1 or matched[0].faces != case.faces or \
            case.descriptor_analysis_id.int == 0:
        raise ValueError('TRAINING_DESCRIPTOR_MISMATCH')
    frame = matched[0]
    features = scene12_features(tuple(frame.luma_quantiles),
                                None if frame.neutral_rgb is None else
                                tuple(frame.neutral_rgb),
                                frame.clipping_fraction, frame.sharpness,
                                frame.person_fraction)
    if len(item.features) != 12 or any(
            not isclose(actual, expected, rel_tol=0, abs_tol=1e-12)
            for actual, expected in zip(item.features, features, strict=True)):
        raise ValueError('TRAINING_FEATURES_MISMATCH')
