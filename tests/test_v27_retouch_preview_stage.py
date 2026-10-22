"""Own retouch pixel writer scenarios; no production adoption evidence."""
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_retouch_stage import stage_retouch_preview
from test_v27_retouch_preview_contract import retouch_params
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure', [None, 'denied', 'integer_true', 'late_denied', 'alias', 'cancel'])
def test_retouch_stage_preserves_exact_metadata_and_fails_closed(tmp_path, failure):
    params = retouch_params()
    edit, image, raw = source_fixture()
    params['source'] = edit['source']
    pixels = np.full((1, 2, 3), .6, np.float32)
    calls = []
    class Artifacts:
        def stage(self, path, reservation, role, media):
            calls.append(role)
            np.testing.assert_array_equal(np.load(path, allow_pickle=False), pixels)
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest())
        def stage_contract(self, value, role):
            calls.append(role)
            result = value.model_dump(mode='json')
            for key in ('color', 'canvas', 'transform_chain', 'alpha'):
                assert result[key] == image[key]
            return ArtifactRef(artifact_id=params['intent']['artifact_id'] if failure == 'alias' else uuid4(),
                sha256=sha256(canonical_bytes(value)).hexdigest())
    def admitted(value):
        assert value.kind == 'retouch_region'
        if failure == 'denied':return False
        if failure == 'integer_true':return 1
        if failure == 'late_denied' and calls:return False
        return True
    def active():
        if failure == 'cancel' and calls:raise InterruptedError('cancelled')
    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        options = dict(verify_admission=admitted, check_active=active,
            max_bytes=128*1024**2, max_output_bytes=2*1024**2)
        if failure:
            with pytest.raises(InterruptedError if failure == 'cancel' else ValueError):
                stage_retouch_preview(artifacts, params, raw, pixels, **options)
            if failure in ('denied','integer_true'):assert calls == []
            if failure in ('late_denied','cancel'):assert calls == ['pixels']
        else:
            result = stage_retouch_preview(artifacts, params, raw, pixels, **options)
            assert result.bytes_written > pixels.nbytes
            assert calls == ['pixels','working_image']


@pytest.mark.parametrize('failure', [None, 'proof', 'evidence_hash', 'evidence_alias', 'late_denied'])
def test_retouch_compute_lifetime_and_exact_evidence(monkeypatch, failure):
    from contextlib import contextmanager
    from types import SimpleNamespace
    from photo_worker.operations.semantic import preview_retouch_stage as module
    from photo_worker.operations.semantic.candidate_preservation import CandidatePreservation
    from photo_worker.operations.semantic.preview_retouch_compute import RetouchPreviewComputed
    params = retouch_params()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    live = False
    lost = False
    calls = []
    def ref():return ArtifactRef(artifact_id=uuid4(), sha256='f'*64)
    staged = SimpleNamespace(pixels=ref(), image=ref(), bytes_written=1000)
    @contextmanager
    def execute():
        nonlocal live
        live = True
        try:
            yield RetouchPreviewComputed(np.ones((1,2,3),np.float32), None,
                CandidatePreservation(2, 1, True, failure != 'proof', True), (), 1048576)
        finally:
            live = False
            calls.append('closed')
    def stage(*args, **kwargs):
        assert live and kwargs['max_bytes'] == (1 << 30)-1048576
        calls.append('image')
        return staged
    monkeypatch.setattr(module, 'stage_retouch_preview', stage)
    class Artifacts:
        def contract_bytes(self, supplied, model):
            assert supplied == ArtifactRef.model_validate_json(canonical_bytes(params['source']['source']))
            return raw
        def stage_contract(self, value, role):
            nonlocal lost
            assert live and role == 'preview_evidence'
            assert value.roi == [0,0,2,1] and value.changed_pixels == 1
            assert value.alpha_unchanged is True and value.algorithm == 'beautyfilter_v1'
            calls.append('evidence')
            lost = failure == 'late_denied'
            return ArtifactRef(artifact_id=staged.image.artifact_id if failure == 'evidence_alias' else uuid4(),
                sha256='0'*64 if failure == 'evidence_hash' else sha256(canonical_bytes(value)).hexdigest())
    def run():
        with module.execute_and_stage_retouch_preview(Artifacts(), params, execute,
                verify_admission=lambda _:not lost, check_active=lambda:None,
                max_ram_bytes=1 << 30, max_output_bytes=4*1048576) as result:
            assert live and result.bytes_written > 1000
            calls.append('yielded')
    if failure:
        with pytest.raises(ValueError):run()
        assert 'yielded' not in calls
        if failure == 'proof':assert 'image' not in calls
    else:
        run()
        assert calls == ['image','evidence','yielded','closed']
    assert not live and calls[-1] == 'closed'
