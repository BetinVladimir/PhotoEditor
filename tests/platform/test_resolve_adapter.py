import numpy as np
import pytest
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.contracts.images import Stats20
from photo_worker.contracts.style import Adaptation,Prototype,Disabled,LUT,ModelReference
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.operations.style.resolve import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_color_fit_adapter import Artifacts
from test_style_resolve import profile
from test_asset_adapters import context_for


def setup(root,adaptive=False,ood=False):
    artifacts=Artifacts(root,values=(.25,.25));style=profile()
    if adaptive:
        lut=LUT(kind='absolute',size=33,encoding='constant',values=[.2,.3,.4])
        adaptation=Adaptation(feature_version='stats20_v1',mean=[0.]*20,scale=[.05]*20,k=1,temperature=1,ood_threshold=1.,
            prototypes=[Prototype(id='one',features=[0.]*20,absolute_lut=lut,local=Disabled())],coverage_bins=[not ood]*512)
        style=style.model_copy(update={'mode':'adaptive','adaptation':adaptation,'models':[ModelReference(id='lraspp_mobilenet_v3_large',weight_id='fixture')]})
        # Exercise the real portable schema, not an unchecked model_copy.
        style=type(style).model_validate_json(canonical_bytes(style))
    style_ref=artifacts.add(style)
    stats=Stats20(source_image=artifacts.before,feature_version='stats20_v1',values=[0.]*20,coverage_bins=[True]*512,mask_confidence=1.,warnings=[])
    stats_ref=artifacts.add(stats)
    artifacts.raw=dict(style=style_ref.model_dump(mode='json'),image=artifacts.before.model_dump(mode='json'),stats=stats_ref.model_dump(mode='json'))
    artifacts.style=lambda ref:artifacts.contracts[ref.artifact_id]
    def stage(path,reservation,role,media_type):
        reservation.validate_file(path)
        assert role=='absolute_lut' and media_type=='application/octet-stream'
        grid=np.load(path,allow_pickle=False)
        assert grid.shape==(33,33,33,3) and grid.dtype==np.float32
        assert np.allclose(grid[12,14,21],[.2,.3,.4])
        artifacts.grid=grid
        import hashlib
        return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    artifacts.stage=stage
    ctx=context_for(artifacts.before,'style.resolve')
    ctx.lease.execution_profile['resource_estimate']={'ram_bytes':3*1024**3}
    return artifacts,ctx


@pytest.mark.parametrize('adaptive',[False,True])
def test_resolver_publishes_fixed_or_binary_lut(tmp_path,adaptive):
    artifacts,ctx=setup(tmp_path,adaptive);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;result=execute(ctx,artifacts)
        assert list(scratch.directory.iterdir())==[]
    role,manifest=artifacts.staged[0]
    assert role=='resolved_style' and manifest.mode==('adaptive' if adaptive else 'fixed')
    assert len(result.outputs)==(2 if adaptive else 1)
    if adaptive:assert manifest.absolute_lut.artifact==result.outputs[0] and manifest.neighbors[0].weight==1.
    assert ctx.snapshot_progress()==dict(done=4,total=4)
    ResolvedStyle.model_validate_json(canonical_bytes(manifest))
    import json
    changed=json.loads(canonical_bytes(manifest));changed['strength']=.123
    with pytest.raises(ValueError,match='hash mismatch'):ResolvedStyle.model_validate_json(json.dumps(changed))


@pytest.mark.parametrize('group',[None,'one'])
def test_strict_ood_skip_and_explicit_group_preserved(tmp_path,group):
    artifacts,ctx=setup(tmp_path,True,True);artifacts.raw.update(strict_ood=True,group_prototype_id=group)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;result=execute(ctx,artifacts)
    manifest=artifacts.staged[0][1]
    assert manifest.mode==('group_fixed' if group else 'skip') and result.review_required
    assert manifest.reasons==['ood_color_coverage'] and manifest.uncovered_fraction==1.
    if not group:assert manifest.fixed is None and manifest.absolute_lut is None and not manifest.local.enabled


def test_stats_source_mismatch_never_downloads_pixels(tmp_path):
    artifacts,ctx=setup(tmp_path,True)
    stats_id=next(key for key,value in artifacts.contracts.items() if isinstance(value,Stats20))
    artifacts.contracts[stats_id].source_image=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and artifacts.opens==0 and not artifacts.staged


def test_job_and_per_image_overrides_are_applied_in_precedence_order(tmp_path):
    artifacts,ctx=setup(tmp_path)
    artifacts.raw.update(
        job_override={"schema_version":1,"modules":{"strength":.2}},
        override={"schema_version":1,"input_sha256":artifacts.before.sha256,
                  "modules":{"strength":.8},"revision":1},
    )
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;result=execute(ctx,artifacts)
    manifest=artifacts.staged[0][1]
    assert result.review_required is False
    assert manifest.strength==.8
    assert manifest.params.job_override.modules.strength==.2
    assert manifest.params.override.modules.strength==.8


def test_per_image_override_is_bound_to_exact_working_image(tmp_path):
    artifacts,ctx=setup(tmp_path)
    artifacts.raw.update(override={"schema_version":1,"input_sha256":"f"*64,
                                   "modules":{"strength":.8},"revision":1})
    with pytest.raises(OperationFailure) as error:
        execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID'
    assert not artifacts.staged


def test_identity_recipe_resolves_without_a_style_artifact(tmp_path):
    artifacts,ctx=setup(tmp_path)
    artifacts.raw.pop('style')
    artifacts.raw.update(identity=True, stats=None,
        override={"schema_version":1,"input_sha256":artifacts.before.sha256,
                  "modules":{"strength":.6},"revision":1})
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;result=execute(ctx,artifacts)
    manifest=artifacts.staged[0][1]
    assert manifest.mode=='fixed' and manifest.strength==.6
    assert manifest.fixed.residual_lut.values==[0.,0.,0.]
    assert manifest.modules.models==[] and not result.review_required
