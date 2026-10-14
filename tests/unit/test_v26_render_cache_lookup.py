from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
from photo_worker.v2.render_cache_lookup import lookup_render_cache
from test_v26_render_cache_identity import values


@pytest.mark.parametrize('damage',['root','ordinal','purpose','result','shape','reference'])
def test_malformed_primary_candidate_is_never_returned(damage):
    tenant,recipe,profile,parents,pin=values()
    inputs=RenderAdoptionInputs(recipe,canonical_bytes(profile),parents,pin)
    root={'artifact_id':str(uuid4()),'sha256':'a'*64}
    entry={**root,'purpose':'b'*64,'ordinal':0}
    row=[uuid4(),root,[entry]]
    if damage=='root':root['extra']=True
    elif damage=='ordinal':entry['ordinal']=True
    elif damage=='purpose':entry['purpose']='BAD'
    elif damage=='result':row[0]='not-a-uuid'
    elif damage=='shape':row.pop()
    else:entry['artifact_id']=str(uuid4())
    class DB:
        def execute(self,query,args):return self
        def fetchone(self):return row
    with pytest.raises(AdoptionInvalid):lookup_render_cache(DB(),tenant,uuid4(),1,1,inputs)


def test_primary_miss_preserves_recompute_path():
    tenant,recipe,profile,parents,pin=values()
    inputs=RenderAdoptionInputs(recipe,canonical_bytes(profile),parents,pin)
    class DB:
        def execute(self,query,args):
            assert 'lookup_v26_render_cache' in query
            assert parse_json(args[5])['qualification']['device_profile_sha']==pin.device_profile_sha
            return self
        def fetchone(self):return None
    assert lookup_render_cache(DB(),tenant,uuid4(),1,1,inputs) is None
