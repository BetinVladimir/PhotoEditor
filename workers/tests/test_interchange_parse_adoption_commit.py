from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import PreparedAdoption, AdoptionInvalid
from photo_worker.v2.interchange_parse_adoption import VerifiedParsedInterchange, PreparedParsedAdoption
from photo_worker.v2.interchange_parse_adoption_commit import interchange_parse_commit_statement


def test_parse_commit_preserves_verified_bytes_and_fence():
    tenant, task, result = uuid4(), uuid4(), uuid4()
    ctx = SimpleNamespace(operation='v2.interchange.parse', version='1', attempt=2, fence=17, result_id=result)
    inventory = ({'artifact_id':str(uuid4()),'sha256':'a'*64,'purpose':'b'*64,'ordinal':0},)
    prepared = PreparedParsedAdoption(VerifiedParsedInterchange(b'{"params":1}',b'{"result":2}',12),PreparedAdoption(inventory,'c'*64),b'[]')
    statement, values = interchange_parse_commit_statement(tenant,task,ctx,prepared)
    assert 'adopt_v30_interchange_parsed' in statement
    assert values[:5] == (tenant,task,2,17,result)
    assert values[5].obj == list(inventory)
    assert values[6:] == ('c'*64,'{"params":1}','{"result":2}','[]')
    ctx.operation = 'v2.interchange.export'
    with pytest.raises(AdoptionInvalid):
        interchange_parse_commit_statement(tenant,task,ctx,prepared)
