import pytest
from photo_worker.v2.multi_adopter_config import multi_adopter_from_environment


def test_disabled_adopter_needs_no_runtime_directories_or_evidence():
    assert multi_adopter_from_environment('optical.panorama',lambda key,default='':default) is None


@pytest.mark.parametrize('value',['yes','1','TRUE'])
def test_invalid_enablement_refused(value):
    with pytest.raises(ValueError,match='enablement'):
        multi_adopter_from_environment('optical.panorama',lambda key,default='':value if key=='PHOTO_ADOPTER_PANORAMA_ENABLED' else default)


@pytest.mark.parametrize('budget',['0','-1','1.5','9007199254740992','true'])
def test_invalid_replay_budget_refused_before_file_io(budget):
    def getenv(key,default=''):
        if key=='PHOTO_ADOPTER_PANORAMA_ENABLED':return 'true'
        if key.endswith('_MAX_BYTES'):return budget
        return default
    with pytest.raises(ValueError,match='resource limit'):multi_adopter_from_environment('optical.panorama',getenv)
