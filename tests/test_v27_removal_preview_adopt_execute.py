from photo_worker.contracts.base import canonical_bytes
import pytest
from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput
from photo_worker.operations.semantic.preview_removal_adopt_execute import execute_shown_removal_adoption
from test_v27_preview_adoption_contract import fixture as exposure_fixture
from uuid import UUID,uuid5
from photo_worker.contracts.semantic_preview import ShownExposureAdoptionInput


def fixture():
    value=ShownExposureAdoptionInput.model_validate(exposure_fixture()).model_dump(mode='json')
    value['preview_task_id']=str(uuid5(UUID(str(value['preview_run_id'])),'semantic.preview_remove_object:'+str(value['target_id'])))
    return value


@pytest.mark.parametrize('accepted',[False,None,1])
def test_adoption_requires_explicit_live_authority_before_documents(accepted):
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('unadmitted document IO')
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_ADOPTION_DENIED'):
        execute_shown_removal_adoption(Forbidden(),ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(fixture())),
            verify_admission=lambda _:accepted,verify_runtime=lambda:True,
            verify_color=lambda _:True,check_active=lambda:None,max_output_bytes=2**40,expected_pixel_count=1)


@pytest.mark.parametrize('read_number', range(1, 8))
@pytest.mark.parametrize('loss', ['admission', 'runtime', 'cancel'])
def test_copy_stops_immediately_after_each_document_read(monkeypatch, read_number, loss):
    from photo_worker.operations.semantic import preview_removal_adopt_execute as module
    from photo_worker.runtime.executor import TaskCancelled
    from test_v27_removal_preview_result_binding import fixture as preview_fixture
    presentation_raw = preview_fixture()[2]
    reads = []; revoked = False

    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal revoked
            reads.append(model.__name__)
            if len(reads) == read_number:
                revoked = True
                return b'not-json'
            return presentation_raw if model.__name__ == 'RemovalPreviewPresentation' else b'{}'

    def active():
        if revoked and loss == 'cancel':
            raise TaskCancelled('cancelled during copy document read')
    def forbidden(*args, **kwargs):
        raise AssertionError('binding or staging after authority loss')
    monkeypatch.setattr(module, 'bind_shown_removal_adoption', forbidden)
    monkeypatch.setattr(module, 'stage_shown_preview_image', forbidden)
    with pytest.raises(TaskCancelled if loss == 'cancel' else ValueError):
        execute_shown_removal_adoption(Artifacts(), fixture(),
            verify_admission=lambda _: not (revoked and loss == 'admission'),
            verify_runtime=lambda: not (revoked and loss == 'runtime'),
            verify_color=lambda _: True, check_active=active,
            max_output_bytes=2**40, expected_pixel_count=64)
    assert len(reads) == read_number


def test_removal_copy_rejects_noncanonical_approved_document_before_other_binding():
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.semantic.preview_removal_adopt_binding import bind_shown_removal_adoption
    from test_v27_approved_edit_binding import documents
    params = fixture()
    raw = canonical_bytes(documents()[0]) + b' '
    params['approved_edit']['sha256'] = sha256(raw).hexdigest()
    with pytest.raises(ValueError, match='ADOPTION_DOCUMENT_INVALID'):
        bind_shown_removal_adoption(params, raw, b'not-json', b'not-json', b'not-json',
            b'not-json', b'not-json', b'not-json')
