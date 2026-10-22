from copy import deepcopy
from uuid import uuid4
import pytest
from photo_worker.v2.portrait_donor_qualification import donor_compiler_profile,verify_donor_qualification
from photo_worker.v2.contract_lock import SQL_TRANSPORT_SCHEMAS_SHA256, COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256
from test_portrait_donor_qualification import fixture


def sealed_fixture():
    profile,report,calibration,policy=fixture()
    sealed=deepcopy(profile)
    sealed.update(contracts_sha=SQL_TRANSPORT_SCHEMAS_SHA256,
        v2_input_manifest={'artifact_id':str(uuid4()),'sha256':'a'*64},
        v2_entry_count=20,v2_entries_sha='b'*64)
    return profile,sealed,report,calibration,policy


@pytest.mark.parametrize("contracts_sha", [SQL_TRANSPORT_SCHEMAS_SHA256, COPY_FENCE_SCHEMAS_SHA256, COPY_FENCE_DUAL_PREVIOUS_SCHEMAS_SHA256, COPY_FENCE_PREVIOUS_SCHEMAS_SHA256])
def test_qualified_sealed_profile_preserves_owned_compiler_fields(contracts_sha):
    profile,sealed,report,calibration,policy=sealed_fixture()
    sealed["contracts_sha"]=contracts_sha
    extracted=donor_compiler_profile(sealed)
    assert extracted==profile
    pin,cal=verify_donor_qualification(sealed,report,calibration,policy,check_active=lambda:None)
    expected,_=verify_donor_qualification(profile,report,calibration,policy,check_active=lambda:None)
    assert pin==expected and cal.qualified
    extracted['resource_estimate']['mutated']=True
    assert 'mutated' not in sealed['resource_estimate']


@pytest.mark.parametrize('mode',['partial','unknown','contracts','bool_count','count','digest','manifest','nil_manifest'])
def test_transport_envelope_rejects_corruption(mode):
    _,sealed,*_=sealed_fixture()
    if mode=='partial':del sealed['v2_entries_sha']
    elif mode=='unknown':sealed['extra']=1
    elif mode=='contracts':sealed['contracts_sha']='0'*64
    elif mode=='bool_count':sealed['v2_entry_count']=True
    elif mode=='count':sealed['v2_entry_count']=128
    elif mode=='digest':sealed['v2_entries_sha']='bad'
    elif mode=='manifest':sealed['v2_input_manifest']['extra']=1
    elif mode=='nil_manifest':sealed['v2_input_manifest']['artifact_id']='00000000-0000-0000-0000-000000000000'
    with pytest.raises(ValueError):donor_compiler_profile(sealed)
