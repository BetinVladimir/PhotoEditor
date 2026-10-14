from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.v2 import relight_adoption_authority as module
from test_relight_inputs import inputs


@pytest.mark.parametrize('mode',['ok','depth','manual','qualification','mutation','unsupported'])
def test_completed_authority_rechecks_dependencies_and_qualification(monkeypatch,tmp_path,mode):
    params,source,depth,subject,guard,calibration,*_=inputs()
    subject=subject.model_copy(update={'selector':'person'})
    params=params.model_copy(update={'subject':params.subject.model_copy(update={'sha256':canonical_hash(subject)})})
    guard=guard.model_copy(update={'selector':'manual'})
    if mode=='unsupported':guard=guard.model_copy(update={'selector':'prompt'})
    params=params.model_copy(update={'protected':params.protected.model_copy(update={'sha256':canonical_hash(guard)})})
    calls=[]
    def source_gate(*a,**k):calls.append('source');return {'fixture':True}
    monkeypatch.setattr(module,'load_relight_adoption_source',source_gate)
    monkeypatch.setattr(module,'relight_adoption_source_checkpoint',lambda *a,**k:lambda:calls.append('checkpoint'))
    def verify_depth(*a,**k):calls.append('depth');return mode!='depth'
    def verify_manual(*a,**k):calls.append('manual');assert k['protected'] is True;return mode!='manual'
    monkeypatch.setattr(module,'verify_relight_adoption_depth',verify_depth)
    monkeypatch.setattr(module,'verify_relight_adoption_manual',verify_manual)
    monkeypatch.setattr(module,'verify_relight_adoption_person',lambda *a,**k:calls.append('person') or True)
    def qualification(*a):
        calls.append('qualification')
        if mode=='mutation':params.strength=.2
        return mode!='qualification'
    authority=module.CompletedRelightAuthority(SimpleNamespace(autocommit=True),None,uuid4(),uuid4(),SimpleNamespace(),
        scratch_root=tmp_path,max_bytes=10000,verify_qualification=qualification,check_active=lambda:None)
    if mode=='ok':
        assert authority(params,source,depth,subject,guard,calibration) is True
        assert calls.count('qualification')==2 and calls.count('depth')==1
        assert calls.count('person')==1 and calls.count('manual')==1
    else:
        with pytest.raises(ValueError):authority(params,source,depth,subject,guard,calibration)
        if mode=='unsupported':assert calls==[]


def test_installed_factory_requires_runtime_before_and_after_qualification(monkeypatch,tmp_path):
    from photo_worker.operations.portrait import relight_runtime_lock
    from photo_worker.v2 import relight_adoption_qualification
    events=[]
    monkeypatch.setattr(relight_runtime_lock,'verify_installed_runtime',lambda *a:events.append('runtime'))
    monkeypatch.setattr(relight_adoption_qualification,'verify_completed_relight_qualification',lambda *a,**k:events.append('qualification') or True)
    context=SimpleNamespace(attempt=1)
    authority=module.qualified_completed_relight_authority(SimpleNamespace(autocommit=True),None,
        uuid4(),uuid4(),context,scratch_root=tmp_path,max_bytes=1000,runtime_lock={},
        expected_runtime_sha='a'*64,check_active=lambda:None)
    assert events==['runtime']
    assert authority.qualification({},None,None) is True
    assert events==['runtime','runtime','qualification','runtime']


def test_coordinator_relight_requires_explicit_installation_before_reader_io(monkeypatch):
    from photo_worker.v2 import adoption_service
    from photo_worker.v2.adoption import ArtifactRef
    connection=SimpleNamespace(autocommit=True)
    coordinator=adoption_service.AdoptionCoordinator(connection,SimpleNamespace(),None,'bucket')
    context=adoption_service.AdoptionContext(1,1,uuid4(),'image.relight','1',ArtifactRef(uuid4(),'a'*64))
    monkeypatch.setattr(coordinator,'context',lambda *a:context)
    monkeypatch.setattr(adoption_service,'PostgresObjectEvidenceReader',lambda *a,**k:None)
    with pytest.raises(ValueError,match='RELIGHT_ADOPTION_INSTALLATION_REQUIRED'):
        coordinator.adopt(uuid4(),uuid4())


def test_coordinator_rejects_untyped_relight_installation():
    from photo_worker.v2.adoption_service import AdoptionCoordinator
    with pytest.raises(ValueError,match='Invalid relight'):
        AdoptionCoordinator(SimpleNamespace(autocommit=True),SimpleNamespace(),None,'bucket',relight_installation=object())
