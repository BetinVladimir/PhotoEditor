"""Task-declared input-only immutable array reader for negative processing."""
from photo_worker.contracts.base import ArtifactRef as ContractRef
from .render_replay_reader import RenderReplayReader


class NegativeInputReader:
    """Join verified V2 membership with a trusted versioned evidence reader.

    Underlying evidence access must be installed for the live worker role and
    attempt. This wrapper adds membership/hash checks; it creates no SQL grant.
    """
    def __init__(self,artifacts,reader,*,check_active):
        if not callable(check_active):raise ValueError('NEGATIVE_AUTHORITY')
        self._artifacts=artifacts
        self._reader=RenderReplayReader(reader,check_active)
        self._check=check_active

    def _declared(self,ref):
        self._check()
        self._artifacts.require_declared_input(ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256))
        self._check()

    def evidence(self,ref):
        self._declared(ref)
        value=self._reader.evidence(ref)
        self._declared(ref)
        return value

    def iter_bytes(self,ref,max_bytes):
        self._declared(ref)
        iterator=iter(self._reader.iter_bytes(ref,max_bytes))
        try:
            for block in iterator:
                self._declared(ref)
                yield block
            self._declared(ref)
        finally:
            close=getattr(iterator,'close',None)
            if close is not None:close()
