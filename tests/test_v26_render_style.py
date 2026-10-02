from contextlib import contextmanager
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash, parse_json
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_scalars import ResolvedRenderOverride
from photo_worker.v2.render_style import prepare_render_style
from test_v26_frozen_render_recipe import fixture


class Artifacts:
    def __init__(self, bodies):
        self.bodies = bodies
        self.reads = []

    @contextmanager
    def stream(self, ref):
        self.reads.append(ref.artifact_id)
        with BytesIO(self.bodies[ref.artifact_id]) as stream:
            yield stream


def setup(example=False):
    style = parse_json((Path(__file__).parents[1] / 'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    if not example:
        style['quality']['status'] = 'needs_review'
    patch = {'schema_version': 1, 'input_sha256': 'a' * 64,
             'modules': {'strength': .4}, 'revision': 1,
             'accepted_candidate_ids': ['preserved']}
    refs = [ArtifactRef.model_validate_json(canonical_bytes({'artifact_id': str(uuid4()), 'sha256': canonical_hash(document)}))
            for document in (style, patch)]
    value = fixture()
    value['recipe']['base_style'] = refs[0].model_dump(mode='json')
    value['scalars']['base_style'] = value['recipe']['base_style']
    value['recipe']['overrides'] = [refs[1].model_dump(mode='json')]
    value['recipe_sha256'] = canonical_hash(value['recipe'])
    frozen = decode_frozen_render_recipe(canonical_bytes(value))
    inputs = SimpleNamespace(recipe=frozen, source=SimpleNamespace(canvas=SimpleNamespace(source_sha256='a' * 64), transform_chain=SimpleNamespace(hash='b' * 64)))
    artifacts = Artifacts(dict(zip((ref.artifact_id for ref in refs), (canonical_bytes(style), canonical_bytes(patch)))))
    zero = CorrectionVector.model_validate_json(canonical_bytes(dict(ev=0, log_r=0, log_g=0, log_b=0, saturation_delta=0, retouch_delta=0)))
    return artifacts, inputs, (ResolvedRenderOverride(refs[1], zero),)


def test_style_preparation_preserves_modules_decisions_and_reconciles_scalar_once():
    artifacts, inputs, scalars = setup()
    result = prepare_render_style(artifacts, inputs, scalars, max_override_document_bytes=4096)
    assert result.base.strength == 1 and result.merged.strength == .4
    assert result.overrides[0].accepted_candidate_ids == ['preserved']
    assert result.effective_delta.ev == .1 and len(artifacts.reads) == 2
    assert result.effective.global_.gains_rgb == [2**.1]*3
    assert result.merged.global_.gains_rgb == [1,1,1]
    result.effective_delta.ev = .2
    assert inputs.recipe.scalars.effective_delta.ev == .1


def test_example_style_rejected_before_override_loading():
    artifacts, inputs, scalars = setup(example=True)
    with pytest.raises(ValueError, match='Example profile'):
        prepare_render_style(artifacts, inputs, scalars, max_override_document_bytes=4096)
    assert len(artifacts.reads) == 1


def test_scalar_substitution_rejected_after_complete_module_validation():
    artifacts, inputs, scalars = setup()
    scalars[0].delta.ev = .2
    with pytest.raises(ValueError, match='EFFECTIVE_DELTA_CHANGED'):
        prepare_render_style(artifacts, inputs, scalars, max_override_document_bytes=4096)


def test_actual_pinned_style_bytes_required():
    artifacts, inputs, scalars = setup()
    key = inputs.recipe.recipe.base_style.artifact_id
    artifacts.bodies[key] = artifacts.bodies[key].replace(b'needs_review', b'example')
    with pytest.raises(ValueError):
        prepare_render_style(artifacts, inputs, scalars, max_override_document_bytes=4096)
    assert len(artifacts.reads) == 1
