"""Independent adoption refuses to inspect outputs without live authority."""
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.shown_exposure_adoption import verify_shown_exposure_output


class NoIO:
    def evidence(self,*args):
        raise AssertionError('Evidence IO before context validation')


@pytest.mark.parametrize('admission,active', [(None,lambda:None),(lambda raw:True,None)])
def test_shown_adoption_requires_live_context_before_io(admission,active):
    with pytest.raises(AdoptionInvalid,match='CONTEXT_REQUIRED'):
        verify_shown_exposure_output(NoIO(),None,None,verify_admission=admission,check_active=active)


def test_shown_adoption_stops_before_io_on_explicit_inactive():
    with pytest.raises(AdoptionInvalid,match='ADOPTION_INACTIVE'):
        verify_shown_exposure_output(NoIO(),None,None,verify_admission=lambda raw:True,check_active=lambda:False)


def test_generic_adoption_cannot_bypass_shown_copy_authority():
    from uuid import uuid4
    from photo_worker.v2.adoption import prepare_v2_adoption,ArtifactRef
    operation={'id':'semantic.adopt_shown_exposure','version':'1','output':'WorkingImage'}
    with pytest.raises(AdoptionInvalid,match='CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation=operation['id'],version='1',
            root=ArtifactRef(uuid4(),'a'*64),reader=NoIO(),
            schemas={'$defs':{}},operations={'operations':[operation]})


def test_sql_shown_admission_stops_before_query_when_inactive():
    from types import SimpleNamespace
    from photo_worker.v2.shown_exposure_admission import sql_shown_exposure_adoption_admission
    with pytest.raises(AdoptionInvalid,match='ADOPTION_INACTIVE'):
        sql_shown_exposure_adoption_admission(NoIO(),None,None,SimpleNamespace(),NoIO(),check_active=lambda:False)


@pytest.mark.parametrize('autocommit,qualification,active', [
    (False,lambda *args:True,lambda:None),
    (True,None,lambda:None), (True,lambda *args:True,None)])
def test_shown_installation_requires_explicit_qualification(autocommit,qualification,active):
    from types import SimpleNamespace
    from photo_worker.v2.shown_exposure_installation import sql_shown_exposure_adoption_installation
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_shown_exposure_adoption_installation(SimpleNamespace(autocommit=autocommit),
            verify_qualification=qualification,check_active=active)
