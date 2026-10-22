from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.depth import DepthParams
from photo_worker.operations.depth.predict import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_image_denoise import Artifacts, LOCK, Pool, context


def depth_fixture(root):
    artifacts = Artifacts(root)
    artifacts.raw = DepthParams(
        image=artifacts.image_ref,
        model_lock_id=LOCK,
    ).model_dump(mode="json")
    pool = Pool(model="depth_anything_v2_small")

    def infer(rgb):
        return np.linspace(0, 1, 9 * 11, dtype=np.float32).reshape(9, 11)

    pool.engine.infer = infer
    return artifacts, pool, context(artifacts)


def test_depth_predict_opaque_image_omits_invalid_mask(tmp_path):
    artifacts, pool, ctx = depth_fixture(tmp_path)
    root = tmp_path / "scratch"
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 1_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts, pool, "cuda")
    depth = artifacts.staged[0][1]
    assert depth.invalid_mask is None
    assert depth.float32_shape == [20, 24]
    assert len(result.outputs) == 2


def test_depth_predict_rejects_undeclared_pixels(tmp_path):
    artifacts, pool, ctx = depth_fixture(tmp_path)
    ctx.lease.command.inputs.remove(artifacts.pixel_ref)
    with pytest.raises(OperationFailure) as error:
        execute(ctx, artifacts, pool, "cuda")
    assert error.value.error.code == "INPUT_INVALID"
    assert not artifacts.uploads


def test_depth_runtime_change_during_inference_prevents_staging(tmp_path):
    artifacts,pool,ctx=depth_fixture(tmp_path)
    valid=True
    original=pool.engine.infer
    def infer(rgb):
        nonlocal valid
        result=original(rgb);valid=False;return result
    pool.engine.infer=infer
    with pytest.raises(OperationFailure) as error:
        execute(ctx,artifacts,pool,'cuda',lambda _:valid)
    assert error.value.error.code=='DEPTH_RUNTIME_UNQUALIFIED'
    assert not artifacts.uploads
    assert not artifacts.staged


def test_depth_runtime_is_rechecked_after_result_document_staging(tmp_path):
    artifacts,pool,ctx=depth_fixture(tmp_path)
    valid=True
    original=artifacts.stage_contract
    def stage_contract(*args,**kwargs):
        nonlocal valid
        result=original(*args,**kwargs);valid=False;return result
    artifacts.stage_contract=stage_contract
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        with pytest.raises(OperationFailure) as error:
            execute(ctx,artifacts,pool,'cuda',lambda _:valid)
    assert error.value.error.code=='DEPTH_RUNTIME_UNQUALIFIED'
    # Staged files are attempt-owned; no OperationResult escapes for completion.
    assert artifacts.staged


def test_depth_cancellation_after_inference_prevents_staging(tmp_path):
    artifacts,pool,ctx=depth_fixture(tmp_path)
    cancelled=False
    original=pool.engine.infer
    def infer(rgb):
        nonlocal cancelled
        result=original(rgb);cancelled=True;return result
    def active():
        if cancelled:raise OperationFailure('TASK_CANCELLED')
    pool.engine.infer=infer;ctx.check_active=active
    with pytest.raises(OperationFailure) as error:
        execute(ctx,artifacts,pool,'cuda',lambda _:True)
    assert error.value.error.code=='TASK_CANCELLED'
    assert not artifacts.uploads
    assert not artifacts.staged
