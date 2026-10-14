from dataclasses import asdict,replace
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_qualification_pin import RenderQualificationPin,load_render_adoption_qualification


def pin():return RenderQualificationPin('a'*64,'b'*64,'cuda','c'*64,'d'*64,'e'*64)


@pytest.mark.parametrize('field',list(RenderQualificationPin.__dataclass_fields__))
def test_each_identity_axis_changes_fingerprint(field):
    value=pin();changed=replace(value,**{field:'cpu' if field=='backend' else 'f'*64})
    assert changed.fingerprint()!=value.fingerprint()
    assert RenderQualificationPin.from_value(asdict(value))==value


@pytest.mark.parametrize('kind',['extra','missing','upper','null','backend','short'])
def test_malformed_pin_never_becomes_qualified_identity(kind):
    value=asdict(pin())
    if kind=='extra':value['other']=1
    elif kind=='missing':del value['calibration_sha']
    elif kind=='upper':value['device_profile_sha']='A'*64
    elif kind=='null':value['dataset_sha']=None
    elif kind=='backend':value['backend']='auto'
    else:value['runtime_lock_sha']='a'
    with pytest.raises(AdoptionInvalid,match='PIN_INVALID'):RenderQualificationPin.from_value(value)


@pytest.mark.parametrize('row',[None,(None,),(),({},{}),('invalid',)])
def test_primary_missing_or_malformed_pin_rejects(row):
    class Connection:
        def execute(self,query,args):return self
        def fetchone(self):return row
    context=SimpleNamespace(operation='render.compose',version='1',attempt=1,fence=2,result_id=uuid4())
    with pytest.raises(AdoptionInvalid):load_render_adoption_qualification(Connection(),uuid4(),uuid4(),context)
