from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2 import exposure_installation as module
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('qualified',[True,False,1])
def test_factory_requires_report_qualification_and_origin(monkeypatch,qualified):
    edit,image,_=source_fixture();edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    color=WorkingImage.model_validate_json(canonical_bytes(image)).color
    pin=ContractRef(artifact_id=uuid4(),sha256='a'*64);params=ArtifactRef(uuid4(),'b'*64);calls=[]
    monkeypatch.setattr(module,'sql_exposure_adoption_admission',lambda *a,**k:(params,edit,lambda value:True))
    monkeypatch.setattr(module,'load_exposure_protection_pin',lambda *a,**k:(pin,SimpleNamespace(artifact=pin)))
    monkeypatch.setattr(module,'sql_exposure_adoption_origin',lambda *a,**k:lambda *args:True)
    def verify(*args):calls.append(args);return qualified
    factory=module.sql_exposure_adoption_installation(SimpleNamespace(autocommit=True),
        working_icc_sha=color.icc_sha256,verify_qualification=verify,check_active=lambda:None)
    context=SimpleNamespace(attempt=1,fence=2,result_id=uuid4())
    if qualified is not True:
        with pytest.raises(AdoptionInvalid):factory(uuid4(),uuid4(),context,object())
    else:
        installed=factory(uuid4(),uuid4(),context,object())
        assert installed.params==params and installed.verify_color(color) is True
        assert installed.verify_color(color.model_copy(update={'icc_sha256':'f'*64})) is False
        assert installed.verify_admission(edit) is True and len(calls)==2


def test_factory_has_no_qualification_default():
    with pytest.raises(ValueError):
        module.sql_exposure_adoption_installation(SimpleNamespace(autocommit=True),
            working_icc_sha='a'*64,verify_qualification=None,check_active=lambda:None)
