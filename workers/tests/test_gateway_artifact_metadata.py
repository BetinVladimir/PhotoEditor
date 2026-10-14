from email.message import Message
import pytest
from photo_worker.runtime.gateway import artifact_metadata, GatewayError

@pytest.mark.parametrize('mode', ['valid','legacy','partial','duplicate','digest','media'])
def test_immutable_gateway_metadata_headers(mode):
    headers=Message()
    if mode!='legacy':
        headers['X-Photo-Media-Type']='application/json' if mode!='media' else 'application/json; charset=utf-8'
        if mode!='partial':headers['X-Photo-Object-Version-SHA256']='a'*64 if mode!='digest' else 'not-a-digest'
    if mode=='duplicate':headers['X-Photo-Media-Type']='application/json'
    if mode=='legacy':assert artifact_metadata(headers,32) is None
    elif mode=='valid':
        result=artifact_metadata(headers,32)
        assert (result.bytes,result.media_type,result.version_sha256)==(32,'application/json','a'*64)
    else:
        with pytest.raises(GatewayError):artifact_metadata(headers,32)
