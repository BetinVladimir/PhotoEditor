import json
from pathlib import Path
from hashlib import sha256
import pytest
from jsonschema import Draft202012Validator,FormatChecker
from photo_worker.v2.contract_lock import SCHEMAS_SHA256,EXPOSURE_PREVIOUS_SCHEMAS_SHA256
from test_v27_approved_semantic_edit_contract import fixture

ROOT=Path(__file__).resolve().parents[1]/'docs/implementation-v2/contracts'


def test_catalog_and_hash_pin():
    raw=(ROOT/'schemas.json').read_bytes();assert sha256(raw).hexdigest()==SCHEMAS_SHA256
    assert EXPOSURE_PREVIOUS_SCHEMAS_SHA256=='dd261b1a9a7bb7d4775155dc25d6e672f068e6abb5d072bc2ddc5de6279ccb87'
    op=next(x for x in json.loads((ROOT/'operations.json').read_text())['operations'] if x['id']=='semantic.local_exposure')
    assert op['params']=='ApprovedSemanticEdit' and op['output']=='ImageResult'
    assert op['resource_class']=='cpu' and set(op['role_policies'])=={'document','pixels','working_image','report'}


@pytest.mark.parametrize('field,value',[('ev',None),('preserve_persons',False),('schema_version',True),('target_id','00000000-0000-0000-0000-000000000000')])
def test_schema_rejects_invalid_approved_edit(field,value):
    schemas=json.loads((ROOT/'schemas.json').read_text())
    validator=Draft202012Validator({**schemas,'$ref':'#/$defs/ApprovedSemanticEdit'},format_checker=FormatChecker())
    body=fixture('local_exposure');assert validator.is_valid(body)
    body[field]=value;assert not validator.is_valid(body)
