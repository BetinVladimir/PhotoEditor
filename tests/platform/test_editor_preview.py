import hashlib
import json
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
from PIL import Image

from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.editor.preview import execute, preview_operations
from photo_worker.runtime.artifacts import ArtifactConfig
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_style_resolve import profile


class Artifacts:
    def __init__(self, root, registry):
        pixels = np.full((12, 20, 3), .25, np.float32)
        path = root / "pixels.npy"
        np.save(path, pixels)
        self.pixel_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        work = registry.resolve("work_rec2020_gamma24_v1")
        self.image = WorkingImage(pixels=self.pixel_ref,
            canvas=Canvas(width=20, height=12, orientation=1, space_id="preview-test", source_sha256="c" * 64),
            color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256=work.sha256, encoding="gamma24"),
            dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
        self.image_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.style_value = profile()
        self.style_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(self.style_value)).hexdigest())
        self.paths = {self.pixel_ref.artifact_id: path}
        self.staged = []
        self.raw = {"image": self.image_ref.model_dump(mode="json"), "style": self.style_ref.model_dump(mode="json"),
                    "modules": {"strength": 0.0}, "scale": "proxy", "roi": [2, 2, 10, 8]}

    def params(self, model):
        return model.model_validate_json(json.dumps(self.raw))

    def contract(self, ref, model):
        assert ref == self.image_ref
        return self.image

    def style(self, ref):
        assert ref == self.style_ref
        return self.style_value

    @contextmanager
    def input(self, ref, *, suffix):
        yield SimpleNamespace(path=self.paths[ref.artifact_id])

    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path)
        with Image.open(path) as image:
            assert image.size == (10, 8) and image.info.get("icc_profile")
            self.preview_mean = np.asarray(image).mean()
        return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())

    def stage_contract(self, value, role):
        self.staged.append((role, value))
        return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def test_editor_preview_executes_revision_recipe_and_color_manages_output(tmp_path):
    root = Path(__file__).resolve().parents[2]
    registry = ICCRegistry(root / "resources/icc")
    artifacts = Artifacts(tmp_path, registry)
    ctx = context_for(artifacts.image_ref, "editor.preview")
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 1 << 30}
    scratch_root = tmp_path / "scratch"; scratch_root.mkdir()
    with ScratchSpace(scratch_root, uuid4(), uuid4(), 1, 4 << 20, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts, registry)
    assert len(result.outputs) == 2
    assert artifacts.staged[0][0] == "derivative_manifest"
    assert artifacts.staged[0][1].variant == "preview1600"
    assert 40 < artifacts.preview_mean < 100


def test_editor_preview_registers_only_with_icc_runtime(tmp_path):
    config = ArtifactConfig(tmp_path, "https://gateway.invalid", None, 1, 1, 1)
    assert preview_operations(config, None) == []
    root = Path(__file__).resolve().parents[2]
    assert [(item.name, item.resource_class) for item in preview_operations(config, root / "resources/icc")] == [("editor.preview", "media")]
