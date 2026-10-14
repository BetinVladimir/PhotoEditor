"""Donor glare requires current SQL consent and versioned input authority."""

from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.operations.portrait import glare_donor_admission as module
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.glare_donor_object_reader import GlareWorkerObjectReader


def test_glare_donor_admission_rechecks_current_attempt_and_reader(monkeypatch, tmp_path):
    monkeypatch.setattr(module, 'canonical_bytes', lambda _: b'{}')
    monkeypatch.setattr(module, 'canonical_hash', lambda _: 'b' * 64)
    gate = Mock(return_value=True)
    db = SimpleNamespace(glare_donor_source_ready=gate,
                         glare_donor_input_evidence=Mock())
    objects = SimpleNamespace(get_object=Mock())
    admission = module.GlareDonorAdmission(db, objects, 'photos',
        runtime_sha='c' * 64, model_lock_sha='a' * 64,
        material_scratch_root=tmp_path, material_max_bytes=1024,
        alpha_mode='require_float32')
    params = SimpleNamespace(mode='donor', model_lock_sha='a' * 64)
    command = SimpleNamespace(operation='portrait.glare', operation_version='1',
        resource_class='gpu', params=SimpleNamespace(sha256='b' * 64))
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend': 'cpu', 'renderer_version': 'v2.2',
                           'restoration_runtime_sha': 'c' * 64}),
        check_active=lambda: None)
    calibration = SimpleNamespace(model_lock_sha='a' * 64)
    assert admission.admit(ctx, params, b'{}', None, None, calibration) is True
    assert isinstance(admission.reader(ctx, params, b'{}'), GlareWorkerObjectReader)
    assert gate.call_count == 2
    gate.return_value = False
    with pytest.raises(OperationFailure, match='GLARE_DONOR_AUTHORITY_UNAVAILABLE'):
        admission.admit(ctx, params, b'{}', None, None, calibration)


def test_glare_donor_admission_refuses_unpinned_runtime(monkeypatch, tmp_path):
    db = SimpleNamespace(glare_donor_source_ready=lambda *_: True,
                         glare_donor_input_evidence=lambda *_: None)
    objects = SimpleNamespace(get_object=lambda *_args, **_kwargs: None)
    with pytest.raises(ValueError, match='GLARE_DONOR_INSTALLATION_INVALID'):
        module.GlareDonorAdmission(db, objects, 'photos', runtime_sha='A' * 64,
            model_lock_sha='a' * 64, material_scratch_root=tmp_path,
            material_max_bytes=1024, alpha_mode='require_float32')
