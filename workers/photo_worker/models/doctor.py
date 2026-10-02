from __future__ import annotations

import hashlib
import importlib.metadata
import platform
import time
from pathlib import Path
from typing import Any

import numpy as np
import psutil

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from .adapters import factories
from .install import ModelError
from .lock import ModelLock
from .pool import ModelPool


def _array(value: Any, model_id: str, *, allow_empty: bool = False) -> np.ndarray:
    result = np.asarray(value)
    if (result.size < 1 and not allow_empty) or result.size > 200_000_000 or not np.issubdtype(result.dtype, np.number) or not np.isfinite(result).all():
        raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
    return result


def smoke(model_id: str, model: Any) -> dict[str, Any]:
    black64 = np.zeros((64, 64, 3), np.float32)
    if model_id == "lraspp_mobilenet_v3_large":
        output = _array(model.infer(black64), model_id)
    elif model_id == "keypointrcnn_resnet50_fpn":
        values = model.infer(np.zeros((640, 640, 3), np.float32))
        if not isinstance(values, dict) or set(values) != {"boxes", "scores", "keypoints", "keypoints_scores"}:
            raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
        output = np.concatenate([_array(value, model_id, allow_empty=True).reshape(-1) for value in values.values()])
    elif model_id == "sfd":
        output = _array(model.infer(black64), model_id, allow_empty=True)
        if output.ndim != 2 or output.shape[1] != 5:
            raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
    elif model_id == "fan2d":
        output = _array(model.infer(np.zeros((256, 256, 3), np.float32)), model_id)
        if output.shape != (68, 3):
            raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
    elif model_id == "u2net":
        output = _array(model.infer(np.zeros((320, 320, 3), np.float32)), model_id)
        if output.shape != (320, 320):
            raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
    elif model_id == "mobilesam":
        values = model.infer(np.zeros((1024, 1024, 3), np.float32), np.array([[512, 512]], np.float32), np.array([1], np.float32), np.array([0, 0, 1023, 1023], np.float32))
        if not isinstance(values, tuple) or len(values) != 2:
            raise ModelError(f"{model_id}:MODEL_OUTPUT_INVALID")
        output = np.concatenate([_array(value, model_id).reshape(-1) for value in values])
    elif model_id == "big_lama":
        output = _array(model.infer(black64, np.zeros((64, 64), np.float32)), model_id)
    elif model_id == "sd15_inpaint":
        mask = np.zeros((512, 512), np.float32)
        mask[192:320, 192:320] = 1
        output = _array(model.generate(np.zeros((512, 512, 3), np.float32), mask, prompt="neutral gray square", negative_prompt="", seed=0, steps=25, guidance_scale=7.5, scheduler="ddim_v1"), model_id)
    else:
        output = _array(model.infer(black64), model_id)
    return {"shape": list(output.shape), "dtype": str(output.dtype), "minimum": float(output.min(initial=0)), "maximum": float(output.max(initial=0))}


def run(root: Path, lock_path: Path, backend: str) -> dict[str, Any]:
    if backend not in ("cpu", "mps", "cuda"):
        raise ValueError("Unsupported backend")
    root, lock_path = root.resolve(strict=True), lock_path.resolve(strict=True)
    if not root.is_dir() or not lock_path.is_file() or lock_path.stat().st_size > 8_388_608:
        raise ValueError("Invalid model doctor paths")
    lock_raw = lock_path.read_bytes()
    lock = ModelLock.model_validate_json(lock_raw)
    pool = ModelPool(root, lock, factories())
    process = psutil.Process()
    results = []
    try:
        for entry in sorted(lock.models, key=lambda value: value.id):
            state = entry.backends[backend]
            if state.status == "unsupported":
                results.append({"model_id": entry.id, "status": "unsupported", "reason": state.reason,
                                "lock_backend_status": state.status, "lock_report_sha256": state.report_sha256})
                continue
            started, rss = time.monotonic(), process.memory_info().rss
            try:
                with pool.acquire(entry.id, backend, allow_unqualified=True) as model:
                    evidence = smoke(entry.id, model)
                results.append({"model_id": entry.id, "status": "passed", "lock_backend_status": state.status,
                                "lock_report_sha256": state.report_sha256,
                                "elapsed_seconds": time.monotonic() - started,
                                "rss_delta_bytes": max(0, process.memory_info().rss - rss), "output": evidence})
            except Exception as exc:
                results.append({"model_id": entry.id, "status": "failed", "lock_backend_status": state.status,
                                "lock_report_sha256": state.report_sha256,
                                "elapsed_seconds": time.monotonic() - started, "error_type": type(exc).__name__})
    finally:
        pool.close()
    report = {"schema_version": 1, "lock_id": lock.lock_id, "lock_file_sha256": hashlib.sha256(lock_raw).hexdigest(),
              "backend": backend, "platform": platform.platform(), "python": platform.python_version(),
              "torch": _version("torch"), "torchvision": _version("torchvision"), "models": results}
    report["report_sha256"] = canonical_hash(report)
    return report


def _version(name: str) -> str | None:
    try:
        return importlib.metadata.version(name)
    except importlib.metadata.PackageNotFoundError:
        return None


def write_report(path: Path, report: dict[str, Any]) -> None:
    target = path.absolute()
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(target.name + ".tmp")
    temporary.write_bytes(canonical_bytes(report) + b"\n")
    temporary.replace(target)
