from types import SimpleNamespace
from uuid import uuid4
import pytest
from test_perspective_evidence import fixture
from photo_worker.contracts.optical_perspective import OpticalPerspectiveParams
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2 import perspective_adoption_prepare as module


def test_invalid_installation_rejected_before_runtime_or_io():
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        module.validate_perspective_adoption_installation(None)


def test_changed_params_ref_refused_before_original_objects_or_replay(monkeypatch):
    raw,lease,record,bodies=fixture();intent=OpticalPerspectiveParams.model_validate_json(raw)
    from hashlib import sha256
    ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());calls=[]
    monkeypatch.setattr(module,'validate_perspective_adoption_installation',lambda value:value)
    monkeypatch.setattr(module,'_document',lambda *args:intent)
    def evidence(*args,**kwargs):
        calls.append('evidence');return {'params_ref':{'artifact_id':str(uuid4()),'sha256':ref.sha256}}
    monkeypatch.setattr(module,'load_perspective_adoption_evidence',evidence)
    monkeypatch.setattr(module,'verify_pinned_perspective_replay',lambda *args,**kwargs:pytest.fail('must not replay stale params'))
    with pytest.raises(ValueError,match='PARAMS_CHANGED'):
        module.prepare_perspective_reconstruction(None,None,uuid4(),uuid4(),SimpleNamespace(root=ref),ref,
            installation=SimpleNamespace(),check_active=lambda:None)
    assert calls==['evidence']
