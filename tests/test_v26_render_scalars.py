from contextlib import contextmanager
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_scalars import verify_render_scalar_evidence,load_render_session_delta
from test_v26_frozen_render_recipe import fixture

class Inputs:
    def __init__(self,bodies=None):self.bodies=bodies or {};self.reads=0
    @contextmanager
    def stream(self,ref):
        self.reads+=1
        with BytesIO(self.bodies[str(ref.artifact_id)]) as stream:yield stream


def test_authored_delta_is_reconciled_without_extra_reads():
    frozen=decode_frozen_render_recipe(canonical_bytes(fixture()));inputs=Inputs()
    assert verify_render_scalar_evidence(inputs,frozen,()).ev==.1 and inputs.reads==0
    frozen.scalars.effective_delta.ev=.2
    with pytest.raises(ValueError,match='EFFECTIVE'):verify_render_scalar_evidence(inputs,frozen,())


@pytest.mark.parametrize('invalid',['locked','ordinal','source'])
def test_session_plan_rejects_locked_nonzero_incomplete_order_and_wrong_source(invalid):
    value=fixture();binding=value['recipe']['source']
    delta=dict(value['recipe']['parameter_delta']);delta['ev']=0
    item=dict(binding=binding,ordinal=0,delta=delta,disposition='apply',reason=None)
    if invalid=='locked':item['disposition']='manual_locked';item['delta']['ev']=.1
    elif invalid=='ordinal':item['ordinal']=1
    else:item['binding']=dict(binding,asset_version_id=str(uuid4()))
    plan=dict(schema_version=1,snapshot_digest='a'*64,items=[item]);ref=dict(artifact_id=str(uuid4()),sha256=canonical_hash(plan))
    value['recipe']['session_corrections']=ref;value['recipe_sha256']=canonical_hash(value['recipe'])
    with pytest.raises(ValueError):load_render_session_delta(Inputs({ref['artifact_id']:canonical_bytes(plan)}),decode_frozen_render_recipe(canonical_bytes(value)))


def test_session_delta_is_added_before_authored_delta_and_matches_frozen_result():
    value=fixture();delta=dict(value['recipe']['parameter_delta']);delta['ev']=.2
    plan=dict(schema_version=1,snapshot_digest='a'*64,items=[dict(binding=value['recipe']['source'],ordinal=0,delta=delta,disposition='apply',reason=None)])
    ref=dict(artifact_id=str(uuid4()),sha256=canonical_hash(plan));value['recipe']['session_corrections']=ref
    value['recipe_sha256']=canonical_hash(value['recipe']);value['scalars']['effective_delta']=dict(value['scalars']['effective_delta'],ev=.2+.1)
    assert verify_render_scalar_evidence(Inputs({ref['artifact_id']:canonical_bytes(plan)}),decode_frozen_render_recipe(canonical_bytes(value)),()).ev==.2+.1


def test_override_evidence_requires_exact_order_and_final_bounds():
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.operations.session.contracts import CorrectionVector
    from photo_worker.v2.render_scalars import ResolvedRenderOverride
    value=fixture();refs=[dict(artifact_id=str(uuid4()),sha256='a'*64) for _ in range(2)]
    value['recipe']['overrides']=refs;value['recipe_sha256']=canonical_hash(value['recipe'])
    delta=dict(value['recipe']['parameter_delta'],ev=0)
    overrides=tuple(ResolvedRenderOverride(ArtifactRef.model_validate_json(canonical_bytes(ref)),CorrectionVector.model_validate_json(canonical_bytes(delta))) for ref in refs)
    frozen=decode_frozen_render_recipe(canonical_bytes(value));inputs=Inputs()
    assert verify_render_scalar_evidence(inputs,frozen,overrides).ev==.1
    with pytest.raises(ValueError,match='OVERRIDE'):verify_render_scalar_evidence(inputs,frozen,tuple(reversed(overrides)))
    assert inputs.reads==0
    overrides[0].delta.ev=1
    with pytest.raises(ValueError):verify_render_scalar_evidence(inputs,frozen,overrides)
