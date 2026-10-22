import pytest
import numpy as np
from photo_worker.contracts.base import parse_json
from photo_worker.operations.semantic.preview_loader import load_exposure_preview_arrays
from test_v27_exposure_loader import loader_fixture
from test_v27_exposure_preview_binding import fixture


def setup(tmp_path):
    edit,root,artifacts,events=loader_fixture(tmp_path)
    from types import SimpleNamespace
    intent=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['intent']['artifact_id']),None))
    params,_,_=fixture();params.update(source=edit['source'],candidate=edit['candidate'],intent=edit['intent'],protection=root,snapshot_id=intent['target_snapshot_id'])
    return params,artifacts,events


@pytest.mark.parametrize('failure',[None,'admission','origin','icc','budget'])
def test_preview_loader_materializes_owned_arrays_and_closes_mappings(tmp_path,failure):
    params,artifacts,events=setup(tmp_path)
    options=dict(verify_admission=lambda params:failure!='admission',verify_origin=lambda *args:failure!='origin',verify_color=lambda color:failure!='icc',max_bytes=64*1024**2 if failure=='budget' else 128*1024**2,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):load_exposure_preview_arrays(artifacts,params,**options)
        assert events==[]
    else:
        result=load_exposure_preview_arrays(artifacts,params,**options)
        np.testing.assert_array_equal(result.original,np.full((8,8,3),.5,np.float32))
        assert all(value.flags.owndata for value in (result.original,result.mask,result.protected))
        assert events==['open','open','open','close','close','close']


def test_artifact_preview_entry_computes_real_npy_without_apply(tmp_path):
    from photo_worker.operations.semantic.preview_loader import execute_artifact_exposure_preview
    params,artifacts,events=setup(tmp_path)
    result=execute_artifact_exposure_preview(artifacts,params,
        verify_admission=lambda value:True,verify_origin=lambda *args:True,
        verify_color=lambda value:True,max_bytes=128*1024**2,check_active=lambda:None)
    assert result.dtype==np.float32 and result.shape==(8,8,3)
    assert np.all(result>.5)
    assert events==['open','open','open','close','close','close']


def test_artifact_preview_denies_authority_before_document_io():
    from photo_worker.operations.semantic.preview_loader import execute_artifact_exposure_preview
    params,_,_=fixture()
    class ForbiddenArtifacts:
        def contract_bytes(self,*args):raise AssertionError('read before admission')
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        execute_artifact_exposure_preview(ForbiddenArtifacts(),params,
            verify_admission=lambda value:False,verify_origin=lambda *args:True,
            verify_color=lambda value:True,max_bytes=128*1024**2,check_active=lambda:None)


@pytest.mark.parametrize('read_number', range(1, 8))
@pytest.mark.parametrize('loss', ['cancel', 'admission'])
@pytest.mark.parametrize('operation', ['local_exposure', 'background_cleanup', 'retouch_region'])
def test_preview_loader_fences_each_document_read(tmp_path, read_number, loss, operation):
    if operation == 'retouch_region':
        from test_v27_retouch_preview_loader import retouch_documents
        from photo_worker.operations.semantic.preview_retouch_loader import load_retouch_preview_arrays
        params, source, events = retouch_documents(tmp_path)
        load = load_retouch_preview_arrays
    elif operation == 'background_cleanup':
        from test_v27_background_cleanup_preview_loader import background_documents
        from photo_worker.operations.semantic.preview_background_cleanup_loader import load_background_cleanup_preview_arrays
        params, source, events = background_documents(tmp_path)
        load = load_background_cleanup_preview_arrays
    else:
        params, source, events = setup(tmp_path)
        load = load_exposure_preview_arrays
    reads = []
    lost = False
    class Artifacts:
        input = source.input
        def contract_bytes(self, ref, model):
            nonlocal lost
            raw = source.contract_bytes(ref, model)
            reads.append(ref.artifact_id)
            if len(reads) == read_number:
                lost = True
            return raw
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during input read')
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        load(Artifacts(), params,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda _: True,
            max_bytes=128*1024**2, check_active=active)
    assert len(reads) == read_number
    assert events == []


@pytest.mark.parametrize('open_number', [1, 2, 3])
@pytest.mark.parametrize('loss', ['cancel', 'admission'])
@pytest.mark.parametrize('operation', ['local_exposure', 'background_cleanup', 'retouch_region'])
def test_preview_loader_closes_inputs_when_authority_changes_during_open(tmp_path, open_number, loss, operation):
    from contextlib import contextmanager
    if operation == 'retouch_region':
        from test_v27_retouch_preview_loader import retouch_documents
        from photo_worker.operations.semantic.preview_retouch_loader import load_retouch_preview_arrays
        params, source, events = retouch_documents(tmp_path)
        load = load_retouch_preview_arrays
    elif operation == 'background_cleanup':
        from test_v27_background_cleanup_preview_loader import background_documents
        from photo_worker.operations.semantic.preview_background_cleanup_loader import load_background_cleanup_preview_arrays
        params, source, events = background_documents(tmp_path)
        load = load_background_cleanup_preview_arrays
    else:
        params, source, events = setup(tmp_path)
        load = load_exposure_preview_arrays
    opened = 0
    lost = False
    class Artifacts:
        contract_bytes = source.contract_bytes
        @contextmanager
        def input(self, ref, *, suffix):
            nonlocal opened, lost
            with source.input(ref, suffix=suffix) as local:
                opened += 1
                if opened == open_number:
                    lost = True
                yield local
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during array open')
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        load(Artifacts(), params,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda _: True,
            max_bytes=128*1024**2, check_active=active)
    assert opened == open_number
    assert events == ['open']*open_number + ['close']*open_number
