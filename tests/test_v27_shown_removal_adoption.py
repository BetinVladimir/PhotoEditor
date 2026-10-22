import pytest
from uuid import uuid4
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef, prepare_v2_adoption
from photo_worker.v2.shown_removal_adoption import verify_shown_removal_output


class NoIO:
    def evidence(self, *args):
        raise AssertionError('unauthorized evidence IO')


def test_removal_adopter_requires_own_live_context():
    with pytest.raises(AdoptionInvalid, match='CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='semantic.adopt_shown_removal',version='1',
            root=ArtifactRef(uuid4(),'a'*64),reader=NoIO(),schemas={'$defs':{}},
            operations={'operations':[{'id':'semantic.adopt_shown_removal','version':'1','output':'WorkingImage'}]})


def test_removal_output_verifier_stops_on_inactive_context():
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_shown_removal_output(NoIO(),None,None,verify_admission=lambda raw:True,check_active=lambda:False)
