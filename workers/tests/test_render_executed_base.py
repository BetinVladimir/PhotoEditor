"""Seal receipt authority must originate in primary scope, never JSON alone."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.render_executed_base import qualify_scoped_executed_base
from photo_worker.v2.render_adoption_inputs import decode_executed_base_reference
from photo_worker.v2.adoption import AdoptionInvalid


def inputs(reference):
    return SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(executed_base_receipt=reference)))


def test_ordinary_base_needs_no_seal_authority_or_io():
    scoped = SimpleNamespace(executed_base_receipt=None)
    assert qualify_scoped_executed_base(None, inputs(None), scoped, lambda: None) is None


@pytest.mark.parametrize('case', ['missing_primary', 'unexpected_primary', 'wrong_hash', 'wrong_id'])
def test_receipt_cannot_be_self_authorized_before_document_io(case):
    ref = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    primary = SimpleNamespace(artifact_id=ref.artifact_id, sha256=ref.sha256)
    requested = ref
    if case == 'missing_primary': primary = None
    if case == 'unexpected_primary': requested = None
    if case == 'wrong_hash': primary.sha256 = 'b'*64
    if case == 'wrong_id': primary.artifact_id = uuid4()
    with pytest.raises(ValueError, match='AUTHORITY_REQUIRED'):
        qualify_scoped_executed_base(None, inputs(requested), SimpleNamespace(executed_base_receipt=primary), lambda: None)


@pytest.mark.parametrize('row', [(), ('a',), ('bad', 'a'*64), (str(uuid4()), 'z'*64), (str(uuid4()), 'a'*64, 'extra')])
def test_primary_receipt_row_is_strict(row):
    with pytest.raises(AdoptionInvalid): decode_executed_base_reference(row)


def test_no_primary_receipt_is_normal_for_existing_render():
    assert decode_executed_base_reference(None) is None


def sealed_fixture():
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.contracts.editor_effects import EditorExecutedBaseReceipt
    from photo_worker.operations.session.contracts import VersionBinding,CorrectionVector
    from photo_worker.operations.learning.contracts import FrozenRecipe
    from photo_worker.operations.optical.contracts import ImageResult,ImageReport
    from photo_worker.v2.render_recipe import EditingRecipeV3,FrozenRenderRecipe
    def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='b'*64)
    old=source.model_copy(update={'source':ref()});style,originref,receiptref,resultref,reportref=ref(),ref(),ref(),ref(),ref()
    zero=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    def frozen(binding,receipt=None):
        recipe=EditingRecipeV3(schema_version=3,source=binding,base_style=style,session_corrections=None,
            learning_version_id=None,overrides=[],layer_stack=None,renderer_version='v2.2',parameter_delta=zero,
            executed_base_receipt=receipt)
        return FrozenRenderRecipe(schema_version=1,recipe=recipe,recipe_sha256=canonical_hash(recipe),
            scalars=FrozenRecipe(schema_version=1,source=binding,base_style=style,effective_delta=zero,
                renderer_version='v2.2',icc_lock_sha='b'*64,model_lock_sha='c'*64),learning=None)
    origin=frozen(old);final=frozen(source,receiptref)
    report=ImageReport(schema_version=1,status='review',operation='editor.effects_seal',skipped_count=0,reason='generated',
        outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[source],registrations=[])
    result=ImageResult(schema_version=1,working_image=source.source,report=reportref,changed_mask=ref(),generated_mask=ref())
    receipt=EditorExecutedBaseReceipt(schema_version=1,build_run_id=uuid4(),original_recipe=originref,original_source=old,
        processed_source=source,executed_style=style,compilation_sha='d'*64,trace_sha='e'*64,quality=ref(),
        image_result=resultref,generative_requested=True,inherited_review=True)
    docs={receiptref.artifact_id:receipt,originref.artifact_id:origin,resultref.artifact_id:result,reportref.artifact_id:report}
    artifacts=SimpleNamespace(contract=lambda ref,model:docs[ref.artifact_id])
    scoped=SimpleNamespace(executed_base_receipt=receiptref,parent_roots=(resultref,))
    return artifacts,SimpleNamespace(recipe=final),scoped,docs,receipt,report


def test_sealed_base_accepts_live_exact_receipt_with_inherited_generated_review():
    artifacts,state,scope,docs,receipt,report=sealed_fixture()
    accepted=qualify_scoped_executed_base(artifacts,state,scope,lambda:None)
    assert accepted[0]==receipt and accepted[2]==report


@pytest.mark.parametrize('mode',['missing_parent','repeat_layer','repeat_delta','drop_review'])
def test_sealed_base_cannot_drop_provenance_or_reapply_completed_work(mode):
    artifacts,state,scope,docs,receipt,report=sealed_fixture()
    if mode=='missing_parent':scope.parent_roots=()
    elif mode=='repeat_layer':state.recipe.recipe.layer_stack=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    elif mode=='repeat_delta':state.recipe.recipe.parameter_delta.ev=.1
    else:report.status='pass'
    with pytest.raises(ValueError):qualify_scoped_executed_base(artifacts,state,scope,lambda:None)
