"""Read a small task contract with byte limits, full digest and strict JSON."""
from __future__ import annotations
from contextlib import closing
import hashlib
from typing import BinaryIO,Callable,TypeVar
from photo_worker.contracts.base import ArtifactRef,Contract,parse_json
from .materialize import ArtifactTransferError

T=TypeVar('T',bound=Contract)
STYLE_DOCUMENT_LIMIT=100000000

def read_contract(ref:ArtifactRef,model:type[T],open_stream:Callable[[ArtifactRef],BinaryIO],
                  check_active:Callable[[],None],*,max_bytes:int=1048576)->T:
    """Only small control documents, never decoded images or arbitrary URLs.

    The gateway enforces task input authorization. This layer separately verifies
    bytes against the pinned reference, then rejects duplicate keys and invalid
    JCS values before Pydantic performs strict, operation-specific validation.
    """
    if type(max_bytes) is not int or not 1<=max_bytes<=8*1024*1024:
        raise ValueError('Typed document limit must be 1..8388608 bytes')
    if not isinstance(model,type) or not issubclass(model,Contract):
        raise TypeError('Expected a strict task contract')
    return _read(ref,model,open_stream,check_active,max_bytes)


def read_style(ref:ArtifactRef,open_stream:Callable[[ArtifactRef],BinaryIO],check_active:Callable[[],None]):
    """Large portable profiles use the specified 100 MB artifact limit only."""
    from photo_worker.contracts.style import StyleV2
    return _read(ref,StyleV2,open_stream,check_active,STYLE_DOCUMENT_LIMIT)


def read_contract_bytes(ref:ArtifactRef,model:type[T],open_stream:Callable[[ArtifactRef],BinaryIO],
                        check_active:Callable[[],None],*,max_bytes:int=1048576)->bytes:
    """Return exact pinned JSON bytes after full strict contract validation."""
    if type(max_bytes) is not int or not 1<=max_bytes<=8*1024*1024:
        raise ValueError('Typed document limit must be 1..8388608 bytes')
    if not isinstance(model,type) or not issubclass(model,Contract):
        raise TypeError('Expected a strict task contract')
    return _read(ref,model,open_stream,check_active,max_bytes,return_bytes=True)


def _read(ref,model,open_stream,check_active,max_bytes,*,return_bytes=False):
    check_active()
    data=bytearray();digest=hashlib.sha256()
    with closing(open_stream(ref)) as stream:
        while True:
            check_active()
            block=stream.read(min(65536,max_bytes-len(data)+1))
            if not isinstance(block,bytes):raise ArtifactTransferError('ARTIFACT_STREAM_INVALID')
            if not block:break
            data.extend(block)
            if len(data)>max_bytes:raise ArtifactTransferError('ARTIFACT_SIZE_MISMATCH')
            digest.update(block)
    check_active()
    if digest.hexdigest()!=ref.sha256:raise ArtifactTransferError('ARTIFACT_HASH_MISMATCH')
    raw=bytes(data)
    del data
    try:parse_json(raw,max_bytes=max_bytes)
    except ValueError as exc:raise ArtifactTransferError('ARTIFACT_JSON_INVALID') from exc
    result=model.model_validate_json(raw)
    check_active()
    return raw if return_bytes else result
