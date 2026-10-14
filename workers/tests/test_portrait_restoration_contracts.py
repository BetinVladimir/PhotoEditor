"""V28 intents reject mode confusion and aliased physical evidence."""
from uuid import uuid4

import pytest
from pydantic import ValidationError

from photo_worker.contracts.portrait_restoration import ImageReflectionParams, PortraitGlareParams


def ref():
    return dict(artifact_id=uuid4(), sha256='a'*64)


def binding():
    return dict(asset_version_id=uuid4(), source=ref(), canvas_hash='b'*64)


def glare(mode='donor'):
    return dict(schema_version=1, base=binding(), strength=.5,
        donor=binding() if mode == 'donor' else None,
        glasses_mask=ref(), protected=ref(), mode=mode, confirmation=ref(),
        model_lock_sha='c'*64, calibration=ref(), source_faces=ref(),
        donor_faces=ref() if mode == 'donor' else None)


def test_glare_requires_matching_mode_and_distinct_donor():
    assert PortraitGlareParams.model_validate(glare()).mode == 'donor'
    assert PortraitGlareParams.model_validate(glare('generative')).donor is None
    mixed = glare()
    mixed['donor'] = None
    with pytest.raises(ValidationError):
        PortraitGlareParams.model_validate(mixed)
    aliased = glare()
    aliased['donor']['asset_version_id'] = aliased['base']['asset_version_id']
    with pytest.raises(ValidationError):
        PortraitGlareParams.model_validate(aliased)
    reused = glare()
    reused['protected'] = reused['glasses_mask']
    with pytest.raises(ValidationError):
        PortraitGlareParams.model_validate(reused)


def test_reflection_requires_distinct_nonzero_evidence():
    value = dict(schema_version=1, base=binding(), strength=.5,
                 mask=ref(), protected=ref(), model_lock_sha='c'*64, calibration=ref())
    assert ImageReflectionParams.model_validate(value).strength == .5
    value['protected'] = value['mask']
    with pytest.raises(ValidationError):
        ImageReflectionParams.model_validate(value)
