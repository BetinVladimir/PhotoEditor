import pytest
from photo_worker.operations.semantic.preview_complete import execute_complete_exposure_preview
from test_v27_exposure_preview_binding import fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('field', ['ram', 'output'])
def test_complete_preview_rejects_budget_before_compute(tmp_path, monkeypatch, field):
    params, _, _ = fixture()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    class Artifacts:
        def contract_bytes(self, ref, model):
            return raw
    def forbidden(*args, **kwargs):
        raise AssertionError('compute reached before full presentation budget')
    monkeypatch.setattr('photo_worker.operations.semantic.preview_complete.execute_artifact_exposure_preview', forbidden)
    with pytest.raises(ValueError, match='SEMANTIC_EXPOSURE_RESOURCE_PROFILE'):
        execute_complete_exposure_preview(Artifacts(), params, icc_root=tmp_path,
            icc_lock_sha='a'*64, srgb_sha='b'*64,
            max_bytes=1 if field == 'ram' else 2**40,
            max_output_bytes=1 if field == 'output' else 2**40,
            verify_admission=lambda _: True, verify_origin=lambda *a: True,
            verify_color=lambda *a: True, verify_runtime=lambda: True, check_active=lambda: None)
