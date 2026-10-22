"""Private background-cleanup kernel and content binding, not deployment proof."""
from hashlib import sha256

import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.semantic.preview_background_cleanup import compute_background_cleanup_preview
from photo_worker.operations.semantic.preview_binding import bind_background_cleanup_preview_documents
from test_v27_removal_preview_binding import removal_fixture
from test_v27_removal_preview import Engine, arrays


def background_fixture():
    params, candidate, raw = removal_fixture()
    params['kind'] = 'background_cleanup'
    intent = parse_json(raw)
    intent['kind'] = 'background_cleanup'
    raw = canonical_bytes(intent)
    params['intent']['sha256'] = sha256(raw).hexdigest()
    return params, candidate, raw


def compute(value, params, engine, admission=lambda _: True, strength=1.):
    return compute_background_cleanup_preview(value,params,engine,
        verify_admission=admission,max_bytes=1024**3,check_active=lambda: None,strength=strength)


def test_background_cleanup_binding_retains_own_kind_and_no_approval():
    params, candidate, raw = background_fixture()
    owned, _, intent = bind_background_cleanup_preview_documents(params,candidate,raw)
    assert owned.kind == intent.kind == 'background_cleanup'
    assert 'confirmed' not in owned.model_dump()
    assert 'approval_id' not in owned.model_dump()


def test_background_cleanup_preserves_source_protection_and_outside_bits():
    params, _, _ = background_fixture()
    value = arrays()
    before = value.original.copy()
    engine = Engine()
    seen = []
    result = compute(value,params,engine,lambda p: seen.append(p.kind) or True)
    assert engine.calls == 1 and result.preservation.changed_pixels > 0
    assert seen and set(seen) == {'background_cleanup'}
    keep = (value.mask < .5) | (value.protected > 0)
    np.testing.assert_array_equal(result.candidate[keep].view(np.uint32),before[keep].view(np.uint32))
    np.testing.assert_array_equal(value.original.view(np.uint32),before.view(np.uint32))


@pytest.mark.parametrize('kind', ['remove_object','retouch_region','local_exposure'])
def test_other_kinds_cannot_enter_background_cleanup_kernel(kind):
    params, _, _ = background_fixture()
    params['kind'] = kind
    engine = Engine()
    with pytest.raises(ValueError):
        compute(arrays(),params,engine)
    assert engine.calls == 0


def test_background_cleanup_admission_loss_after_inference_has_no_result():
    params, _, _ = background_fixture()
    engine = Engine()
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        compute(arrays(),params,engine,lambda _: engine.calls == 0)
    assert engine.calls == 1


def test_background_cleanup_zero_strength_is_exact_source():
    params, _, _ = background_fixture()
    value = arrays()
    result = compute(value,params,Engine(),strength=0.)
    assert result.preservation.changed_pixels == 0
    np.testing.assert_array_equal(result.candidate.view(np.uint32),value.original.view(np.uint32))


@pytest.mark.parametrize('read_number', [1, 2])
@pytest.mark.parametrize('loss', ['cancel', 'admission'])
def test_background_cleanup_compute_entry_fences_proposal_reads(monkeypatch, read_number, loss):
    from photo_worker.operations.semantic import preview_background_cleanup as module
    params, candidate_raw, intent_raw = background_fixture()
    reads = []
    lost = False
    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal lost
            reads.append(model.__name__)
            if len(reads) == read_number:
                lost = True
            return candidate_raw if model.__name__ == 'TransferCandidate' else intent_raw
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during proposal read')
    def forbidden(*args, **kwargs):
        raise AssertionError('array IO reached after proposal authority loss')
    monkeypatch.setattr('photo_worker.operations.semantic.preview_background_cleanup_loader.load_background_cleanup_preview_arrays', forbidden)
    engine = Engine()
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        module.execute_artifact_background_cleanup_preview(Artifacts(), params, engine,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda _: True,
            max_bytes=1024**3, check_active=active)
    assert reads == ['TransferCandidate', 'EditIntent'][:read_number]
    assert engine.calls == 0
