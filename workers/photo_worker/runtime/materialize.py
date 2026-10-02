"""Bounded, hash-checked transfer into an owned attempt directory."""
from __future__ import annotations
from contextlib import closing
from dataclasses import dataclass
import hashlib
from pathlib import Path
from typing import BinaryIO, Callable
from photo_worker.contracts.base import ArtifactRef
from .scratch import ScratchSpace, Reservation


class ArtifactTransferError(ValueError):
    pass


@dataclass
class MaterializedArtifact:
    ref: ArtifactRef
    path: Path
    bytes: int
    reservation: Reservation

    def close(self):self.reservation.release()
    def __enter__(self):return self
    def __exit__(self,*exc):self.close()


def materialize(ref:ArtifactRef,expected_bytes:int,scratch:ScratchSpace,
                open_stream:Callable[[ArtifactRef],BinaryIO],check_active:Callable[[],None],
                *,suffix:str='.bin')->MaterializedArtifact:
    """The object gateway derives access from task capability, not an arbitrary URL.

    expected_bytes/suffix come from verified artifact metadata. The transfer holds
    its reservation until the caller releases the materialized artifact. No
    partially downloaded file survives cancellation, I/O failure or hash mismatch.
    """
    if type(expected_bytes) is not int or expected_bytes<0:
        raise ValueError('Invalid artifact byte count')
    check_active()
    reservation=scratch.reserve(expected_bytes)
    try:
        path=reservation.path('input',suffix)
        digest=hashlib.sha256()
        total=0
        with closing(open_stream(ref)) as source,scratch.open_writer(path,reservation) as output:
            while True:
                check_active()
                block=source.read(min(1024*1024,expected_bytes-total+1))
                if not block:break
                if not isinstance(block,bytes):raise ArtifactTransferError('ARTIFACT_STREAM_INVALID')
                total+=len(block)
                if total>expected_bytes:raise ArtifactTransferError('ARTIFACT_SIZE_MISMATCH')
                output.write(block)
                digest.update(block)
            output.flush()
        check_active()
        if total!=expected_bytes:raise ArtifactTransferError('ARTIFACT_SIZE_MISMATCH')
        if digest.hexdigest()!=ref.sha256:raise ArtifactTransferError('ARTIFACT_HASH_MISMATCH')
        reservation.validate()
        return MaterializedArtifact(ref,path,total,reservation)
    except BaseException:
        reservation.release()
        raise


def materialize_gateway(ref:ArtifactRef,scratch:ScratchSpace,open_stream,check_active:Callable[[],None],
                        *,suffix:str='.bin')->MaterializedArtifact:
    """Reserve from the authenticated Content-Length before reading body bytes.

    open_stream must be Gateway.open_stream (or the same checked transport port).
    Gateway bounds and checks headers before exposing its read-only size. The
    stream is closed even when scratch reservation fails, without downloading it.
    """
    check_active()
    with closing(open_stream(ref)) as stream:
        size=stream.size
        if type(size) is not int or size<0:raise ArtifactTransferError('ARTIFACT_SIZE_MISMATCH')
        return materialize(ref,size,scratch,lambda _:stream,check_active,suffix=suffix)
