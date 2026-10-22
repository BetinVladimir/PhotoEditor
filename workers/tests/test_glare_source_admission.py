"""Authored source-only adapter scenarios; qualification is not simulated here."""
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.operations.portrait import glare_source_admission as module
from photo_worker.runtime.glare_config import glare_donor_installation_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_source_adapter_reuses_bounded_existing_helper(monkeypatch):
    rgb = np.zeros((8, 8, 3), dtype=np.float32)
    mask = np.zeros((8, 8), dtype=np.float32)
    mask[2, 3] = .5
    engine = object()
    checks, calls = [], []

    def infer(actual_engine, actual_rgb, actual_mask, limit):
        calls.append((actual_engine, actual_rgb, actual_mask, limit))
        return actual_rgb.copy()

    monkeypatch.setattr(module, 'infer_inpaint_candidate', infer)
    result = module._GlareInpaint(engine).infer(rgb, mask,
        check_active=lambda: checks.append('active'))
    assert len(checks) == 2
    assert len(calls) == 1
    assert calls[0][0] is engine and calls[0][1] is rgb
    assert calls[0][2].dtype == np.bool_ and calls[0][2][2, 3]
    assert calls[0][3] == 1_081_600
    assert np.array_equal(result, rgb)


def test_source_adapter_does_not_infer_after_cancellation(monkeypatch):
    def cancelled():
        raise RuntimeError('cancelled')

    monkeypatch.setattr(module, 'infer_inpaint_candidate',
        lambda *_: pytest.fail('inference after cancellation'))
    with pytest.raises(RuntimeError, match='cancelled'):
        module._GlareInpaint(object()).infer(None, None,
            check_active=cancelled)


def test_mps_does_not_advertise_uninstalled_source_glare():
    config = SimpleNamespace(resource_class='gpu', backend='mps')
    values = {'PHOTO_GLARE_DONOR_ENABLED': 'true',
              'PHOTO_GLARE_GENERATIVE_ENABLED': 'true'}
    assert glare_donor_installation_from_environment(config,
        lambda key, default=None: values.get(key, default)) is None


def test_source_enablement_rejects_nonboolean_before_loading_models():
    config = SimpleNamespace(resource_class='gpu', backend='cpu',
        model_lock=object(), model_root=object())
    values = {'PHOTO_GLARE_DONOR_ENABLED': 'true',
              'PHOTO_GLARE_GENERATIVE_ENABLED': 'yes'}
    with pytest.raises(WorkerConfigurationError,
                       match='Invalid generative glare enablement'):
        glare_donor_installation_from_environment(config,
            lambda key, default=None: values.get(key, default))
