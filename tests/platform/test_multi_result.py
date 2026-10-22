from uuid import uuid4
from pathlib import Path
import pytest
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.multi_result import multi_image_report,multi_clipping_fraction,stage_multi_result,multi_required_inputs


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def test_multi_report_retains_source_order_and_motion_evidence():
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='b'*64) for _ in range(2)]
    registrations=[ref(),ref()]
    report=multi_image_report('panorama',bindings,registrations,motion_fraction=.125,registration_inconsistent=True)
    assert report.sources==bindings and report.registrations==registrations
    assert report.status=='review' and report.skipped_count==0
    assert 'motion_fraction=0.125' in report.reason and 'REGISTRATION_INCONSISTENT' in report.reason


@pytest.mark.parametrize('fraction',[float('nan'),float('inf'),-.1,1.1,True])
def test_invalid_motion_evidence_rejected(fraction):
    with pytest.raises(ValueError,match='MULTI_REPORT_INVALID'):
        multi_image_report('panorama',[None,None],[ref(),ref()],motion_fraction=fraction,registration_inconsistent=False)


def test_multi_clipping_fraction_counts_pixels_not_channels_and_preserves_hdr():
    rgb=np.array([[[.5,.5,.5],[1.,.5,.5]], [[-.1,.5,.5],[2.,2.,2.]]],dtype=np.float32)
    assert multi_clipping_fraction(rgb,tile=1)==.75
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='b'*64) for _ in range(2)]
    report=multi_image_report('focus_stack',bindings,[ref(),ref()],motion_fraction=0.,
                              registration_inconsistent=False,clipping_fraction=.75)
    assert report.clipping_fraction==.75
    np.testing.assert_array_equal(rgb[1,1],[2.,2.,2.])


def test_no_staging_for_untyped_preparation():
    class NoWrites:
        def stage(self,*args):pytest.fail('unexpected staging')
        def stage_contract(self,*args):pytest.fail('unexpected staging')
    with pytest.raises(ValueError,match='MULTI_RESULT_INVALID'):stage_multi_result(NoWrites(),None)


def test_required_inputs_include_typed_source_children_and_reject_conflicting_sha():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    source=WorkingImage.model_validate_json(path.read_bytes())
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(source)),
                             canvas_hash=canonical_hash(source.canvas)) for _ in range(2)]
    roots=multi_required_inputs([source,source],bindings)
    assert roots[0]==bindings[0].source and bindings[1].source in roots
    assert source.pixels in roots
    if source.alpha is not None:assert source.alpha in roots
    replaced=source.model_copy(deep=True)
    replaced.pixels=ArtifactRef(artifact_id=source.pixels.artifact_id,sha256='f'*64)
    bindings[1].source=ArtifactRef(artifact_id=bindings[1].source.artifact_id,sha256=canonical_hash(replaced))
    with pytest.raises(ValueError,match='MULTI_SOURCE_MISMATCH'):
        multi_required_inputs([source,replaced],bindings)

    alias=source.model_copy(deep=True)
    alias.alpha=alias.pixels
    aliased_bindings=[VersionBinding(asset_version_id=uuid4(),
        source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(alias)),
        canvas_hash=canonical_hash(alias.canvas)) for _ in range(2)]
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        multi_required_inputs([alias,alias],aliased_bindings)

    version_id=uuid4()
    duplicate_versions=[VersionBinding(asset_version_id=version_id,
        source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(source)),
        canvas_hash=canonical_hash(source.canvas)) for _ in range(2)]
    with pytest.raises(ValueError,match='MULTI_SOURCE_MISMATCH'):
        multi_required_inputs([source,source],duplicate_versions)


def test_required_inputs_reject_cross_source_document_array_alias_even_with_matching_sha(monkeypatch):
    from photo_worker.operations.optical import multi_result as module
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    second=WorkingImage.model_validate_json(path.read_bytes())
    first=second.model_copy(deep=True)
    first.pixels=ArtifactRef(artifact_id=uuid4(),sha256=second.pixels.sha256)
    # Isolate role collision from the independent canonical source-hash check.
    monkeypatch.setattr(module,'canonical_hash',lambda _:second.pixels.sha256)
    bindings=[VersionBinding(asset_version_id=uuid4(),
        source=ArtifactRef(artifact_id=second.pixels.artifact_id if index==0 else uuid4(),
                           sha256=second.pixels.sha256),
        canvas_hash=second.pixels.sha256) for index in range(2)]
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        module.multi_required_inputs([first,second],bindings)


def test_required_inputs_reject_cross_source_pixel_alpha_role_collision():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    first=WorkingImage.model_validate_json(path.read_bytes())
    second=first.model_copy(deep=True)
    second.pixels=ref()
    second.alpha=ArtifactRef(artifact_id=first.pixels.artifact_id,sha256=first.pixels.sha256)
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(
        artifact_id=uuid4(),sha256=canonical_hash(image)),canvas_hash=canonical_hash(image.canvas))
        for image in (first,second)]
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        multi_required_inputs([first,second],bindings)


def test_required_inputs_reject_wrong_canvas_hash():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    source=WorkingImage.model_validate_json(path.read_bytes())
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(
        artifact_id=uuid4(),sha256=canonical_hash(source)),canvas_hash='f'*64) for _ in range(2)]
    with pytest.raises(ValueError,match='MULTI_SOURCE_MISMATCH'):
        multi_required_inputs([source,source],bindings)
