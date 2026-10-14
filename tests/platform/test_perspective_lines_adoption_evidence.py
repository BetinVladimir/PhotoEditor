from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import perspective_lines_adoption_evidence as module
from photo_worker.v2.adoption import AdoptionInvalid


def test_checkpoint_owns_original_context_and_rejects_source_change(monkeypatch):
    context=SimpleNamespace(attempt=1,fence=4,result_id=uuid4())
    value={'source':{'object_version':'v1'}}
    seen=[]
    def load(*args,**kwargs):
        seen.append(args[3].fence)
        return value
    monkeypatch.setattr(module,'load_perspective_lines_adoption_evidence',load)
    check=module.perspective_lines_adoption_checkpoint(None,uuid4(),uuid4(),context,b'params',value,check_active=lambda:None)
    context.fence=5
    assert check()==value and seen==[4]
    value['source']['object_version']='v2'
    with pytest.raises(AdoptionInvalid,match='ADOPTION_CHANGED'):check()


def test_loader_refuses_transactional_connection_and_oversized_params():
    with pytest.raises(ValueError,match='READ_CONFIGURATION'):
        module.load_perspective_lines_adoption_evidence(SimpleNamespace(autocommit=False),uuid4(),uuid4(),None,b'{}',check_active=lambda:None)
    with pytest.raises(AdoptionInvalid,match='PARAMS_INVALID'):
        module.load_perspective_lines_adoption_evidence(SimpleNamespace(autocommit=True),uuid4(),uuid4(),None,b'x'*65537,check_active=lambda:None)


def test_array_locations_require_order_versions_bucket_and_no_aliases():
    from copy import deepcopy
    source={'artifact_id':str(uuid4()),'bucket':'photos'}
    def location():return dict(artifact_id=str(uuid4()),sha256='a'*64,bucket='photos',
        object_key='source/pixels',object_version='v1',bytes=256,media_type='application/x-npy')
    arrays=[{'role':'WorkingPixels','object':location()},{'role':'WorkingAlpha','object':location()}]
    module.validate_array_locations(arrays,source)
    for mutate in [
        lambda a:a.reverse(),
        lambda a:a[0]['object'].update(object_version='null'),
        lambda a:a[0]['object'].update(bucket='foreign'),
        lambda a:a[1]['object'].update(artifact_id=a[0]['object']['artifact_id']),
        lambda a:a[0]['object'].update(artifact_id=source['artifact_id']),
        lambda a:a[0]['object'].update(bytes=True),
        lambda a:a[0]['object'].update(extra=1),
    ]:
        changed=deepcopy(arrays);mutate(changed)
        with pytest.raises(ValueError):module.validate_array_locations(changed,source)
    with pytest.raises(ValueError):module.validate_array_locations([],source)
