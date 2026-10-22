"""Authored policy cases: base Style identity never substitutes exact Params."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.region.geometry_policy import validate_region_geometry_policy


def style(upscale='2x'):
    return SimpleNamespace(geometry=None,extensions=SimpleNamespace(composition=None,
        restoration=SimpleNamespace(upscale=upscale)))


def stage(scale=2,lock='a'*64):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    params=dict(image=ref.model_dump(mode='json'),scale=scale,model_id='realesrgan_x'+str(scale),model_lock_id=lock)
    return (SimpleNamespace(operation='image.upscale'),params,None,None)


def test_upscale_requires_exact_scale_and_installed_model_lock():
    result=validate_region_geometry_policy(style(),[stage()],model_lock_sha='a'*64)
    assert len(result[2])==1
    with pytest.raises(ValueError,match='REGION_UPSCALE_POLICY_MISMATCH'):
        validate_region_geometry_policy(style(),[stage(scale=4)],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='REGION_UPSCALE_POLICY_MISMATCH'):
        validate_region_geometry_policy(style(),[stage(lock='c'*64)],model_lock_sha='a'*64)


def test_enabled_module_cannot_be_missing_or_duplicated_in_geometry_trace():
    with pytest.raises(ValueError,match='REGION_GEOMETRY_STAGE_COVERAGE_MISMATCH'):
        validate_region_geometry_policy(style(),[],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='REGION_GEOMETRY_STAGE_COVERAGE_MISMATCH'):
        validate_region_geometry_policy(style(),[stage(),stage()],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='REGION_UPSCALE_POLICY_MISMATCH'):
        validate_region_geometry_policy(style('off'),[stage()],model_lock_sha='a'*64)


def test_generative_outpaint_requires_its_independent_decision_policy(tmp_path):
    from test_region_outpaint import policy_fixture,LOCK
    effective,entry=policy_fixture(tmp_path)
    missing=(*entry[:4],{})
    with pytest.raises(ValueError,match='REGION_OUTPAINT_POLICY_MISMATCH'):
        validate_region_geometry_policy(effective,[missing],model_lock_sha=LOCK)


def test_explicit_disabled_geometry_and_composition_do_not_require_stages():
    effective=style('off');effective.geometry=SimpleNamespace(enabled=False)
    effective.extensions.composition=SimpleNamespace(enabled=False)
    assert validate_region_geometry_policy(effective,[],model_lock_sha='a'*64)==((),(),())
