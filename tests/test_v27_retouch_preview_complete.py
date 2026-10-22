"""Lifetime/budget orchestration scenarios; component IO replaced by doubles."""
from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import RetouchPreviewNumericalEvidence
from photo_worker.operations.semantic import preview_retouch_complete as module
from test_v27_retouch_preview_contract import retouch_params
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure', [None, 'media_loss', 'context_exit', 'evidence_mismatch', 'presentation_loss', 'after_close_loss', 'media_storage', 'presentation_storage'])
def test_complete_retouch_keeps_compute_alive_through_publication(monkeypatch, failure):
    params = retouch_params()
    edit, _, source = source_fixture();params['source'] = edit['source']
    live = False;lost = False
    calls = []
    def fresh():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    pixels,image = fresh(),fresh()
    proof = SimpleNamespace(changed_pixels=1,protected_unchanged=True,outside_mask_unchanged=True,alpha_unchanged=True)
    computed = SimpleNamespace(peak_bytes=1000000000,pixels=np.ones((1,2,3),np.float32),preservation=proof)
    @contextmanager
    def execute():
        nonlocal live,lost
        live = True
        try:yield computed
        finally:
            live = False;calls.append('closed')
            if failure == 'after_close_loss':lost = True
        if failure == 'context_exit':raise InterruptedError('qualification lost on exit')
    @contextmanager
    def stage(artifacts, owned, captured, **kwargs):
        with captured():
            value = RetouchPreviewNumericalEvidence(schema_version=1,params=owned,working_image=image,
                pixels=pixels,algorithm='beautyfilter_v1',changed_pixels=1,
                protected_pixels_unchanged=True,outside_mask_unchanged=True,alpha_unchanged=True,roi=[0,0,2,1])
            raw = canonical_bytes(value)
            ref = ArtifactRef(artifact_id=uuid4(),sha256='0'*64 if failure == 'evidence_mismatch' else sha256(raw).hexdigest())
            yield SimpleNamespace(image=image,pixels=pixels,evidence=ref,bytes_written=1000)
    def load(*a,**kwargs):
        assert live and kwargs['max_bytes'] == 3000000000
        return SimpleNamespace(original=np.zeros((1,2,3),np.float32),mask=np.zeros((1,2),np.float32),protected=np.zeros((1,2),np.float32))
    display = SimpleNamespace(rgb=np.zeros((1,2,3),np.float32),alpha=None)
    media = SimpleNamespace(before=display,after=display,affected=np.zeros((1,2,4),np.uint8),
        protected=np.zeros((1,2,4),np.uint8),width=2,height=1)
    def prepare(*a,**kwargs):
        assert live and kwargs['max_bytes'] == 3000000000-40
        calls.append('prepare');return media
    def stage_media(*a,**kwargs):
        nonlocal lost
        assert live and kwargs['max_output_bytes'] == 10000000-1000
        lost = failure == 'media_loss';calls.append('media')
        if failure == 'media_storage':raise OSError('media upload failed')
        return object()
    def presentation(*a,**kwargs):
        nonlocal lost
        assert live;calls.append('presentation')
        if failure == 'presentation_storage':raise OSError('presentation upload failed')
        if failure == 'presentation_loss':lost = True
        return fresh(),500
    monkeypatch.setattr(module,'execute_and_stage_retouch_preview',stage)
    monkeypatch.setattr(module,'load_retouch_preview_arrays',load)
    monkeypatch.setattr(module,'load_retouch_preview_alpha',lambda *a,**k:None)
    monkeypatch.setattr(module,'prepare_retouch_preview_media',prepare)
    monkeypatch.setattr(module,'stage_exposure_preview_media',stage_media)
    monkeypatch.setattr(module,'stage_retouch_preview_presentation',presentation)
    class Artifacts:
        def contract_bytes(self,ref,model):return source if ref == ArtifactRef.model_validate_json(canonical_bytes(params['source']['source'])) else b'{}'
    def run():return module._execute_complete_retouch_preview(Artifacts(),params,execute,
        icc_root=None,icc_lock_sha='a'*64,srgb_sha='b'*64,max_ram_bytes=4000000000,max_output_bytes=10000000,
        verify_admission=lambda _:not lost,verify_origin=lambda *a:True,verify_color=lambda _:True,
        verify_runtime=lambda:True,check_active=lambda:None)
    if failure:
        with pytest.raises(InterruptedError if failure == 'context_exit' else OSError
                if failure in ('media_storage','presentation_storage') else ValueError):run()
        if failure in ('media_loss','evidence_mismatch','media_storage'):assert 'presentation' not in calls
    else:
        result = run();assert result.bytes_written == 1500
        assert calls == ['prepare','media','presentation','closed']
    assert not live and calls[-1] == 'closed'


def test_complete_retouch_budget_refuses_boolean_and_low_memory():
    from photo_worker.contracts.images import WorkingImage
    _, _, raw = source_fixture();image = WorkingImage.model_validate_json(raw)
    for memory in (True,1000000):
        with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
            module.validate_retouch_preview_budget(image,memory,10000000)
