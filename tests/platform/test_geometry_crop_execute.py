from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.composition import CropParams, ImageStructure
from photo_worker.operations.composition.plan import execute as plan_execute
from photo_worker.operations.geometry.crop import execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext, OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_composition_crop import Artifacts, image, ref
from test_asset_adapters import context_for


class CropArtifacts:
    def __init__(self, root, alpha):
        self.image = image(17, 13)
        self.rgb = np.random.default_rng(91).random((13, 17, 3), dtype=np.float32)
        self.paths = {}; self.staged = {}; self.documents = {}; self.opens = 0
        for role, value in [('rgb', self.rgb), *([('alpha', np.arange(221, dtype=np.uint16).reshape(13, 17)*200)] if alpha else [])]:
            path = root / (role + '.npy'); np.save(path, value)
            pin = ref(); self.paths[pin.artifact_id] = path
            if role == 'rgb': self.image.pixels = pin
            else: self.image.alpha = pin
        source = Artifacts(self.image, ImageStructure(image=ref()))
        plan_execute(context_for(source.structure_ref, 'composition.plan'), source)
        self.plan = source.staged[0][1]
        self.image_ref, self.plan_ref = source.image_ref, ref(self.plan)
        self.value = CropParams(image=self.image_ref, plan=self.plan_ref)
        self.inputs = [self.image_ref, self.plan_ref, self.image.pixels, *([self.image.alpha] if alpha else [])]
    def params(self, model): return self.value
    def contract(self, pin, model): return self.image if pin == self.image_ref else self.plan
    @contextmanager
    def input(self, pin, *, suffix):
        self.opens += 1
        yield SimpleNamespace(path=self.paths[pin.artifact_id])
    def stage(self, path, reservation, role, media_type):
        reservation.validate(); self.staged[role] = np.load(path).copy()
        return ref()
    def stage_contract(self, document, role):
        self.documents[role] = document
        return ref(document)


def make_context(inputs, plan=None):
    now = datetime.now(timezone.utc)
    return ExecutionContext(Lease(SimpleNamespace(inputs=inputs), uuid4(), 1,
        now+timedelta(minutes=1), now+timedelta(minutes=1),
        {'backend':'cpu', 'resource_estimate':{'ram_bytes':3<<30, 'scratch_bytes':1000000, 'output_bytes':3<<20, 'vram_bytes':0, 'pixel_count':plan.output_width*plan.output_height if plan else 1}}, 'e'*64))


@pytest.mark.parametrize('alpha', [False, True])
def test_native_crop_operation_publishes_geometry_and_alpha(tmp_path, alpha):
    a = CropArtifacts(tmp_path, alpha); ctx = make_context(a.inputs, a.plan)
    scratch_root = tmp_path/'scratch'; scratch_root.mkdir()
    with ScratchSpace(scratch_root, uuid4(), uuid4(), 1, 1000000, min_free_bytes=0) as scratch:
        a.scratch = scratch; result = execute(ctx, a)
    output, report = a.documents['working_image'], a.documents['crop_report']
    assert output.canvas.width == a.plan.output_width
    assert output.canvas.height == a.plan.output_height
    assert output.transform_chain.steps[-1].from_canvas == a.image.canvas
    assert report.alpha_preserved is alpha
    assert (output.alpha is not None) is alpha
    assert ('working_alpha' in a.staged) is alpha
    assert result.review_required == (a.plan.disposition == 'review')
    assert len(result.outputs) == (4 if alpha else 3)
    assert result.bytes_written > a.staged['working_rgb'].nbytes


def test_crop_operation_rejects_undeclared_alpha_before_pixel_io(tmp_path):
    a = CropArtifacts(tmp_path, True)
    with pytest.raises(OperationFailure) as failure:
        execute(make_context([pin for pin in a.inputs if pin != a.image.alpha]), a)
    assert failure.value.error.code == 'INPUT_INVALID'
    assert a.opens == 0 and not a.staged


def test_crop_peak_accounts_full_source_output_and_alpha(tmp_path):
    from photo_worker.operations.geometry.prepare import crop_provider_peak
    a = CropArtifacts(tmp_path, True)
    peak = crop_provider_peak(a.image_ref, a.image, a.plan_ref, a.plan)
    source = a.image.canvas.width*a.image.canvas.height
    output = a.plan.output_width*a.plan.output_height
    assert peak['ram_bytes'] >= 72*source+120*output
    assert peak['scratch_bytes'] >= 14*output+65536
    assert peak['vram_bytes'] == peak['output_bytes'] == 0
    assert a.opens == 0
    a.image.alpha = None
    opaque = crop_provider_peak(a.image_ref, a.image, a.plan_ref, a.plan)
    assert peak['ram_bytes']-opaque['ram_bytes'] == 24*source+40*output
    assert peak['scratch_bytes']-opaque['scratch_bytes'] == 2*output


@pytest.mark.parametrize('field,code', [('ram_bytes','RAM_ADMISSION_EXCEEDED'),
    ('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
    ('output_bytes','OUTPUT_ADMISSION_EXCEEDED'),('pixel_count','CONFIG_INVALID')])
def test_crop_rejects_underbudget_before_pixel_io(tmp_path, field, code):
    a = CropArtifacts(tmp_path, True); ctx = make_context(a.inputs, a.plan)
    ctx.lease.execution_profile['resource_estimate'][field] = 1
    with pytest.raises(OperationFailure) as failure:
        execute(ctx, a)
    # The profile's historical minimum RAM check also rejects malformed RAM.
    assert failure.value.error.code == ('CONFIG_INVALID' if field == 'ram_bytes' else code)
    assert a.opens == 0 and not a.staged
