from types import SimpleNamespace
import pytest
from urllib3.exceptions import ReadTimeoutError,SSLError
from photo_worker.runtime.semantic_objects import SemanticObjectReader
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('during_read',[False,True])
def test_transient_failure_has_retryable_sanitised_task_error(during_read):
    def fail(*args,**kwargs):raise ReadTimeoutError(None,'secret-location','credential-text')
    response=SimpleNamespace(read=fail,close=lambda:None,release_conn=lambda:None)
    client=SimpleNamespace(get_object=(lambda *args,**kwargs:response) if during_read else fail)
    reader=SemanticObjectReader(client)
    with pytest.raises(OperationFailure) as error:
        stream=reader.get_object('owned','key',version_id='exact')
        stream.read(10)
    assert error.value.error.code=='DEPENDENCY_UNAVAILABLE' and error.value.error.retryable
    assert 'credential' not in str(error.value)


def test_tls_failure_cannot_be_retried_as_transient():
    def fail(*args,**kwargs):raise SSLError('untrusted certificate')
    with pytest.raises(OperationFailure) as error:SemanticObjectReader(SimpleNamespace(get_object=fail)).get_object('owned','key',version_id='exact')
    assert error.value.error.code=='CONFIG_INVALID' and not error.value.error.retryable


def test_exact_version_and_response_cleanup_are_preserved():
    events=[]
    response=SimpleNamespace(read=lambda n:b'body'[:n],close=lambda:events.append('close'),release_conn=lambda:events.append('release'))
    def get(bucket,key,*,version_id):
        assert (bucket,key,version_id)==('owned','key','old-version');return response
    stream=SemanticObjectReader(SimpleNamespace(get_object=get)).get_object('owned','key',version_id='old-version')
    assert stream.read(3)==b'bod'
    stream.close();stream.release_conn()
    assert events==['close','release']


@pytest.mark.parametrize('code,expected,retryable',[('SlowDown','DEPENDENCY_UNAVAILABLE',True),('NoSuchVersion','ARTIFACT_MISSING',False),('AccessDenied','CONFIG_INVALID',False)])
def test_s3_errors_keep_retry_policy_distinct(code,expected,retryable):
    from minio.error import S3Error
    def fail(*args,**kwargs):raise S3Error(None,code,'private-location','resource','request','host')
    with pytest.raises(OperationFailure) as error:SemanticObjectReader(SimpleNamespace(get_object=fail)).get_object('owned','key',version_id='exact')
    assert error.value.error.code==expected and error.value.error.retryable is retryable
    assert 'private-location' not in str(error.value)
