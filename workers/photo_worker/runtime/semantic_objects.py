"""Worker-only S3 fault classification; exact-version/content gates are external."""
from minio.error import S3Error
from urllib3.exceptions import TimeoutError as HTTPTimeout,ProtocolError,NewConnectionError,MaxRetryError,SSLError
from .executor import OperationFailure


def _call(function,*args,**kwargs):
    try:return function(*args,**kwargs)
    except S3Error as error:
        if error.code in ('SlowDown','ServiceUnavailable','InternalError','RequestTimeout'):
            raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from None
        if error.code in ('NoSuchKey','NoSuchVersion','NoSuchBucket'):
            raise OperationFailure('ARTIFACT_MISSING') from None
        if error.code in ('AccessDenied','InvalidAccessKeyId','SignatureDoesNotMatch'):
            raise OperationFailure('CONFIG_INVALID') from None
        raise
    except MaxRetryError as error:
        if isinstance(error.reason,SSLError):raise OperationFailure('CONFIG_INVALID') from None
        if isinstance(error.reason,(HTTPTimeout,ProtocolError,NewConnectionError)):
            raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from None
        raise
    except SSLError:
        raise OperationFailure('CONFIG_INVALID') from None
    except (HTTPTimeout,ProtocolError,NewConnectionError,TimeoutError,ConnectionError):
        raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from None


class SemanticObjectReader:
    def __init__(self,client):self._client=client
    def get_object(self,bucket,key,*,version_id):
        response=_call(self._client.get_object,bucket,key,version_id=version_id)
        return _Response(response)


class _Response:
    def __init__(self,response):self._response=response
    def read(self,size):return _call(self._response.read,size)
    def close(self):return self._response.close()
    def release_conn(self):return self._response.release_conn()
