from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas, MaskRef, TransformChain
from photo_worker.contracts.override import OverrideMaskBinding, OverrideV1


def mask(selector="manual", protected=False):
    canvas = Canvas(width=32, height=24, orientation=1, space_id="source", source_sha256="a" * 64)
    return MaskRef(
        artifact=ArtifactRef(artifact_id=uuid4(), sha256="b" * 64),
        canvas=canvas,
        transform_chain=TransformChain.create([]),
        selector=selector,
        confidence=1,
        model_lock_id=None,
        protected=protected,
    )


def test_override_mask_binding_requires_matching_role_and_confirmation():
    binding = OverrideMaskBinding(
        binding_id="inpaint-1", purpose="inpaint_target", mask=mask(), confirmed=True, strength=.8
    )
    value = OverrideV1(
        schema_version=1,
        input_sha256="c" * 64,
        modules={"strength": .7},
        mask_refs=[binding],
        revision=1,
    )
    assert value.mask_refs[0].purpose == "inpaint_target"
    with pytest.raises(ValueError):
        OverrideMaskBinding(binding_id="unsafe", purpose="inpaint_target", mask=mask(), confirmed=False)
    with pytest.raises(ValueError):
        OverrideMaskBinding(binding_id="wrong", purpose="portrait_teeth", mask=mask("person"))
    with pytest.raises(ValueError):
        OverrideMaskBinding(binding_id="guard", purpose="protected", mask=mask(), confirmed=True)


def test_override_mask_binding_ids_are_unique():
    binding = OverrideMaskBinding(binding_id="same", purpose="portrait_shine", mask=mask("skin"))
    with pytest.raises(ValueError):
        OverrideV1(
            schema_version=1,
            input_sha256="c" * 64,
            modules={"strength": .7},
            mask_refs=[binding, binding],
            revision=1,
        )
