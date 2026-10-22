"""A revoked lease cannot reach gateway/scratch through artifact-only adapters.

These are authored invocation scenarios, not numerical/physical qualification.
The real TaskArtifacts.create boundary remains in the path.
"""
import importlib
from types import SimpleNamespace
import pytest
from photo_worker.runtime.executor import LeaseLost
from photo_worker.runtime import artifacts


FACTORIES = (
    ('archive.build', 'archive_operations'),
    ('background.blur', 'blur_operations'),
    ('background.compose', 'background_operations'),
    ('collection.cull', 'cull_operations'),
    ('collection.features', 'feature_operations'),
    ('collection.group', 'group_operations'),
    ('color.stats', 'stats_operations'),
    ('composition.plan', 'composition_operations'),
    ('composition.structure', 'structure_operations'),
    ('conservative', 'conservative_operations'),
    ('defects.propose', 'defect_propose_operations'),
    ('defects.repair', 'defect_repair_operations'),
    ('derivative.resize', 'resize_operations'),
    ('derivative.sharpen', 'sharpen_operations'),
    ('derivative.watermark', 'watermark_operations'),
    ('mask.face_skin', 'face_skin_operations'),
    ('geometry.body', 'body_operations'),
    ('geometry.crop', 'crop_operations'),
    ('image.color', 'image_color_operations'),
    ('image.local', 'image_local_operations'),
    ('learning.fit', 'learning_fit_operations'),
    ('mask.rasterize', 'mask_operations'),
    ('mask.transform', 'mask_transform_operations'),
    ('pair.fit.beauty', 'beauty_fit_operations'),
    ('pair.fit.body', 'body_fit_operations'),
    ('pair.fit.color', 'color_fit_operations'),
    ('pair.fit.defects', 'defect_fit_operations'),
    ('pair.register', 'pair_operations'),
    ('portrait.beauty', 'beauty_operations'),
    ('portrait.detail', 'detail_operations'),
    ('quality.check', 'quality_operations'),
    ('report.contact_sheet', 'contact_sheet_operations'),
    ('sensor.dust', 'sensor_operations'),
    ('sensor.repair', 'dust_repair_operations'),
    ('series.balance', 'balance_operations'),
    ('series.measure', 'measure_operations'),
    ('series.pack', 'pack_operations'),
    ('session.adapter', 'session_plan_operations'),
    ('style.aggregate', 'aggregate_operations'),
    ('style.import_style', 'import_operations'),
    ('style.package', 'package_operations'),
    ('style.resolve', 'resolve_operations'),
    ('style.validate', 'validate_operations'),
)


@pytest.mark.parametrize('module_name,factory_name', FACTORIES)
def test_lost_lease_cannot_open_any_adapter_artifact_io(monkeypatch, module_name, factory_name):
    factory = getattr(importlib.import_module('photo_worker.operations.' + module_name), factory_name)
    # No config attributes are needed until the actual active-lease check passes.
    operations = factory(SimpleNamespace())
    calls = []
    def forbidden(*args, **kwargs):
        calls.append(True)
        raise AssertionError('Lost lease reached artifact credentials/gateway/scratch')
    monkeypatch.setattr(artifacts, 'TaskCredentials', forbidden)
    monkeypatch.setattr(artifacts, 'Gateway', forbidden)
    monkeypatch.setattr(artifacts, 'ScratchSpace', forbidden)
    class Lost:
        # Qualified profile lets balance reach its common artifact boundary;
        # this scenario is about cancellation, not a profile mismatch shortcut.
        lease = SimpleNamespace(execution_profile={'backend': 'cpu'})
        def check_active(self):
            raise LeaseLost()
    assert operations
    for operation in operations:
        with pytest.raises(LeaseLost):
            operation.execute(Lost())
    assert calls == []
