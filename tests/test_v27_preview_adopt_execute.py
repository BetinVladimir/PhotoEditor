import pytest
from photo_worker.contracts.semantic_preview import ShownExposureAdoptionInput
from photo_worker.operations.semantic.preview_adopt_execute import execute_shown_exposure_adoption
from test_v27_preview_adoption_contract import fixture


@pytest.mark.parametrize('accepted',[False,None,1])
def test_adoption_requires_explicit_live_authority_before_documents(accepted):
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('unadmitted document IO')
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_ADOPTION_DENIED'):
        execute_shown_exposure_adoption(Forbidden(),ShownExposureAdoptionInput.model_validate(fixture()),
            verify_admission=lambda _:accepted,verify_runtime=lambda:True,
            verify_color=lambda _:True,check_active=lambda:None,max_output_bytes=2**40,expected_pixel_count=1)
